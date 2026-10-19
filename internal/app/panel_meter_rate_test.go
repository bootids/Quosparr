package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

// rateTestPanel renders the quota section of provider with ms, mirroring
// panel_meter_test.go's fixture harness.
func rateTestPanel(t *testing.T, provider store.Provider, ms runway.MeterState) *ui.Tester {
	t.Helper()
	snap, st, first := meterTestSnapshot(t, provider, ms)
	l := l10n.New(l10n.English)
	return ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() {
			panelMeterQuota(c, l, snap, provider, PanelActions{}, first, st, st.resetSwitch(store.ResetTimeRelative))
		})
	}, 400, 500)
}

func TestPanelMeterRateAnalysisRows(t *testing.T) {
	l := l10n.New(l10n.English)
	now := time.Now()
	tt := rateTestPanel(t, store.ProviderKimi, runway.MeterState{
		Availability: runway.MeterReady,
		Plan:         "fixture plan",
		Meters:       []runway.Meter{{Title: "pool", RemainingPercent: 50, UsedPercent: 50}},
		Rates: []runway.MeterRate{
			{ID: "cost", Title: "Cost", Unit: "usd", PerHour: 2.5, HasRate: true, HasExhaust: true, ExhaustsAt: now.Add(3*time.Hour + 20*time.Minute), HasAtReset: true, AtReset: 18.5},
			{ID: "usage", Title: "Usage", IsPercent: true, PerHour: 6, HasRate: true, HasReset: true, ResetsAt: now.Add(time.Hour), HasAtReset: true, AtReset: 84},
			{ID: "sparse", Title: "Sparse", Unit: "tokens", HasRate: false},
		},
	})
	for _, want := range []string{
		tx(l, xMeterRateAnalysis),
		"Cost",
		"≈ $2.5/h",
		tx(l, xMeterRateExhausts, "3h20m"),
		tx(l, xMeterRateAtReset, "$18.5"),
		"Usage",
		"≈ 6%/h",
		tx(l, xMeterRateAtReset, "84%"),
	} {
		if !tt.HasText(want) {
			t.Fatalf("rate row missing %q; texts %q", want, tt.Texts())
		}
	}
	for _, text := range tt.Texts() {
		if text == "Sparse" {
			t.Fatalf("pool without a rate rendered a rate row; texts %q", tt.Texts())
		}
	}
}

func TestPanelMeterRateEmptyHintOnlyWhenReadyWithPools(t *testing.T) {
	l := l10n.New(l10n.English)
	tt := rateTestPanel(t, store.ProviderKimi, runway.MeterState{
		Availability: runway.MeterReady,
		Plan:         "fixture plan",
		Meters:       []runway.Meter{{Title: "pool", RemainingPercent: 50, UsedPercent: 50}},
	})
	if !tt.HasText(tx(l, xMeterRateEmpty)) {
		t.Fatalf("sampling hint missing; texts %q", tt.Texts())
	}

	// Not-configured and error states never show the rate block.
	for _, ms := range []runway.MeterState{
		{Availability: runway.MeterNotConfigured},
		{Availability: runway.MeterError, Detail: []runway.DetailLine{{Value: "quota request failed"}}},
	} {
		tt := rateTestPanel(t, store.ProviderAgy, ms)
		if tt.HasText(tx(l, xMeterRateAnalysis)) || tt.HasText(tx(l, xMeterRateEmpty)) {
			t.Fatalf("rate block shown for availability %v; texts %q", ms.Availability, tt.Texts())
		}
	}
}

func TestMeterModuleRecordsHistoryAndExposesRates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meter-history.json")
	hist := meter.LoadHistory(path)
	mod := newMeterModule(enLocalizer(), func(context.Context) meter.State {
		return meter.State{Status: meter.StatusOK, Plan: "Pro", Limits: []meter.Limit{
			{ID: "cost", Title: "Cost", Unit: "usd", Used: 10, LimitValue: 100},
		}}
	}, nil)
	mod.provider = store.ProviderKimi
	mod.history = hist

	// Seed a synthetic sample one hour back so a usable rate exists without
	// waiting for the history's minimum span.
	base := time.Now().Add(-time.Hour)
	if err := hist.Record(string(store.ProviderKimi), meter.State{Status: meter.StatusOK, Limits: []meter.Limit{
		{ID: "cost", Title: "Cost", Unit: "usd", Used: 5, LimitValue: 100},
	}}, base); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := mod.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("history file missing: %v", err)
	}
	st := mod.State(time.Now())
	if len(st.Rates) == 0 {
		t.Fatalf("no rates mapped: %+v", st)
	}
	var rate *runway.MeterRate
	for i := range st.Rates {
		if st.Rates[i].ID == "cost" {
			rate = &st.Rates[i]
		}
	}
	if rate == nil || !rate.HasRate || rate.Title != "Cost" || rate.Unit != "usd" || rate.PerHour <= 0 {
		t.Fatalf("cost rate = %+v", rate)
	}
}

func TestPanelMeterRateSkipsNoiseRows(t *testing.T) {
	l := l10n.New(l10n.English)
	tt := rateTestPanel(t, store.ProviderKimi, runway.MeterState{
		Availability: runway.MeterReady,
		Plan:         "fixture plan",
		Meters:       []runway.Meter{{Title: "pool", RemainingPercent: 50, UsedPercent: 50}},
		Rates: []runway.MeterRate{
			{ID: "dust", Title: "Dust", HasRate: true, PerHour: 3.2e-17, HasAtReset: true, AtReset: 91.00000000000009},
			{ID: "zero", Title: "Zero", HasRate: true, PerHour: 0},
		},
	})
	for _, text := range tt.Texts() {
		if strings.Contains(text, "≈") || strings.Contains(text, "Dust") || strings.Contains(text, "Zero") {
			t.Fatalf("noise rate row rendered %q; texts %q", text, tt.Texts())
		}
	}
	if !tt.HasText(tx(l, xMeterRateEmpty)) {
		t.Fatalf("sampling hint missing; texts %q", tt.Texts())
	}
}

func TestPanelMeterRateUnknownUnitAndFloatArtifacts(t *testing.T) {
	l := l10n.New(l10n.English)
	tt := rateTestPanel(t, store.ProviderKimi, runway.MeterState{
		Availability: runway.MeterReady,
		Plan:         "fixture plan",
		Meters:       []runway.Meter{{Title: "pool", RemainingPercent: 50, UsedPercent: 50}},
		Rates: []runway.MeterRate{
			{ID: "counted", Title: "Counted", Unit: "unknown", PerHour: 2.5, HasRate: true, HasAtReset: true, AtReset: 91.00000000000009},
			{ID: "pct", Title: "Pct", IsPercent: true, PerHour: 1.2, HasRate: true, HasAtReset: true, AtReset: 90.90000000000012},
		},
	})
	for _, want := range []string{
		"≈ 2.5/h",
		tx(l, xMeterRateAtReset, "91"),
		"≈ 1.2%/h",
		tx(l, xMeterRateAtReset, "90.9%"),
	} {
		if !tt.HasText(want) {
			t.Fatalf("rate row missing %q; texts %q", want, tt.Texts())
		}
	}
	for _, text := range tt.Texts() {
		if strings.Contains(text, "unknown") {
			t.Fatalf("unknown unit leaked into %q; texts %q", text, tt.Texts())
		}
	}
}

func TestMeterModuleRecordsBalanceHistoryAndExposesRates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meter-history.json")
	hist := meter.LoadHistory(path)
	mod := newMeterModule(enLocalizer(), func(context.Context) meter.State {
		return meter.State{Status: meter.StatusOK, Balances: []meter.Balance{
			{Currency: "CNY", Total: 80},
		}}
	}, nil)
	mod.provider = store.ProviderDeepSeek
	mod.history = hist
	mod.sampleBalances = true

	// Seed a sample one hour back so a usable spend rate exists without
	// waiting for the history's minimum span.
	base := time.Now().Add(-time.Hour)
	if err := hist.RecordBalances(string(store.ProviderDeepSeek), meter.State{Status: meter.StatusOK, Balances: []meter.Balance{
		{Currency: "CNY", Total: 100},
	}}, base); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := mod.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	st := mod.State(time.Now())
	var rate *runway.MeterRate
	for i := range st.Rates {
		if st.Rates[i].ID == meter.BalancePoolID("CNY") {
			rate = &st.Rates[i]
		}
	}
	if rate == nil {
		t.Fatalf("no balance rate mapped: %+v", st.Rates)
	}
	if rate.Title != "Balance (CNY)" || rate.Unit != "CNY" {
		t.Fatalf("balance rate title/unit = %q/%q", rate.Title, rate.Unit)
	}
	if !rate.HasRate || rate.PerHour < 19.9 || rate.PerHour > 20.1 {
		t.Fatalf("spend rate = %+v want ~20/h", rate)
	}
	// 80 left at 20/h → depleted about four hours from the latest sample.
	if !rate.HasExhaust || time.Until(rate.ExhaustsAt) > 5*time.Hour {
		t.Fatalf("depletion = %+v", rate)
	}
}

func TestMeterModuleWithoutBalanceSamplingStaysDetailOnly(t *testing.T) {
	// Pixellab keeps its balance as a detail line: no balance pools are
	// recorded even after successful refreshes.
	hist := meter.LoadHistory(filepath.Join(t.TempDir(), "meter-history.json"))
	mod := newMeterModule(enLocalizer(), func(context.Context) meter.State {
		return meter.State{Status: meter.StatusOK, Balances: []meter.Balance{
			{Currency: "USD", Total: 12.5},
		}}
	}, nil)
	mod.provider = store.ProviderPixelLab
	mod.history = hist
	for range 2 {
		if err := mod.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if pools := hist.Rates(string(store.ProviderPixelLab), time.Now()); len(pools) != 0 {
		t.Fatalf("pixellab balances must not sample: %+v", pools)
	}
}

func TestPanelMeterBalanceRateRowAndHint(t *testing.T) {
	l := l10n.New(l10n.English)
	now := time.Now()
	balanceRow := runway.MeterRate{
		ID: meter.BalancePoolID("CNY"), Title: "Balance (CNY)", Unit: "CNY",
		PerHour: 1.25, HasRate: true, HasExhaust: true, ExhaustsAt: now.Add(64 * time.Hour),
	}
	tt := rateTestPanel(t, store.ProviderDeepSeek, runway.MeterState{
		Availability: runway.MeterReady,
		Detail:       []runway.DetailLine{{Title: "Balance (CNY)", Value: "80 · granted 30"}},
		Rates:        []runway.MeterRate{balanceRow},
	})
	for _, want := range []string{
		tx(l, xMeterRateAnalysis),
		"Balance (CNY)",
		"≈ 1.25 CNY/h",
		tx(l, xMeterRateExhausts, "64h"),
	} {
		if !tt.HasText(want) {
			t.Fatalf("balance rate row missing %q; texts %q", want, tt.Texts())
		}
	}

	// While samples accumulate, the balance detail lines anchor the hint.
	empty := rateTestPanel(t, store.ProviderDeepSeek, runway.MeterState{
		Availability: runway.MeterReady,
		Detail:       []runway.DetailLine{{Title: "Balance (CNY)", Value: "80"}},
	})
	if !empty.HasText(tx(l, xMeterRateEmpty)) {
		t.Fatalf("sampling hint missing for balance-only provider; texts %q", empty.Texts())
	}
}
