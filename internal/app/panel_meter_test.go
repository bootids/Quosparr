package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/speed"
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
		Detail:       []runway.DetailLine{{Value: "synthetic detail row"}},
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
	if !tt.HasText("GLM Coding Plan") || !tt.HasText("synthetic detail row") {
		t.Fatal("plan or detail row missing")
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

func TestPanelQuotaOnlyTabDropsCodexModules(t *testing.T) {
	for _, provider := range []store.Provider{store.ProviderCursor, store.ProviderKimi, store.ProviderZai, store.ProviderAgy} {
		t.Run(string(provider), func(t *testing.T) {
			snap := panelFixture()
			snap.Accounts.Message = "codex account message"
			snap.Codex.LastError = "codex scan failed"
			snap.Meters = map[store.Provider]runway.MeterState{provider: {
				Availability: runway.MeterReady,
				Plan:         "fixture plan",
				Meters:       []runway.Meter{{Title: "pool", RemainingPercent: 50, UsedPercent: 50}},
			}}
			a, tt := newPanel(t, func(p *store.Preferences) { p.SelectedProvider = provider }, snap, PanelActions{})
			l := a.L()
			if !tt.HasText("fixture plan") || !hasExact(tt, "50% left") {
				t.Fatalf("the tab's own quota section missing; texts %q", tt.Texts())
			}
			for _, gone := range []string{
				l.Text(l10n.KeyTokenUsageHeatmap), // token usage kept the Codex chart
				tx(l, xSpeedTitle),                // speed analysis kept the Codex report
				l.Text(l10n.KeyApiCost),
				l.Text(l10n.KeyRecentSessions),
				l.Text(l10n.KeyQuotaEstimate),
				l.Text(l10n.KeyResetCredits),
				"Ada Lovelace",         // Codex identity row
				"quota-headline",       // Codex quota headline
				"reset-today-headline", // reset-today module
				"codex account message",
				"codex scan failed",
			} {
				if tt.HasText(gone) {
					t.Fatalf("%s tab still shows %q; texts %q", provider, gone, tt.Texts())
				}
			}
		})
	}
}

func TestPanelQuotaOnlyTabKeepsRemotePullError(t *testing.T) {
	snap := panelFixture()
	snap.Codex.LastError = "pull failed"
	snap.Meters = map[store.Provider]runway.MeterState{store.ProviderKimi: {Availability: runway.MeterReady, Plan: "fixture plan"}}
	_, tt := newPanel(t, func(p *store.Preferences) {
		p.SelectedProvider = store.ProviderKimi
		p.RemoteSource = &store.RemoteSource{Enabled: true, Host: "example"}
	}, snap, PanelActions{})
	if !tt.HasText("pull failed") {
		t.Fatalf("remote pull error hidden on a quota-only tab; texts %q", tt.Texts())
	}
}

func TestPanelMeterKimiRowsAllPrimaryWithResets(t *testing.T) {
	l := l10n.New(l10n.English)
	now := time.Now()
	snap, st, first := meterTestSnapshot(t, store.ProviderKimi, runway.MeterState{
		Availability: runway.MeterReady,
		Plan:         "Kimi for Coding",
		Meters: []runway.Meter{
			{Title: "5h", RemainingPercent: 70, UsedPercent: 30, ResetsAt: now.Add(3 * time.Hour)},
			{Title: "7d", RemainingPercent: 40, UsedPercent: 60, ResetsAt: now.Add(48 * time.Hour)},
		},
	})
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() {
			panelMeterQuota(c, l, snap, store.ProviderKimi, PanelActions{}, first, st, st.resetSwitch(store.ResetTimeRelative))
		})
	}, 400, 500)
	if !hasExact(tt, "70% left") || !hasExact(tt, "40% left") {
		t.Fatalf("primary percent rows missing; texts %q", tt.Texts())
	}
	if tt.HasText("60% used") {
		t.Fatalf("second row rendered as a slim used row; texts %q", tt.Texts())
	}
	if !hasExact(tt, resetText(l, now.Add(3*time.Hour), now, store.ResetTimeRelative)) ||
		!hasExact(tt, resetText(l, now.Add(48*time.Hour), now, store.ResetTimeRelative)) {
		t.Fatalf("reset lines missing; texts %q", tt.Texts())
	}
}

func TestPanelMeterSpeedBlock(t *testing.T) {
	l := l10n.New(l10n.English)
	snap, st, first := meterTestSnapshot(t, store.ProviderKimi, runway.MeterState{
		Availability: runway.MeterReady,
		Plan:         "Kimi for Coding",
		Meters:       []runway.Meter{{Title: "5h", RemainingPercent: 70, UsedPercent: 30}},
		Speed: &speed.Report{Groups: []speed.Stat{{
			Model: "kimi-k2", Effort: "high", Calls: 5,
			TTFTP50: 1200 * time.Millisecond, Decode: 10 * time.Second, OutputTokens: 2000,
		}}},
	})
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() {
			panelMeterQuota(c, l, snap, store.ProviderKimi, PanelActions{}, first, st, st.resetSwitch(store.ResetTimeRelative))
		})
	}, 400, 600)
	for _, want := range []string{
		tx(l, xSpeedTitle), "kimi-k2 high", "1.2s", "200 tok/s", "5", tx(l, xMeterSpeedSource),
	} {
		if !hasExact(tt, want) {
			t.Fatalf("speed block missing %q; texts %q", want, tt.Texts())
		}
	}
}

func TestPanelMeterSpeedHiddenWithoutData(t *testing.T) {
	l := l10n.New(l10n.English)
	snap, st, first := meterTestSnapshot(t, store.ProviderCursor, runway.MeterState{
		Availability: runway.MeterReady,
		Plan:         "Pro",
		Meters:       []runway.Meter{{Title: "monthly", RemainingPercent: 50, UsedPercent: 50}},
	})
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() {
			panelMeterQuota(c, l, snap, store.ProviderCursor, PanelActions{}, first, st, st.resetSwitch(store.ResetTimeRelative))
		})
	}, 400, 500)
	for _, gone := range []string{tx(l, xSpeedTitle), "tok/s", tx(l, xMeterSpeedSource)} {
		if tt.HasText(gone) {
			t.Fatalf("cursor tab shows speed %q; texts %q", gone, tt.Texts())
		}
	}
	if !hasExact(tt, "50% left") {
		t.Fatalf("quota row missing; texts %q", tt.Texts())
	}
}

func TestPanelMeterSpeedErrorWithoutReport(t *testing.T) {
	l := l10n.New(l10n.English)
	snap, st, first := meterTestSnapshot(t, store.ProviderKimi, runway.MeterState{
		Availability: runway.MeterReady,
		Plan:         "Kimi for Coding",
		Meters:       []runway.Meter{{Title: "5h", RemainingPercent: 70, UsedPercent: 30}},
		SpeedError:   "speed scan failed",
	})
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() {
			panelMeterQuota(c, l, snap, store.ProviderKimi, PanelActions{}, first, st, st.resetSwitch(store.ResetTimeRelative))
		})
	}, 400, 500)
	if !tt.HasText("speed scan failed") {
		t.Fatalf("speed error missing; texts %q", tt.Texts())
	}
	if tt.HasText(tx(l, xSpeedTitle)) {
		t.Fatalf("speed block shown without a report; texts %q", tt.Texts())
	}
}

func TestPanelMeterAgyGroupedRowsAllPrimary(t *testing.T) {
	l := l10n.New(l10n.English)
	now := time.Now()
	reset := now.Add(26 * time.Hour)
	snap, st, first := meterTestSnapshot(t, store.ProviderAgy, runway.MeterState{
		Availability: runway.MeterReady,
		Meters: []runway.Meter{
			{Title: "Weekly Limit Remaining", Group: "Gemini Models",
				GroupDescription: "Models within this group: Gemini Flash, Gemini Pro",
				RemainingPercent: 77, UsedPercent: 22, RemainingCaption: "77.75", ResetsAt: reset},
			{Title: "Five Hour Limit Remaining", Group: "Gemini Models",
				GroupDescription: "Models within this group: Gemini Flash, Gemini Pro",
				RemainingPercent: 100, UsedPercent: 0},
			{Title: "Weekly Limit Remaining", Group: "Claude and GPT models",
				GroupDescription: "Models within this group: Claude Sonnet, GPT-5",
				RemainingPercent: 100, UsedPercent: 0},
			{Title: "Five Hour Limit Remaining", Group: "Claude and GPT models",
				GroupDescription: "Models within this group: Claude Sonnet, GPT-5",
				RemainingPercent: 100, UsedPercent: 0},
		},
	})
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() {
			panelMeterQuota(c, l, snap, store.ProviderAgy, PanelActions{}, first, st, st.resetSwitch(store.ResetTimeRelative))
		})
	}, 400, 700)
	for _, want := range []string{
		"Gemini Models", "Claude and GPT models",
		"Models within this group: Gemini Flash, Gemini Pro",
		"Models within this group: Claude Sonnet, GPT-5",
		"77.75% left", tx(l, xMeterQuotaAvailable),
		resetText(l, reset, now, store.ResetTimeRelative),
	} {
		if !hasExact(tt, want) {
			t.Fatalf("missing %q; texts %q", want, tt.Texts())
		}
	}
	if tt.HasText("77% left") {
		t.Fatalf("integral percent rendered instead of the caption; texts %q", tt.Texts())
	}
	if tt.HasText("% used") {
		t.Fatalf("slim used rows rendered for agy; texts %q", tt.Texts())
	}
}
