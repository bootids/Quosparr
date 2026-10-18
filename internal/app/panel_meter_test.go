package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

func meterTestSnapshot(t *testing.T, provider store.Provider, ms runway.MeterState) (runway.Snapshot, *panelState, *bool) {
	t.Helper()
	snap := runway.Snapshot{}
	if ms.Availability != runway.MeterNotConfigured || len(ms.Meters) > 0 {
		snap.Meters = map[store.Provider]runway.MeterState{provider: ms}
	}
	first := true
	return snap, &panelState{}, &first
}

func TestPanelMeterQuotaShowsPlanAndPercentRow(t *testing.T) {
	l := l10n.New(l10n.English)
	now := time.Now()
	rows := meterLimitMeters(l, []meter.Limit{
		{ID: "tokens_5h", Title: "5h", Unit: "tokens", WindowMinutes: meter.FiveHourMinutes, Used: 30, LimitValue: 100, Percent: 30, HasPercent: true},
	}, now)
	snap, st, first := meterTestSnapshot(t, store.ProviderZai, runway.MeterState{
		Availability: runway.MeterReady,
		Plan:         "GLM Coding Plan",
		Meters:       rows,
		Detail:       []runway.DetailLine{{Value: "Team key"}, {Value: "GLM-5.3: 826662212"}},
	})
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() {
			panelMeterQuota(c, l, snap, store.ProviderZai, PanelActions{}, first, st, st.resetSwitch(store.ResetTimeRelative))
		})
	}, 400, 500)
	if !tt.HasText(l.Text(l10n.KeyProviderZai)) {
		t.Fatal("section title missing")
	}
	if !tt.HasText("GLM Coding Plan") || !tt.HasText("GLM-5.3: 826662212") {
		t.Fatal("plan or usage detail missing")
	}
	if !tt.HasText("70% left") {
		t.Fatal("percent row missing")
	}
}

func TestPanelMeterQuotaNotConfigured(t *testing.T) {
	l := l10n.New(l10n.English)
	snap, st, first := meterTestSnapshot(t, store.ProviderCursor, runway.MeterState{})
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() {
			panelMeterQuota(c, l, snap, store.ProviderCursor, PanelActions{}, first, st, st.resetSwitch(store.ResetTimeRelative))
		})
	}, 400, 300)
	if !tt.HasText(l.Text(l10n.KeyMeterNotConfigured)) {
		t.Fatal("not-configured text missing")
	}
}

func TestPanelMeterQuotaErrorShowsMessage(t *testing.T) {
	l := l10n.New(l10n.English)
	snap, st, first := meterTestSnapshot(t, store.ProviderAgy, runway.MeterState{
		Availability: runway.MeterError,
		Detail:       []runway.DetailLine{{Value: "quota request failed"}},
	})
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() {
			panelMeterQuota(c, l, snap, store.ProviderAgy, PanelActions{}, first, st, st.resetSwitch(store.ResetTimeRelative))
		})
	}, 400, 300)
	if !tt.HasText("quota request failed") {
		t.Fatal("error message missing")
	}
}
