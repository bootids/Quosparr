package runway

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/apiquota"
	"github.com/licoy/quosparr/internal/estimate"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/export/swiftjson"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/sessions"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

type fakeRemoteFetcher struct {
	mu   sync.Mutex
	data []byte
	err  error
}

func (f *fakeRemoteFetcher) FetchStatus(ctx context.Context) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data, f.err
}

func remoteIntPtr(v int) *int { return &v }

func remoteStringPtr(v string) *string { return &v }

func remoteDecimalPtr(s string) *swiftjson.Decimal {
	d := swiftjson.MustDecimal(s)
	return &d
}

func remoteTestNow() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC) }

func remoteResetTime() time.Time { return remoteTestNow().Add(90 * time.Minute) }

func remoteFullStatus() export.StatusSnapshot {
	now, reset := remoteTestNow(), remoteResetTime()
	plan := "plus"
	balance := 1.25
	secondary := export.Window{UsedPercent: 30, ResetsAt: &reset}
	quota := export.NewStatusQuota(export.QuotaInput{
		Plan: &plan, Primary: export.Window{UsedPercent: 42, ResetsAt: &reset},
		Secondary:      &secondary,
		Additional:     []export.NamedWindow{{Name: "gpt-5-codex", Window: export.Window{UsedPercent: 7, ResetsAt: &reset}}},
		CreditsBalance: &balance, UpdatedAt: now,
	})
	cost := export.CostSummary{
		Source: "localSessions", Confidence: "priced",
		Window:       export.Interval{Start: now.Add(-time.Hour), Duration: 3600},
		EstimatedUSD: remoteDecimalPtr("1.5"),
		Totals:       sessions.Totals{TotalTokens: 100, UncachedInputTokens: 60, CachedInputTokens: 20, OutputTokens: 20, Turns: 4, Threads: 2},
		DailyRows: []export.CostDailyRow{{
			Date: "2026-10-18", Totals: sessions.Totals{TotalTokens: 100, Turns: 4},
			EstimatedUSD: remoteDecimalPtr("1.5"), RawCredits: 2,
		}},
		Warnings: []string{}, PricingVersion: "test", CalculatedAt: now,
	}
	codex := export.NewProviderSnapshot(export.ProviderSnapshot{
		Provider: export.ProviderCodex, Availability: export.Available, Plan: &plan,
		Quota: []export.WidgetQuota{
			export.NewWidgetQuota("5-hour", remoteIntPtr(300), export.QuotaStandard, 42, 58, &reset),
			export.NewWidgetQuota("Weekly", remoteIntPtr(10080), export.QuotaStandard, 30, 70, &reset),
			export.NewWidgetQuota("gpt-5-codex", remoteIntPtr(300), export.QuotaModelSpecific, 7, 93, &reset),
		},
		TokenSource:  export.TokensThisMac,
		DailyTokens:  []export.DailyTokens{export.NewDailyTokens("2026-10-17", 1200)},
		ResetCredits: &export.ResetCredits{AvailableCount: 2, ExpiringCount: 1},
	})
	grok := export.NewProviderSnapshot(export.ProviderSnapshot{
		Provider: export.ProviderGrok, Availability: export.Available, Plan: remoteStringPtr("supergrok"),
		Quota: []export.WidgetQuota{
			export.NewWidgetQuota("Included", remoteIntPtr(43200), export.QuotaStandard, 30, 70, &reset),
			export.NewWidgetQuota("grok-4", remoteIntPtr(300), export.QuotaModelSpecific, 10, 90, nil),
		},
	})
	claude := export.NewProviderSnapshot(export.ProviderSnapshot{
		Provider: export.ProviderClaude, Availability: export.Available,
		Quota: []export.WidgetQuota{export.NewWidgetQuota("5h", remoteIntPtr(300), export.QuotaStandard, 15, 85, &reset)},
	})
	return export.StatusSnapshot{
		GeneratedAt: now, Quota: &quota, Cost: &cost,
		Sessions: &sessions.Summary{Items: []sessions.Item{{
			ID: "s1", Title: "Fix bug", ProjectName: "Quosparr", UpdatedAt: now,
			Totals: sessions.Totals{TotalTokens: 100, Turns: 4},
		}}},
		Providers: &export.StatusProviders{
			Codex: &codex, Grok: &grok, Claude: &claude,
			ResetToday: &export.ResetTodaySnapshot{State: export.ResetYes, FetchedAt: now},
		},
	}
}

func remoteTestPrefs() store.Preferences {
	p := store.DefaultPreferences()
	p.FloatingWindow.Enabled = true
	return p
}

func TestBuildRemoteSnapshotFull(t *testing.T) {
	prefs := remoteTestPrefs()
	prefs.ShowsModelSpecificQuotaUsage = true
	snap := BuildRemoteSnapshot(remoteFullStatus(), prefs, l10n.New(l10n.English), remoteTestNow())

	q := snap.Codex.Quota
	if q == nil {
		t.Fatal("codex quota missing")
	}
	if q.Plan != "plus" || q.Primary.UsedPercent != 42 || q.Primary.WindowMinutes != FiveHourMinutes {
		t.Fatalf("primary = %+v", q.Primary)
	}
	if q.Secondary == nil || q.Secondary.UsedPercent != 30 || q.Secondary.WindowMinutes != WeeklyMinutes {
		t.Fatalf("secondary = %+v", q.Secondary)
	}
	if len(q.AdditionalWindows) != 1 || q.AdditionalWindows[0].Name != "gpt-5-codex" {
		t.Fatalf("additional = %+v", q.AdditionalWindows)
	}
	if q.CreditsBalance == nil || *q.CreditsBalance != 1.25 {
		t.Fatalf("credits balance = %+v", q.CreditsBalance)
	}
	if len(snap.Codex.Meters) != 3 {
		t.Fatalf("codex meters = %d", len(snap.Codex.Meters))
	}
	if snap.Codex.StatusText == "" || snap.Codex.QuotaText == "" || len(snap.Codex.QuotaLines) == 0 {
		t.Fatalf("codex text = %q lines %d", snap.Codex.StatusText, len(snap.Codex.QuotaLines))
	}
	c := snap.Codex.Cost
	if c == nil || c.EstimatedUSD == nil || !c.EstimatedUSD.Equal(decimal.MustParse("1.5")) || len(c.DailyRows) != 1 {
		t.Fatalf("cost = %+v", c)
	}
	if snap.Codex.DisplayedCost == nil || snap.Codex.CostText == "" || len(snap.Codex.CostLines) == 0 {
		t.Fatalf("displayed cost = %+v text %q", snap.Codex.DisplayedCost, snap.Codex.CostText)
	}
	if len(snap.Codex.RecentSessions) != 1 || len(snap.Codex.RecentSessionLines) != 1 {
		t.Fatalf("recent sessions = %+v", snap.Codex.RecentSessions)
	}
	if snap.Codex.Heatmap.Local["2026-10-17"] != 1200 {
		t.Fatalf("heatmap = %+v", snap.Codex.Heatmap)
	}
	if snap.Codex.ResetCredits == nil || snap.Codex.ResetCredits.AvailableCount != 2 {
		t.Fatalf("reset credits = %+v", snap.Codex.ResetCredits)
	}
	if snap.Codex.ResetCreditsText == "" || len(snap.Codex.ResetCreditLines) == 0 {
		t.Fatalf("reset credits text = %q", snap.Codex.ResetCreditsText)
	}
	if snap.Grok.Availability != GrokReady || len(snap.Grok.Meters) != 2 || snap.Grok.Plan != "supergrok" {
		t.Fatalf("grok = %+v", snap.Grok)
	}
	if snap.Claude.Availability != ClaudeReady || len(snap.Claude.Meters) != 1 {
		t.Fatalf("claude = %+v", snap.Claude)
	}
	if !snap.ResetToday.HasData || snap.ResetToday.Widget == nil || snap.ResetToday.Widget.State != export.ResetYes {
		t.Fatalf("reset today = %+v", snap.ResetToday)
	}
	if snap.ResetToday.Text == "" || len(snap.ResetToday.Lines) == 0 {
		t.Fatalf("reset today text = %q", snap.ResetToday.Text)
	}
	if snap.StatusBar.Text == "" {
		t.Fatal("status bar text empty")
	}
	if len(snap.Floating.Providers) == 0 {
		t.Fatal("floating empty")
	}
}

func TestBuildRemoteSnapshotEmpty(t *testing.T) {
	snap := BuildRemoteSnapshot(export.StatusSnapshot{}, remoteTestPrefs(), l10n.New(l10n.English), remoteTestNow())
	if snap.Codex.Quota != nil || snap.Codex.Cost != nil || snap.Codex.ResetCredits != nil {
		t.Fatalf("codex = %+v", snap.Codex)
	}
	if snap.Grok.Availability != GrokCLIUnavailable || snap.Claude.Availability != ClaudeNotLoggedIn {
		t.Fatalf("providers = %+v %+v", snap.Grok, snap.Claude)
	}
	if snap.ResetToday.HasData {
		t.Fatal("reset today should have no data")
	}
}

func TestBuildRemoteSnapshotFiltersModelSpecific(t *testing.T) {
	st := remoteFullStatus()
	for _, show := range []bool{false, true} {
		prefs := remoteTestPrefs()
		prefs.ShowsModelSpecificQuotaUsage = show
		snap := BuildRemoteSnapshot(st, prefs, l10n.New(l10n.English), remoteTestNow())
		want := 2
		if show {
			want = 3
		}
		if got := len(snap.Codex.Meters); got != want {
			t.Errorf("show=%v codex meters = %d, want %d", show, got, want)
		}
		wantGrok := 1
		if show {
			wantGrok = 2
		}
		if got := len(snap.Grok.Meters); got != wantGrok {
			t.Errorf("show=%v grok meters = %d, want %d", show, got, wantGrok)
		}
		for _, m := range snap.Grok.Meters {
			if !show && m.Source == SourceModelSpecific {
				t.Errorf("show=%v model-specific meter leaked: %+v", show, m)
			}
		}
	}
}

func waitForRemote(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestRemoteServiceFirstFrameAndClose(t *testing.T) {
	data, err := json.Marshal(remoteFullStatus())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemoteFetcher{data: data}
	s := NewRemote(RemoteOptions{
		Fetcher: f, Preferences: remoteTestPrefs(),
		OnSnapshot: func(Snapshot) {},
	})
	s.Start(context.Background())
	defer s.Close()
	waitForRemote(t, 2*time.Second, func() bool { return s.Snapshot().Codex.Quota != nil })
	snap := s.Snapshot()
	if snap.GeneratedAt.IsZero() || snap.Seq == 0 {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestRemoteServiceFetchError(t *testing.T) {
	f := &fakeRemoteFetcher{err: errors.New("ssh failed: secret stdout leak")}
	s := NewRemote(RemoteOptions{Fetcher: f, Preferences: remoteTestPrefs()})
	s.Start(context.Background())
	defer s.Close()
	waitForRemote(t, 2*time.Second, func() bool { return s.Snapshot().Codex.LastError != "" })
	snap := s.Snapshot()
	for name, msg := range map[string]string{
		"codex": snap.Codex.LastError, "grok": snap.Grok.LastError, "claude": snap.Claude.LastError,
	} {
		if msg == "" {
			t.Fatalf("%s last error empty", name)
		}
		if strings.Contains(msg, "secret") || strings.Contains(msg, "stdout") {
			t.Fatalf("%s error leaks remote content: %q", name, msg)
		}
	}
	if snap.StatusBar.Tooltip == "" {
		t.Fatal("tooltip empty")
	}
}

func TestRemoteServiceDecodeError(t *testing.T) {
	f := &fakeRemoteFetcher{data: []byte(`{"quota":"MARKER LEAK"}`)}
	s := NewRemote(RemoteOptions{Fetcher: f, Preferences: remoteTestPrefs()})
	s.Start(context.Background())
	defer s.Close()
	waitForRemote(t, 2*time.Second, func() bool { return s.Snapshot().Codex.LastError != "" })
	if msg := s.Snapshot().Codex.LastError; strings.Contains(msg, "MARKER") {
		t.Fatalf("decode error leaks remote content: %q", msg)
	}
}

func TestRemoteServiceRefresh(t *testing.T) {
	data, err := json.Marshal(remoteFullStatus())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemoteFetcher{data: data}
	s := NewRemote(RemoteOptions{Fetcher: f, Preferences: remoteTestPrefs()})
	done := s.Refresh()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not finish")
	}
	if s.Snapshot().Codex.Quota == nil {
		t.Fatal("no data after refresh")
	}
	s.Close()
	select {
	case <-s.Refresh():
	default:
		t.Fatal("refresh after close should return a closed channel")
	}
}

func TestRemoteSourceKeysTranslateEveryLanguage(t *testing.T) {
	keys := []l10n.Key{l10n.KeyRemoteSourceUnavailable, l10n.KeyRemoteSourceErrorFetch, l10n.KeyRemoteSourceErrorDecode}
	for _, lang := range l10n.Languages() {
		for _, k := range keys {
			if s, ok := l10n.Lookup(lang, k); !ok || strings.TrimSpace(s) == "" {
				t.Fatalf("%v missing translation for %v", lang, k)
			}
		}
	}
}

func (f *fakeRemoteFetcher) setResult(data []byte, err error) {
	f.mu.Lock()
	f.data, f.err = data, err
	f.mu.Unlock()
}

func remotePanelStatus() export.StatusSnapshot {
	st := remoteFullStatus()
	now := remoteTestNow()
	asOf := time.Date(2026, 10, 17, 23, 0, 0, 0, time.UTC)
	generated := now.Add(-30 * time.Minute)
	remaining, used, total := 3.5, 1.5, 5.0
	apiQuota := exportToAPIQuota(remaining, used, total)
	expires := now.Add(48 * time.Hour)
	estCredits, estUSD, change := 120.0, 4.8, 12.5
	kind := estimate.ChangeIncreased
	reason := estimate.ReasonNoUsage
	st.Panel = &export.StatusPanel{
		Schema: export.PanelSchemaVersion,
		Codex: &export.CodexPanelBlock{
			Estimate: &export.PanelEstimate{
				WindowMode: "rollingWeek", CycleStartDate: "2026-10-12", UsedPercent: 42.5,
				UsedCredits: 30.25, UsedUSD: 1.21, DayCount: 6,
				EstimatedCredits: &estCredits, EstimatedUSD: &estUSD, CanExtrapolate: true,
				HasWeeklyWindow: true,
				CurrentRows:     []export.PanelEstimateRow{{Date: "2026-10-17", Credits: 5.25, Tokens: 900, Turns: 3, CreditsReported: true, TotalsReported: true}},
				ChangePercent:   &change, ChangeKind: string(kind),
				History:          []estimate.HistorySample{{CycleStartDate: "2026-10-05", EstimatedCredits: 100, UsedPercent: 40, UsedCredits: 28, RecordedAt: now.Add(-7 * 24 * time.Hour)}},
				PreviousEstimate: &estimate.HistorySample{CycleStartDate: "2026-10-05", EstimatedCredits: 100, UsedPercent: 40, UsedCredits: 28, RecordedAt: now.Add(-7 * 24 * time.Hour)},
				CalculatedAt:     now, UnavailableReason: string(reason), Error: "",
			},
			AllDevicesTokens: []export.DailyTokens{export.NewDailyTokens("2026-10-16", 5000)},
			LocalTokens:      []export.DailyTokens{export.NewDailyTokens("2026-10-17", 1200), export.NewDailyTokens("2026-10-16", 800)},
			TokensAsOf:       &asOf, TokensGenerated: &generated,
			ResetCredits: &export.PanelResetCredits{AvailableCount: 2, UpdatedAt: now, Credits: []export.PanelResetCredit{
				{ID: "cr1", Status: "available", CreatedAt: now.Add(-24 * time.Hour), ExpiresAt: expires, RemainingSeconds: 172800},
				{ID: "cr2", Status: "redeemed", CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-time.Hour), RemainingSeconds: 0},
			}},
			Speed: &export.PanelSpeedReport{
				Groups:   []export.PanelSpeedStat{{Model: "gpt-5", Effort: "medium", Calls: 4, TTFTP50NS: 900_000_000, TTFTP90NS: 2_000_000_000, OutputTokens: 400, DecodeNS: 8_000_000_000}},
				Sessions: []export.PanelSpeedSession{{ID: "s1", Title: "Fix bug", ProjectName: "Quosparr", UpdatedAt: now, Groups: []export.PanelSpeedStat{{Model: "gpt-5", Effort: "medium", Calls: 4, TTFTP50NS: 900_000_000}}}},
				NoTTFT:   false, Tiered: true, From: now.Add(-time.Hour), To: now, CalculatedAt: now,
			},
			APIKeyMode: true, APIBaseURL: "https://relay.example/v1", APIBalance: apiQuota,
			Accounts: []export.PanelAccount{
				{ID: "acc-2", SortIndex: 2, AuthMode: "oauth", Username: "second", AccountID: "ws-2", PlanType: "pro", Quota: &export.PanelAccountQuotaLine{Plan: "pro", PrimaryUsedPercent: 10, UpdatedAt: now}},
				{ID: "acc-1", SortIndex: 1, AuthMode: "oauth", DisplayName: "First", AccountID: "ws-1", PlanType: "plus", LastUsedAt: &now, RequiresReauth: true},
			},
			ActiveAccountID: "acc-1",
			Display:         &export.PanelIdentity{Authenticated: true, DisplayName: "First"},
		},
		Grok: &export.ProviderPanelBlock{
			Cost:         st.Cost,
			LocalTokens:  []export.DailyTokens{export.NewDailyTokens("2026-10-17", 700)},
			Speed:        &export.PanelSpeedReport{Groups: []export.PanelSpeedStat{{Model: "grok-4", Calls: 2, TTFTP50NS: 500_000_000}}, CalculatedAt: now},
			ResetCredits: &export.PanelResetCredits{AvailableCount: 1, UpdatedAt: now, Credits: []export.PanelResetCredit{{ID: "gr1", Status: "available", ExpiresAt: expires, RemainingSeconds: 100000}}},
			Accounts:     []export.PanelAccount{{ID: "g-1", SortIndex: 1, DisplayName: "GrokUser"}},
			AccountQuotas: []export.PanelAccountQuota{
				{ID: "g-1", Plan: "supergrok", PeriodKind: "weekly", UsedPercent: 44, ResetsAt: &expires, IncludedUSD: "$66 / $150", UpdatedAt: now},
			},
			Billing: &export.PanelGrokBilling{IncludedUsagePercent: 44, UpdatedAt: now},
		},
		Claude: &export.ProviderPanelBlock{
			RecentSessions: []sessions.Item{{ID: "c1", Title: "Claude session", UpdatedAt: now, Totals: sessions.Totals{TotalTokens: 50, Turns: 2}}},
			ExtraUsage:     "10 / 1234 USD",
			API:            &export.PanelAPIView{BaseURL: "https://claude-relay.example", ErrorCode: "api_failed"},
			Accounts:       []export.PanelAccount{{ID: "cl-1", SortIndex: 1, DisplayName: "ClaudeUser"}},
			AccountQuotas: []export.PanelAccountQuota{
				{ID: "cl-1", Plan: "Max 20x", PeriodKind: "monthly", UsedPercent: 60, ResetsAt: &expires, ExtraUsage: "10 / 1234 USD", Balance: "3 / 5", UpdatedAt: now},
			},
		},
	}
	return st
}

func exportToAPIQuota(remaining, used, total float64) *apiquota.Quota {
	return &apiquota.Quota{Remaining: &remaining, Used: &used, Total: &total, FetchedAt: remoteTestNow()}
}

func TestBuildRemoteSnapshotPanelFull(t *testing.T) {
	prefs := remoteTestPrefs()
	snap := BuildRemoteSnapshot(remotePanelStatus(), prefs, l10n.New(l10n.English), remoteTestNow())

	// Codex estimate.
	e := snap.Codex.Estimate
	if e == nil {
		t.Fatal("estimate missing")
	}
	if e.WindowMode != estimate.ModeRollingWeek || e.CycleStartDate != "2026-10-12" || e.UsedPercent != 42.5 || e.DayCount != 6 {
		t.Fatalf("estimate = %+v", e)
	}
	if e.EstimatedCredits == nil || *e.EstimatedCredits != 120 || e.ChangeKind == nil || *e.ChangeKind != estimate.ChangeIncreased {
		t.Fatalf("estimate projection = %+v", e)
	}
	if e.UnavailableReason == nil || *e.UnavailableReason != estimate.ReasonNoUsage {
		t.Fatalf("unavailable reason = %+v", e.UnavailableReason)
	}
	if len(e.CurrentRows) != 1 || e.CurrentRows[0].Tokens != 900 || len(e.History) != 1 || e.PreviousEstimate == nil {
		t.Fatalf("estimate rows = %+v", e)
	}
	if snap.Codex.EstimateError != "" {
		t.Fatalf("estimate error = %q", snap.Codex.EstimateError)
	}
	// Dual token series.
	if snap.Codex.Heatmap.AllDevices["2026-10-16"] != 5000 || snap.Codex.Heatmap.Local["2026-10-17"] != 1200 || snap.Codex.Heatmap.Local["2026-10-16"] != 800 {
		t.Fatalf("heatmap = %+v", snap.Codex.Heatmap)
	}
	if snap.Codex.Heatmap.OfficialStatsAsOf != "2026-10-17" {
		t.Fatalf("official stats as of = %q", snap.Codex.Heatmap.OfficialStatsAsOf)
	}
	// Full reset credits with the detail list.
	rc := snap.Codex.ResetCredits
	if rc == nil || rc.AvailableCount != 2 || len(rc.Credits) != 2 || rc.Credits[0].ID != "cr1" || rc.Credits[0].ExpiresAt.IsZero() {
		t.Fatalf("reset credits = %+v", rc)
	}
	if len(snap.Codex.CreditDetails) != 2 {
		t.Fatalf("credit details = %+v", snap.Codex.CreditDetails)
	}
	// Speed.
	sp := snap.Codex.Speed
	if sp == nil || len(sp.Groups) != 1 || sp.Groups[0].TTFTP50 != 900*time.Millisecond || sp.Groups[0].Decode != 8*time.Second || !sp.Tiered {
		t.Fatalf("speed = %+v", sp)
	}
	if len(sp.Sessions) != 1 || sp.Sessions[0].Groups[0].TTFTP50 != 900*time.Millisecond {
		t.Fatalf("speed sessions = %+v", sp.Sessions)
	}
	// API key mode and balance.
	if !snap.Codex.APIKeyMode || snap.Codex.APIBaseURL != "https://relay.example/v1" || snap.Codex.APIBalance == nil || *snap.Codex.APIBalance.Remaining != 3.5 {
		t.Fatalf("api key mode = %+v", snap.Codex.APIBalance)
	}
	// Accounts: sidebar order by sort index, active id, no email.
	av := snap.Accounts
	if len(av.Sidebar) != 2 || av.Sidebar[0].ID != "acc-1" || av.Sidebar[1].ID != "acc-2" {
		t.Fatalf("sidebar = %+v", av.Sidebar)
	}
	if av.ActiveID != "acc-1" || !av.Display.Authenticated || av.Display.DisplayName != "First" || av.Display.Email != "" {
		t.Fatalf("accounts view = %+v", av)
	}
	if av.Sidebar[0].RequiresReauth != true || av.Sidebar[1].CachedQuota == nil || av.Sidebar[1].CachedQuota.Plan != "pro" {
		t.Fatalf("account rows = %+v", av.Sidebar)
	}
	// Grok.
	g := snap.Grok
	if g.Cost == nil || g.Cost.EstimatedUSD == nil || !g.Cost.EstimatedUSD.Equal(decimal.MustParse("1.5")) {
		t.Fatalf("grok cost = %+v", g.Cost)
	}
	if g.Heatmap.Local["2026-10-17"] != 700 {
		t.Fatalf("grok heatmap = %+v", g.Heatmap)
	}
	if g.Speed == nil || len(g.Speed.Groups) != 1 || g.Speed.Groups[0].Model != "grok-4" {
		t.Fatalf("grok speed = %+v", g.Speed)
	}
	if g.ResetCredits == nil || g.ResetCredits.AvailableCount != 1 || len(g.ResetCredits.Credits) != 1 || g.ResetCreditSummary == nil {
		t.Fatalf("grok reset credits = %+v", g.ResetCredits)
	}
	if g.Billing == nil || g.Billing.IncludedUsagePercent != 44 {
		t.Fatalf("grok billing = %+v", g.Billing)
	}
	gq := g.AccountQuotas["g-1"]
	if g.AccountQuotas == nil || gq.Title == "" || gq.Title == "supergrok" || gq.UsedPercent != 44 || gq.IncludedUSD != "$66 / $150" {
		t.Fatalf("grok account quota = %+v", gq)
	}
	if len(g.Accounts.Sidebar) != 1 || g.Accounts.Sidebar[0].ID != "g-1" {
		t.Fatalf("grok accounts = %+v", g.Accounts)
	}
	// Claude.
	c := snap.Claude
	if len(c.RecentSessions) != 1 || c.RecentSessions[0].ID != "c1" {
		t.Fatalf("claude sessions = %+v", c.RecentSessions)
	}
	if c.ExtraUsage != "10 / 1234 USD" {
		t.Fatalf("claude extra usage = %q", c.ExtraUsage)
	}
	if c.API == nil || c.API.BaseURL != "https://claude-relay.example" || c.API.ErrorCode != "api_failed" {
		t.Fatalf("claude api = %+v", c.API)
	}
	cq := c.AccountQuotas["cl-1"]
	if c.AccountQuotas == nil || cq.Title == "" || cq.Title == "Max 20x" || cq.ExtraUsage != "10 / 1234 USD" || cq.Balance != "3 / 5" {
		t.Fatalf("claude account quota = %+v", cq)
	}
}

func TestBuildRemoteSnapshotPanelEstimateError(t *testing.T) {
	st := remotePanelStatus()
	st.Panel.Codex.Estimate.Error = "estimate failed"
	snap := BuildRemoteSnapshot(st, remoteTestPrefs(), l10n.New(l10n.English), remoteTestNow())
	if snap.Codex.EstimateError != "estimate failed" {
		t.Fatalf("estimate error = %q", snap.Codex.EstimateError)
	}
}

func TestBuildRemoteSnapshotWithoutPanel(t *testing.T) {
	snap := BuildRemoteSnapshot(remoteFullStatus(), remoteTestPrefs(), l10n.New(l10n.English), remoteTestNow())
	if snap.Codex.Estimate != nil || snap.Codex.Speed != nil || snap.Codex.APIKeyMode {
		t.Fatalf("codex = %+v", snap.Codex)
	}
	if len(snap.Accounts.Sidebar) != 0 || snap.Grok.Billing != nil || snap.Grok.Cost != nil || snap.Claude.API != nil {
		t.Fatalf("panel data leaked without a panel block")
	}
	if snap.Codex.ResetCredits == nil || snap.Codex.ResetCredits.AvailableCount != 2 {
		t.Fatalf("widget reset credits = %+v", snap.Codex.ResetCredits)
	}
}

func TestRemoteServiceSetPreferences(t *testing.T) {
	data, err := json.Marshal(remoteFullStatus())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemoteFetcher{data: data}
	s := NewRemote(RemoteOptions{Fetcher: f, Preferences: remoteTestPrefs()})
	s.Start(context.Background())
	defer s.Close()
	waitForRemote(t, 2*time.Second, func() bool { return s.Snapshot().Codex.Quota != nil })
	before := s.Snapshot()

	prefs := remoteTestPrefs()
	prefs.Language = store.LanguageSimplifiedChinese
	s.SetPreferences(prefs)
	<-s.Refresh()
	after := s.Snapshot()
	if after.Language == before.Language || string(after.Language) != string(store.LanguageSimplifiedChinese) {
		t.Fatalf("language = %v -> %v", before.Language, after.Language)
	}
	if after.StatusBar.Text == "" || after.StatusBar.Text == before.StatusBar.Text {
		t.Fatalf("status bar text did not change: %q -> %q", before.StatusBar.Text, after.StatusBar.Text)
	}

	// Interval change: a 1s interval re-fetches without an explicit Refresh.
	prefs.RefreshIntervalSeconds = 1
	s.SetPreferences(prefs)
	waitForRemote(t, 3*time.Second, func() bool { return s.Snapshot().Seq > after.Seq })
}

func TestRemoteServiceKeepsGoodSnapshotOnFailure(t *testing.T) {
	data, err := json.Marshal(remoteFullStatus())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemoteFetcher{data: data}
	s := NewRemote(RemoteOptions{Fetcher: f, Preferences: remoteTestPrefs()})
	s.Start(context.Background())
	defer s.Close()
	waitForRemote(t, 2*time.Second, func() bool { return s.Snapshot().Codex.Quota != nil })
	f.setResult(nil, errors.New("connection lost"))
	<-s.Refresh()
	snap := s.Snapshot()
	if snap.Codex.Quota == nil || snap.Grok.Plan != "supergrok" || len(snap.ResetToday.Lines) == 0 {
		t.Fatalf("good data dropped: %+v", snap)
	}
	for name, msg := range map[string]string{
		"codex": snap.Codex.LastError, "grok": snap.Grok.LastError, "claude": snap.Claude.LastError,
	} {
		if msg == "" {
			t.Fatalf("%s last error empty", name)
		}
	}
	if snap.StatusBar.Text == "" {
		t.Fatal("status bar lost good data")
	}
}

func TestRemoteServiceAlertsDedupeOnce(t *testing.T) {
	st := remotePanelStatus()
	// A quota window over the alert threshold and an expiring credit.
	reset := remoteResetTime()
	q := export.NewStatusQuota(export.QuotaInput{
		Plan:           remoteStringPtr("plus"),
		Primary:        export.Window{UsedPercent: 85, ResetsAt: &reset},
		CreditsBalance: nil, UpdatedAt: remoteTestNow(),
	})
	st.Quota = &q
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemoteFetcher{data: data}
	prefs := remoteTestPrefs()
	prefs.QuotaAlertsEnabled = true
	prefs.ResetCreditAlertsEnabled = true
	var mu sync.Mutex
	events := 0
	s := NewRemote(RemoteOptions{
		Fetcher: f, Preferences: prefs,
		AlertsPath: filepath.Join(t.TempDir(), "alerts.json"),
		OnEvent:    func(Event) { mu.Lock(); events++; mu.Unlock() },
	})
	s.Start(context.Background())
	defer s.Close()
	waitForRemote(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return events > 0
	})
	<-s.Refresh()
	<-s.Refresh()
	mu.Lock()
	defer mu.Unlock()
	if events != 1 {
		t.Fatalf("alert events = %d, want 1", events)
	}
}

func TestRemoteServiceRawStatus(t *testing.T) {
	data, err := json.Marshal(remoteFullStatus())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemoteFetcher{err: errors.New("down")}
	s := NewRemote(RemoteOptions{Fetcher: f, Preferences: remoteTestPrefs()})
	s.Start(context.Background())
	defer s.Close()
	waitForRemote(t, 2*time.Second, func() bool { return s.Snapshot().Codex.LastError != "" })
	if raw := s.RawStatus(); raw.Quota != nil {
		t.Fatalf("raw status before first success = %+v", raw.Quota)
	}
	f.setResult(data, nil)
	<-s.Refresh()
	raw := s.RawStatus()
	if raw.Quota == nil || raw.Quota.Plan == nil || *raw.Quota.Plan != "plus" {
		t.Fatalf("raw status = %+v", raw.Quota)
	}
	// A later failure keeps the last good raw status.
	f.setResult(nil, errors.New("down again"))
	<-s.Refresh()
	if raw := s.RawStatus(); raw.Quota == nil {
		t.Fatal("raw status lost after a failed fetch")
	}
}

// TestRemotePanelEndToEnd proves the two directions against each other: the
// local export fill, the status file encoding and the remote rebuild produce
// the same panel data, so the remote view renders like the local one.
func TestRemotePanelEndToEnd(t *testing.T) {
	src := panelTestSnapshot()
	st := export.StatusSnapshot{GeneratedAt: src.GeneratedAt, Providers: statusExportProviders(src), Panel: statusExportPanel(src)}
	data, err := st.Encode()
	if err != nil {
		t.Fatal(err)
	}
	var decoded export.StatusSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "@chatgpt.com") || strings.Contains(string(data), "@example.com") {
		t.Fatal("status file leaks an email")
	}
	lc := l10n.ForPreference("en", nil)
	out := BuildRemoteSnapshot(decoded, remoteTestPrefs(), lc, src.GeneratedAt)

	e := out.Codex.Estimate
	if e == nil || e.CycleStartDate != "2026-10-12" || e.UsedCredits != 10 || len(e.CurrentRows) != 2 || len(e.History) != 1 || e.PreviousEstimate == nil || e.ChangeKind == nil || *e.ChangeKind != estimate.ChangeIncreased {
		t.Fatalf("estimate = %+v", e)
	}
	if out.Codex.Heatmap.AllDevices["2026-10-17"] != 900 || out.Codex.Heatmap.Local["2026-10-18"] != 650 || out.Codex.Heatmap.OfficialStatsAsOf != "2026-10-18" {
		t.Fatalf("heatmap = %+v", out.Codex.Heatmap)
	}
	if rc := out.Codex.ResetCredits; rc == nil || rc.AvailableCount != 1 || len(rc.Credits) != 1 || rc.Credits[0].ID != "rc-1" {
		t.Fatalf("reset credits = %+v", rc)
	}
	if sp := out.Codex.Speed; sp == nil || len(sp.Groups) != 1 || sp.Groups[0].TTFTP50 != 1200*time.Millisecond || sp.Groups[0].OutputTokens != 900 || len(sp.Sessions) != 1 {
		t.Fatalf("speed = %+v", sp)
	}
	if out.Codex.APIBalance == nil || out.Codex.APIBalance.Remaining == nil || *out.Codex.APIBalance.Remaining != 7.5 {
		t.Fatalf("api balance = %+v", out.Codex.APIBalance)
	}
	if len(out.Accounts.Sidebar) < 2 || out.Accounts.ActiveID != "acc-1" || out.Accounts.Display.Email != "" || out.Accounts.Display.DisplayName != "Alice" {
		t.Fatalf("accounts = %+v", out.Accounts)
	}
	for _, a := range out.Accounts.Sidebar {
		if a.Email != "" {
			t.Fatalf("remote account row carries an email: %+v", a)
		}
	}
	if g := out.Grok; g.Cost == nil || g.Cost.Totals.TotalTokens != 5000 || g.Heatmap.Local["2026-10-18"] != 320 || len(g.RecentSessions) != 1 || g.RecentSessions[0].ID != "g1" || g.Billing == nil {
		t.Fatalf("grok = %+v", g)
	}
	if q := out.Grok.AccountQuotas["g1"]; q.Plan != "grok-heavy" || q.IncludedUSD != "$3 / $10" {
		t.Fatalf("grok quota = %+v", q)
	}
	if c := out.Claude; c.ExtraUsage != "18% of monthly limit" || c.API == nil || c.API.BaseURL != "https://relay.example" || c.API.ErrorCode != "api_failed" || c.Cost == nil || c.Cost.Totals.TotalTokens != 3000 {
		t.Fatalf("claude = %+v", c)
	}
	if q := out.Claude.AccountQuotas["c1"]; q.Balance != "$4 / $5" || q.ExtraUsage != "18%" {
		t.Fatalf("claude quota = %+v", q)
	}
}
