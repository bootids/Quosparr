package app

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/deepseek"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/platform"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/store"
)

func enLocalizer() l10n.Localizer { return l10n.New(l10n.English) }

func TestMeterModuleMapsReadyState(t *testing.T) {
	now := time.Now()
	mod := newMeterModule(enLocalizer(), func(context.Context) meter.State {
		return meter.State{
			Status:    meter.StatusOK,
			Plan:      "Pro",
			FetchedAt: now,
			Limits: []meter.Limit{
				{ID: "tokens_5h", Title: "5h", Unit: "tokens", WindowMinutes: meter.FiveHourMinutes, Used: 40, LimitValue: 100, Percent: 40, HasPercent: true},
			},
		}
	}, nil)
	if err := mod.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := mod.State(now)
	if st.Availability != runway.MeterReady || st.Plan != "Pro" || !st.FetchedAt.Equal(now) {
		t.Fatalf("state = %+v", st)
	}
	if len(st.Meters) != 1 {
		t.Fatalf("meters = %+v", st.Meters)
	}
	m := st.Meters[0]
	if st.Quota == nil || st.Quota.Status != "ok" || st.Quota.Limits[0].ID != "tokens_5h" {
		t.Fatalf("machine quota = %+v", st.Quota)
	}
	if m.Title != enLocalizer().Text(l10n.KeyFiveHourUsage) || m.UsedPercent != 40 {
		t.Fatalf("meter = %+v", m)
	}
}

func TestMeterModuleLoadingBeforeFirstRefresh(t *testing.T) {
	mod := newMeterModule(enLocalizer(), func(context.Context) meter.State { return meter.State{} }, nil)
	if st := mod.State(time.Now()); st.Availability != runway.MeterLoading {
		t.Fatalf("availability = %v, want loading", st.Availability)
	}
}

func TestMeterModuleMapsAuthFailureAndNotConfigured(t *testing.T) {
	now := time.Now()
	auth := newMeterModule(enLocalizer(), func(context.Context) meter.State {
		return meter.Failed(meter.StatusAuth, "quota endpoint rejected the request")
	}, nil)
	if err := auth.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := auth.State(now)
	if st.Availability != runway.MeterError || len(st.Detail) != 1 || st.Detail[0].Value != "quota endpoint rejected the request" {
		t.Fatalf("auth state = %+v", st)
	}
	absent := newMeterModule(enLocalizer(), func(context.Context) meter.State { return meter.NotConfigured() }, nil)
	if err := absent.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := absent.State(now); st.Availability != runway.MeterNotConfigured {
		t.Fatalf("not-configured availability = %v", st.Availability)
	}
}

func TestMeterModuleRefreshLocalDowngradesToNotConfigured(t *testing.T) {
	fired := 0
	mod := newMeterModule(enLocalizer(),
		func(context.Context) meter.State { return meter.State{Status: meter.StatusOK, Plan: "Pro"} },
		func(context.Context) bool { return false })
	mod.SetOnChange(func() { fired++ })
	if err := mod.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fired != 1 {
		t.Fatalf("onChange fired %d times after refresh", fired)
	}
	if err := mod.RefreshLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := mod.State(time.Now())
	if st.Availability != runway.MeterNotConfigured || fired != 2 {
		t.Fatalf("after downgrade: state %+v fired %d", st, fired)
	}
}

func TestMeterValueTextFormats(t *testing.T) {
	cases := []struct {
		limit meter.Limit
		want  string
	}{
		{meter.Limit{Unit: "usd", Used: 12, LimitValue: 200}, "$12 / $200"},
		{meter.Limit{Unit: "usd", Used: 12.5, LimitValue: 200}, "$12.5 / $200"},
		{meter.Limit{Unit: "usd", Used: 1.0 / 3.0, LimitValue: math.NaN()}, "$0.33"},
		{meter.Limit{Unit: "tokens", Used: 1234, LimitValue: 5000}, "1234 / 5000"},
		{meter.Limit{Unit: "percent", Percent: 40, HasPercent: true, Used: math.NaN(), LimitValue: math.NaN()}, ""},
		{meter.Limit{Unit: "tokens", Used: math.NaN()}, ""},
	}
	for _, tc := range cases {
		if got := meterValueText(tc.limit); got != tc.want {
			t.Errorf("meterValueText(%+v) = %q, want %q", tc.limit, got, tc.want)
		}
	}
}

func TestMeterLimitMetersCarryValueText(t *testing.T) {
	now := time.Now()
	rows := meterLimitMeters(enLocalizer(), []meter.Limit{
		{ID: "cost", Title: "Cost", Unit: "usd", WindowMinutes: meter.MonthlyMinutes, Used: 3.5, LimitValue: 20, Percent: 17.5, HasPercent: true},
		{ID: "credits", Title: "Credits", Unit: "credits", Used: 12, LimitValue: 30},
		{ID: "5h", Title: "5h", Unit: "", WindowMinutes: meter.FiveHourMinutes, Used: 0, LimitValue: 100, Percent: 0, HasPercent: true},
		{ID: "7d", Title: "7d", Unit: "", WindowMinutes: meter.WeeklyMinutes, Used: 7, LimitValue: 100, Percent: 7, HasPercent: true},
		{ID: "wk", Title: "7d", Unit: "percent", WindowMinutes: meter.WeeklyMinutes, Percent: 62.5, HasPercent: true},
	}, now)
	if len(rows) != 5 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].ValueText != "$3.5 / $20" || rows[0].UsedPercent != 18 {
		t.Fatalf("usd row = %+v", rows[0])
	}
	if rows[1].ValueText != "12 / 30" || rows[1].UsedPercent != 0 {
		t.Fatalf("credits row = %+v", rows[1])
	}
	if rows[2].ValueText != "" || rows[2].RemainingPercent != 100 || rows[3].ValueText != "" || rows[3].RemainingPercent != 93 {
		t.Fatalf("count rows with a percentage = %+v %+v, want no value text so captions show the percentage", rows[2], rows[3])
	}
	if rows[0].Title != "Cost" {
		t.Fatalf("pool identity title = %q, want Cost", rows[0].Title)
	}
	if rows[4].Title != enLocalizer().Text(l10n.KeyWeeklyUsage) {
		t.Fatalf("bare window title = %q", rows[4].Title)
	}
}

// The zai tab no longer renders usage-summary or account-label detail rows:
// a ready state with usage data must carry no detail lines.
func TestMeterStateOmitsUsageDetail(t *testing.T) {
	mod := newMeterModule(enLocalizer(), func(context.Context) meter.State {
		return meter.State{Status: meter.StatusOK, Usage: &meter.Usage{
			TotalCalls:  8,
			TotalTokens: 826662212,
			Models:      []meter.UsageModel{{Name: "GLM-5.3", Tokens: 826662212}},
		}}
	}, nil)
	if err := mod.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := mod.State(time.Now()); len(st.Detail) != 0 {
		t.Fatalf("detail = %+v, want none", st.Detail)
	}
}

func TestSelectableProvidersReturnsAllProviders(t *testing.T) {
	got := selectableProviders()
	want := store.Providers()
	if !slices.Equal(got, want) || len(got) != 11 {
		t.Fatalf("selectableProviders() = %v", got)
	}
}

func TestMeterModuleSpeedScanFlowsIntoState(t *testing.T) {
	now := time.Now()
	report := speed.Report{
		Groups:       []speed.Stat{{Model: "kimi-k2", Calls: 3, TTFTP50: 800 * time.Millisecond}},
		From:         now.Add(-time.Hour),
		To:           now,
		CalculatedAt: now,
	}
	mod := newMeterModule(enLocalizer(), func(context.Context) meter.State {
		return meter.State{Status: meter.StatusOK, Plan: "Pro"}
	}, nil)
	mod.speedScan = func(context.Context, time.Time) (speed.Report, error) {
		return report, nil
	}
	if err := mod.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := mod.State(now)
	if st.Speed == nil || st.SpeedError != "" {
		t.Fatalf("speed = %+v error = %q", st.Speed, st.SpeedError)
	}
	if st.Speed.Groups[0].Model != "kimi-k2" || st.Speed.Groups[0].TTFTP50 != 800*time.Millisecond {
		t.Fatalf("speed group = %+v", st.Speed.Groups)
	}

	// A failing scan replaces the report with a sanitized error.
	boom := errors.New("stats database is locked")
	mod.speedScan = func(context.Context, time.Time) (speed.Report, error) {
		return speed.Report{}, boom
	}
	if err := mod.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := mod.State(now); st.Speed != nil || st.SpeedError != boom.Error() {
		t.Fatalf("failed scan speed = %+v error = %q", st.Speed, st.SpeedError)
	}

	// A module without a speedScan (cursor, agy) reports neither.
	plain := newMeterModule(enLocalizer(), func(context.Context) meter.State {
		return meter.State{Status: meter.StatusOK}
	}, nil)
	if err := plain.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := plain.State(now); st.Speed != nil || st.SpeedError != "" {
		t.Fatalf("speedless module speed = %+v error = %q", st.Speed, st.SpeedError)
	}
}

func TestMeterModuleFiltersStaleHistoryRates(t *testing.T) {
	hist := meter.LoadHistory(filepath.Join(t.TempDir(), "meter-history.json"))
	mod := newMeterModule(enLocalizer(), func(context.Context) meter.State {
		return meter.State{Status: meter.StatusOK, Limits: []meter.Limit{
			{ID: "5h", Title: "5h", Used: 20, LimitValue: 100},
			{ID: "7d", Title: "7d", Used: 30, LimitValue: 100},
		}}
	}, nil)
	mod.provider = store.ProviderKimi
	mod.history = hist

	// Seed history with a pool (limit_month) that the current state no longer
	// exposes; its leftover rate must not surface as a rendered row.
	base := time.Now().Add(-time.Hour)
	if err := hist.Record(string(store.ProviderKimi), meter.State{Status: meter.StatusOK, Limits: []meter.Limit{
		{ID: "5h", Title: "5h", Used: 10, LimitValue: 100},
		{ID: "7d", Title: "7d", Used: 20, LimitValue: 100},
		{ID: "limit_month", Title: "limit_month", Used: 5, LimitValue: 100},
	}}, base); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := mod.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	st := mod.State(time.Now())
	ids := make(map[string]bool, len(st.Rates))
	for _, r := range st.Rates {
		ids[r.ID] = true
	}
	if len(st.Rates) != 2 || !ids["5h"] || !ids["7d"] || ids["limit_month"] {
		t.Fatalf("rates = %+v", st.Rates)
	}
}

func TestMeterModulesNewProvidersNotConfiguredWithoutCredential(t *testing.T) {
	for _, name := range []string{"DEEPSEEK_API_KEY", "MINIMAX_CODING_API_KEY", "MINIMAX_API_KEY", "PIXELLAB_API_KEY", "ELEVENLABS_API_KEY", "KIMI_SHARE_DIR"} {
		t.Setenv(name, "")
	}
	home := t.TempDir()
	_, mods, _, _ := newMeterModules(home, store.Preferences{}, nil)
	for _, p := range []store.Provider{store.ProviderDeepSeek, store.ProviderMiniMax, store.ProviderPixelLab, store.ProviderElevenLabs} {
		mod, ok := mods[p]
		if !ok {
			t.Fatalf("meter module of %s missing", p)
		}
		if mod.detect(context.Background()) {
			t.Errorf("detect(%s) = true without any credential", p)
		}
		if err := mod.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		if st := mod.State(time.Now()); st.Availability != runway.MeterNotConfigured {
			t.Fatalf("%s availability = %v, want not-configured; detail %+v", p, st.Availability, st.Detail)
		}
	}
}

func TestMeterModulesDeepSeekConfiguredKeyDetected(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	home := t.TempDir()
	root := platform.RunwayDir(home)
	if err := deepseek.SaveConfig(root, "sk-deepseek-test"); err != nil {
		t.Fatal(err)
	}
	_, mods, _, _ := newMeterModules(home, store.Preferences{}, nil)
	mod := mods[store.ProviderDeepSeek]
	if mod == nil {
		t.Fatal("deepseek module missing")
	}
	if !mod.detect(context.Background()) {
		t.Fatal("detect(deepseek) = false with an explicit key")
	}
	st := meterStateFromRaw(enLocalizer(), meter.State{
		Status: meter.StatusOK,
		Balances: []meter.Balance{
			{Currency: "CNY", Total: 110, Granted: 10, ToppedUp: 100},
			{Currency: "USD", Total: 12.5, Granted: 2.5, ToppedUp: 10},
		},
		FetchedAt: time.Now(),
	}, true, time.Now())
	if st.Availability != runway.MeterReady || len(st.Detail) != 2 ||
		!strings.Contains(st.Detail[0].Title, "CNY") || !strings.HasPrefix(st.Detail[0].Value, "110") {
		t.Fatalf("deepseek balance state = %+v", st)
	}
	if st.BalanceText != "110 CNY · 12.5 USD" {
		t.Fatalf("balance text = %q, want the compact multi-currency summary", st.BalanceText)
	}
}
