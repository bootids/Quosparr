package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/export/swiftjson"
	"github.com/licoy/quosparr/internal/grok"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/remote"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/sessions"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
)

func TestRemoteEnabled(t *testing.T) {
	cases := []struct {
		name  string
		prefs store.Preferences
		want  bool
	}{
		{"no source", store.Preferences{}, false},
		{"disabled", store.Preferences{RemoteSource: &store.RemoteSource{Host: "host"}}, false},
		{"no host", store.Preferences{RemoteSource: &store.RemoteSource{Enabled: true}}, false},
		{"enabled", store.Preferences{RemoteSource: &store.RemoteSource{Enabled: true, Host: "user@host"}}, true},
	}
	for _, c := range cases {
		if got := remoteEnabled(c.prefs); got != c.want {
			t.Errorf("%s: remoteEnabled = %v, want %v", c.name, got, c.want)
		}
	}
}

// waitForRemoteTask drains the main queue until the task's completion (and
// its message) has been delivered.
func waitForRemoteTask(t *testing.T, e *env, task *Task) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e.q.drain()
		if !task.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("remote test task did not finish")
}

func TestRemoteDraftSource(t *testing.T) {
	d := &remoteDraftState{enabled: true, host: "  user@host  ", port: "2222", dir: " /srv/quosparr "}
	src, err := d.source()
	if err != nil {
		t.Fatal(err)
	}
	if !src.Enabled || src.Host != "user@host" || src.Port != 2222 || src.Dir != "/srv/quosparr" {
		t.Fatalf("source = %+v", src)
	}
	src, err = (&remoteDraftState{}).source()
	if err != nil || src == nil || src.Port != 0 {
		t.Fatalf("empty draft: %+v, %v", src, err)
	}
	for _, bad := range []string{"0", "65536", "-1", "ssh"} {
		if _, err := (&remoteDraftState{port: bad}).source(); err == nil {
			t.Errorf("port %q accepted", bad)
		}
	}
}

func TestRemoteDraftValidation(t *testing.T) {
	base := store.DefaultPreferences()
	if err := remoteDraftError(base, &store.RemoteSource{Enabled: true, Host: "host"}); err != nil {
		t.Errorf("valid source rejected: %v", err)
	}
	if err := remoteDraftError(base, &store.RemoteSource{Enabled: true}); err == nil {
		t.Error("enabled source without a host passed validation")
	}
	if err := remoteDraftError(base, &store.RemoteSource{Port: 70000}); err == nil {
		t.Error("out-of-range port passed validation")
	}
}

func TestRemoteTestConnectionStateMachine(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	u := newSettingsUI(e.s, SettingsOptions{})
	prev := remoteTestConnection
	called := 0
	remoteTestConnection = func(context.Context, remote.Config) error {
		called++
		return remote.ErrConnectionFailed
	}
	t.Cleanup(func() { remoteTestConnection = prev })

	u.remote.load(e.s.Prefs())
	u.remote.enabled, u.remote.host = true, "user@host"
	u.testRemoteConnection()
	waitForRemoteTask(t, e, &u.remote.test)
	e.q.drain()
	if called != 1 {
		t.Fatalf("test connection ran %d times", called)
	}
	if u.remote.test.Running {
		t.Error("task still running")
	}
	if !u.remote.msgErr || u.remote.msg != e.s.L().Text(l10n.KeyRemoteSourceConnectionFailed) {
		t.Fatalf("msg = %q (err %v)", u.remote.msg, u.remote.msgErr)
	}

	// A second click while idle runs again and can succeed.
	remoteTestConnection = func(context.Context, remote.Config) error { return nil }
	u.testRemoteConnection()
	waitForRemoteTask(t, e, &u.remote.test)
	e.q.drain()
	if u.remote.msgErr || u.remote.msg != e.s.L().Text(l10n.KeyRemoteSourceTestSucceeded) {
		t.Fatalf("success msg = %q (err %v)", u.remote.msg, u.remote.msgErr)
	}
}

func TestRemoteSaveDraftStoresPreferences(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	u := newSettingsUI(e.s, SettingsOptions{})
	u.remote.load(e.s.Prefs())
	u.remote.enabled, u.remote.host, u.remote.port = true, "user@host", "2222"
	if !u.remoteUnsaved() {
		t.Fatal("draft reported saved")
	}
	u.saveRemoteSource()
	e.settle()
	got := e.stored().RemoteSource
	if got == nil || !got.Enabled || got.Host != "user@host" || got.Port != 2222 {
		t.Fatalf("stored remote source = %+v", got)
	}
	if u.remote.msgErr {
		t.Fatalf("save message = %q", u.remote.msg)
	}

	// An invalid draft shows an error and saves nothing.
	u.remote.host, u.remote.enabled = "", true
	u.saveRemoteSource()
	if !u.remote.msgErr {
		t.Error("invalid draft saved without an error")
	}
}

func TestRemoteModeGatesLocalActions(t *testing.T) {
	rt := &liveApp{remoteSvc: &remoteRuntime{}}
	if !rt.remoteMode() {
		t.Fatal("runtime not in remote mode")
	}
	if (&liveApp{}).remoteMode() {
		t.Fatal("empty runtime counted as remote")
	}

	var actions PanelActions
	installAccountActions(rt, nil, &actions)
	if actions.Accounts.Switch != nil || actions.Accounts.Delete != nil || actions.Accounts.RefreshAll != nil {
		t.Fatal("remote mode installed account writers")
	}

	installDetailActions(rt, &actions)
	if actions.QueryCost == nil {
		t.Fatal("remote mode dropped the read-only cost query")
	}
	if _, err := actions.QueryCost(context.Background(), store.ProviderGrok, CostQuery{}); err != errDetailUnavailable {
		t.Fatalf("cost query without a remote service = %v, want %v", err, errDetailUnavailable)
	}
	if actions.QueryReaction != nil || actions.RepairSessions != nil {
		t.Fatal("remote mode installed detail writers")
	}
	if actions.Skills.Query == nil || actions.Skills.ReadBody == nil {
		t.Error("remote mode dropped read-only skills actions")
	}
	if actions.Skills.SetEnabled != nil || actions.Skills.Delete != nil || actions.Skills.Restore != nil || actions.Skills.EmptyTrash != nil {
		t.Error("remote mode installed skills writers")
	}

	panel := rt.remotePanelActions(nil)
	if panel.Refresh == nil {
		t.Error("manual refresh missing in remote mode")
	}
	if panel.SelectProvider == nil {
		t.Error("provider switch missing in remote mode")
	} else {
		panel.SelectProvider(store.ProviderClaude) // nil settings must stay safe
	}
	if panel.SwitchAccount != nil {
		t.Error("remote mode installed account switching")
	}
}

func TestRemoteModeHidesLocalOnlyPanelModules(t *testing.T) {
	prefs := store.DefaultPreferences()
	prefs.ShowsSessionRepairSummary = true
	prefs.ShowsSkills = true
	prefs.ShowsQuotaSummary = true
	prefs.ShowsRecentSessions = true
	if !panelModuleOn(store.ModuleSessionRepair, prefs, false) || !panelModuleOn(store.ModuleSkills, prefs, false) {
		t.Fatal("local mode must show both modules")
	}
	prefs.RemoteSource = &store.RemoteSource{Enabled: true, Host: "10.10.10.20"}
	if panelModuleOn(store.ModuleSessionRepair, prefs, false) || panelModuleOn(store.ModuleSkills, prefs, false) {
		t.Fatal("remote mode must hide session repair and skills")
	}
	if !panelModuleOn(store.ModuleQuota, prefs, false) || !panelModuleOn(store.ModuleRecentSessions, prefs, false) {
		t.Fatal("remote mode must keep the read-only modules")
	}
}

// staticRemoteFetcher serves one exported status snapshot.
type staticRemoteFetcher struct {
	data []byte
}

func (f staticRemoteFetcher) FetchStatus(context.Context) ([]byte, error) { return f.data, nil }

func remoteTestStatus(t *testing.T, snap export.StatusSnapshot, now func() time.Time) *remoteRuntime {
	t.Helper()
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	opts := runway.RemoteOptions{Fetcher: staticRemoteFetcher{data: data}, Preferences: store.DefaultPreferences()}
	if now != nil {
		opts.Now = now
	}
	rr := &remoteRuntime{RemoteService: runway.NewRemote(opts), prefs: store.DefaultPreferences()}
	t.Cleanup(rr.Close)
	return rr
}

func TestRemoteDetailCostFromPanel(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	fixed := start.Add(7 * 24 * time.Hour)
	cost := &export.CostSummary{
		Source: "localSessions", Confidence: "priced",
		Window:    export.Interval{Start: start, Duration: 7 * 24 * 3600},
		ModelRows: []export.CostBreakdownRow{{Name: "grok-4"}},
	}
	for i := 0; i < 7; i++ {
		cost.DailyRows = append(cost.DailyRows, export.CostDailyRow{
			Date:         start.AddDate(0, 0, i).Format("2006-01-02"),
			Totals:       sessions.Totals{TotalTokens: 10},
			EstimatedUSD: swiftjsonPtr(strconv.Itoa(i + 1)),
		})
	}
	rr := remoteTestStatus(t, export.StatusSnapshot{Panel: &export.StatusPanel{Grok: &export.ProviderPanelBlock{Cost: cost}}}, func() time.Time { return fixed })
	rt := &liveApp{remoteSvc: rr}
	<-rr.RemoteService.Refresh()

	ctx := context.Background()
	custom := &usage.CostRange{Window: usage.Window{Start: start.AddDate(0, 0, 1), End: start.AddDate(0, 0, 3)}}
	sum, err := rt.queryRemoteDetailCost(ctx, store.ProviderGrok, CostQuery{Custom: custom})
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.DailyRows) != 2 || sum.DailyRows[0].Date != "2026-10-02" || sum.DailyRows[1].Date != "2026-10-03" {
		t.Fatalf("daily rows = %+v", sum.DailyRows)
	}
	if sum.Totals.TotalTokens != 20 {
		t.Errorf("totals = %+v", sum.Totals)
	}
	if sum.EstimatedUSD == nil || sum.EstimatedUSD.String() != "5" {
		t.Errorf("estimated usd = %v", sum.EstimatedUSD)
	}
	if len(sum.ModelRows) != 0 {
		t.Errorf("partial window copied breakdown rows: %+v", sum.ModelRows)
	}
	if sum.Source != usage.SourceLocalSessions || sum.Confidence != usage.ConfidencePriced {
		t.Errorf("source/confidence = %v/%v", sum.Source, sum.Confidence)
	}

	// The exported window keeps the breakdown rows.
	sum, err = rt.queryRemoteDetailCost(ctx, store.ProviderGrok, CostQuery{Range: store.CostRangeCurrent})
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.DailyRows) != 7 || len(sum.ModelRows) != 1 {
		t.Fatalf("current cycle rows = %d/%d", len(sum.DailyRows), len(sum.ModelRows))
	}
	if !sum.Window.Start.Equal(start) {
		t.Errorf("window start = %v, want %v", sum.Window.Start, start)
	}

	// A provider without a cost mirror stays unavailable.
	if _, err := rt.queryRemoteDetailCost(ctx, store.ProviderClaude, CostQuery{Custom: custom}); err != errDetailUnavailable {
		t.Errorf("claude cost error = %v, want %v", err, errDetailUnavailable)
	}
}

func TestRemoteRuntimeForwardsPreferences(t *testing.T) {
	rr := remoteTestStatus(t, export.StatusSnapshot{}, nil)
	<-rr.RemoteService.Refresh()
	p := rr.Preferences()
	if p.SelectedProvider == store.ProviderGrok {
		p.SelectedProvider = store.ProviderClaude
	} else {
		p.SelectedProvider = store.ProviderGrok
	}
	rr.SetPreferences(p)
	if rr.Preferences().SelectedProvider != p.SelectedProvider {
		t.Fatal("SetPreferences did not store the app-side copy")
	}
	<-rr.RemoteService.Refresh()
	if got := rr.Snapshot().Provider; got != p.SelectedProvider {
		t.Fatalf("snapshot provider = %v, want %v (preferences not forwarded)", got, p.SelectedProvider)
	}
}

func TestRebuildRemoteSnapshotDetail(t *testing.T) {
	var snap runway.Snapshot
	if got := rebuildRemoteSnapshotDetail(snap, export.StatusSnapshot{}); got.Grok.Detail != nil {
		t.Error("empty status produced a grok detail")
	}
	billing := &export.PanelGrokBilling{Plan: ptrString("super"), Source: "current"}
	raw := export.StatusSnapshot{Panel: &export.StatusPanel{Grok: &export.ProviderPanelBlock{Billing: billing}}}
	detail, ok := rebuildRemoteSnapshotDetail(snap, raw).Grok.Detail.(*grok.BillingSnapshot)
	if !ok || detail == nil {
		t.Fatalf("detail = %#v", rebuildRemoteSnapshotDetail(snap, raw).Grok.Detail)
	}
	if detail.Plan == nil || *detail.Plan != "super" || detail.Source != grok.BillingSourceCurrent {
		t.Errorf("detail = %+v", detail)
	}
}

func TestGrokBillingPanelRoundTrip(t *testing.T) {
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	reset := time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)
	limit, used := int64(2500), int64(1250)
	onDemand, unified := true, false
	tokenEnd := start.Add(72 * time.Hour)
	orig := grok.BillingSnapshot{
		Plan:                 ptrString("SuperGrok"),
		IncludedUsagePercent: 42.5,
		Period:               &grok.BillingPeriod{Kind: grok.BillingWeekly, StartsAt: &start, ResetsAt: &reset},
		IncludedLimitCents:   &limit,
		IncludedUsedCents:    &used,
		PrepaidBalanceCents:  &limit,
		OnDemandEnabled:      &onDemand,
		OnDemandUsedCents:    &used,
		OnDemandLimitCents:   &limit,
		ProductUsage:         []grok.BillingProductUsage{{Product: "GrokBuild", UsagePercent: 30}, {Product: "GrokChat", UsagePercent: 12.5}},
		IsUnifiedBillingUser: &unified,
		ResetCredits: &grok.ResetCreditSnapshot{
			AvailableCount: 1,
			Tokens:         []grok.ResetCreditToken{{TokenID: "tok", ValidityStart: &start, ValidityEnd: &tokenEnd}},
			UpdatedAt:      start,
		},
		Source:    grok.BillingSourceCurrent,
		UpdatedAt: start,
	}
	back := grokBillingFromPanel(grokBillingToPanel(&orig))
	if !reflect.DeepEqual(*back, orig) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", *back, orig)
	}
}

func swiftjsonPtr(s string) *swiftjson.Decimal { d := swiftjson.MustDecimal(s); return &d }

func ptrString(s string) *string { return &s }

func TestRemoteRuntimePreferenceBridge(t *testing.T) {
	rr := &remoteRuntime{prefs: store.DefaultPreferences()}
	p := rr.Preferences()
	p.Language = "fr"
	if rr.Preferences().Language == "fr" {
		t.Error("Preferences returned the caller's copy")
	}
	p = store.DefaultPreferences()
	p.Language = "fr"
	rr.SetPreferences(p)
	if rr.Preferences().Language != "fr" {
		t.Error("SetPreferences did not store the copy")
	}
	rr.SetWidgetRequirements(0)
	rr.ReportWidgetError(nil)
	rr.SetPanelOpen(true)
}

func TestRemoteRuntimeImplementsServiceRuntime(t *testing.T) {
	var _ serviceRuntime = &remoteRuntime{}
	var _ serviceRuntime = (*runway.Service)(nil)
}
