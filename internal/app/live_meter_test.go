package app

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/runway"
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
		{meter.Limit{Unit: "usd", Used: 1.0 / 3.0, LimitValue: math.NaN()}, "$0.3333333333333333"},
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
	}, now)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].ValueText != "$3.5 / $20" || rows[0].UsedPercent != 18 {
		t.Fatalf("usd row = %+v", rows[0])
	}
	if rows[1].ValueText != "12 / 30" || rows[1].UsedPercent != 0 {
		t.Fatalf("credits row = %+v", rows[1])
	}
	if rows[0].Title != enLocalizer().Text(l10n.KeyQuota) {
		t.Fatalf("monthly title = %q", rows[0].Title)
	}
}

func TestMeterUsageLines(t *testing.T) {
	lines := meterUsageLines(&meter.Usage{
		TotalCalls:  8,
		TotalTokens: 826662212,
		Models:      []meter.UsageModel{{Name: "GLM-5.3", Tokens: 826662212}},
		Features:    []meter.UsageFeature{{ID: "codePlan", Used: 8}},
	})
	wants := []string{"GLM-5.3: 826662212", "calls: 8", "tokens: 826662212", "codePlan: 8"}
	if len(lines) != len(wants) {
		t.Fatalf("lines = %+v", lines)
	}
	for i, w := range wants {
		if lines[i].Value != w {
			t.Errorf("line %d = %q, want %q", i, lines[i].Value, w)
		}
	}
}

func TestMeterModuleLabelPrependsDetail(t *testing.T) {
	mod := newMeterModule(enLocalizer(), func(context.Context) meter.State {
		return meter.State{Status: meter.StatusOK}
	}, nil)
	mod.label = func() string { return "Team key" }
	if err := mod.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := mod.State(time.Now())
	if len(st.Detail) != 1 || st.Detail[0].Value != "Team key" {
		t.Fatalf("detail = %+v", st.Detail)
	}
}

func TestSelectableProvidersReturnsAllSeven(t *testing.T) {
	got := selectableProviders()
	want := store.Providers()
	if !slices.Equal(got, want) || len(got) != 7 {
		t.Fatalf("selectableProviders() = %v", got)
	}
}
