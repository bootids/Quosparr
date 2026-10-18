package runway

import (
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

func TestFloatingShowsTwoStandardWindowsPerProvider(t *testing.T) {
	weekly := testMeter("每周", 60, WeeklyMinutes, SourceStandard)
	five := testMeter("5小时", 20, FiveHourMinutes, SourceStandard)
	spark := testMeter("Spark", 0, WeeklyMinutes, SourceModelSpecific)
	p := store.DefaultPreferences()
	p.FloatingWindow.Providers = []store.Provider{store.ProviderCodex, store.ProviderClaude}
	v := BuildFloating(zh, StatusInputs{
		Prefs: p, Now: time.Now(),
		CodexText: "80%", CodexMeters: []Meter{weekly, spark, five},
		Claude: ClaudeState{Availability: ClaudeNotLoggedIn},
	})
	if len(v.Providers) != 2 {
		t.Fatalf("providers = %+v", v.Providers)
	}
	codex := v.Providers[0]
	if codex.Provider != store.ProviderCodex || len(codex.Meters) != 2 || codex.Status != "" {
		t.Fatalf("codex = %+v", codex)
	}
	if codex.Meters[0].RemainingPercent != 80 || codex.Meters[1].RemainingPercent != 40 || codex.Meters[0].Reset == "" {
		t.Errorf("codex meters = %+v, want the 5-hour window first", codex.Meters)
	}
	claude := v.Providers[1]
	if len(claude.Meters) != 0 || claude.Status == "" {
		t.Errorf("claude = %+v, want a status without meters", claude)
	}
}

func TestFloatingFollowsThePanelProvider(t *testing.T) {
	p := store.DefaultPreferences()
	p.SelectedProvider = store.ProviderGrok
	monthly := testMeter("每月", 10, MonthlyMinutes, SourceStandard)
	v := BuildFloating(zh, StatusInputs{Prefs: p, Now: time.Now(), Grok: GrokState{Availability: GrokReady, Meters: []Meter{monthly}}})
	if len(v.Providers) != 1 || v.Providers[0].Provider != store.ProviderGrok || len(v.Providers[0].Meters) != 1 {
		t.Errorf("view = %+v", v)
	}
}

func TestFloatingMeterProviderShowsStandardWindows(t *testing.T) {
	five := testMeter("5小时", 20, 300, SourceStandard)
	weekly := testMeter("每周", 40, 10080, SourceStandard)
	p := store.DefaultPreferences()
	p.FloatingWindow.Providers = []store.Provider{store.ProviderZai}
	v := BuildFloating(zh, StatusInputs{
		Prefs: p, Now: time.Now(),
		Meters: map[store.Provider]MeterState{store.ProviderZai: {Availability: MeterReady, Meters: []Meter{weekly, five}}},
	})
	if len(v.Providers) != 1 || v.Providers[0].Provider != store.ProviderZai {
		t.Fatalf("providers = %+v", v.Providers)
	}
	item := v.Providers[0]
	if item.Name != zh.Text(l10n.KeyProviderZai) {
		t.Errorf("name = %q", item.Name)
	}
	if len(item.Meters) != 2 || item.Meters[0].RemainingPercent != 80 || item.Meters[1].RemainingPercent != 60 ||
		item.Meters[0].Reset == "" || item.Status != "" {
		t.Fatalf("meters = %+v, status = %q", item.Meters, item.Status)
	}
}

func TestFloatingMeterProviderNotConfiguredShowsStatus(t *testing.T) {
	p := store.DefaultPreferences()
	p.FloatingWindow.Providers = []store.Provider{store.ProviderKimi}
	v := BuildFloating(zh, StatusInputs{
		Prefs: p, Now: time.Now(),
		Meters: map[store.Provider]MeterState{store.ProviderKimi: {Availability: MeterNotConfigured}},
	})
	if len(v.Providers) != 1 || len(v.Providers[0].Meters) != 0 ||
		v.Providers[0].Status != zh.Text(l10n.KeyMeterNotConfigured) {
		t.Fatalf("providers = %+v", v.Providers)
	}
}

func TestFloatingWithOnlyModelSpecificWindowShowsIt(t *testing.T) {
	spark := testMeter("Spark", 30, WeeklyMinutes, SourceModelSpecific)
	if got := floatingMeters([]Meter{spark}, codexPreferredWindows); len(got) != 1 || got[0].Title != "Spark" {
		t.Errorf("meters = %+v", got)
	}
	if got := floatingMeters(nil, codexPreferredWindows); len(got) != 0 {
		t.Errorf("no meters = %+v", got)
	}
}
