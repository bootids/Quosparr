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

// The floating window follows the Gemini Models group only: its 5-hour and
// weekly windows, never the other-models group (which previously produced a
// second "5h" row).
func TestFloatingAgyShowsOnlyTheGeminiWindows(t *testing.T) {
	geminiWeekly := testMeter("Weekly Limit Remaining", 25, WeeklyMinutes, SourceStandard)
	geminiWeekly.Group = agyGeminiGroup
	geminiFive := testMeter("Five Hour Limit Remaining", 4, FiveHourMinutes, SourceStandard)
	geminiFive.Group = agyGeminiGroup
	otherWeekly := testMeter("Weekly Limit Remaining", 0, WeeklyMinutes, SourceStandard)
	otherWeekly.Group = "Claude and GPT models"
	otherFive := testMeter("Five Hour Limit Remaining", 0, FiveHourMinutes, SourceStandard)
	otherFive.Group = "Claude and GPT models"
	p := store.DefaultPreferences()
	p.FloatingWindow.Providers = []store.Provider{store.ProviderAgy}
	v := BuildFloating(zh, StatusInputs{
		Prefs: p, Now: time.Now(),
		Meters: map[store.Provider]MeterState{store.ProviderAgy: {Availability: MeterReady,
			Meters: []Meter{geminiWeekly, geminiFive, otherWeekly, otherFive}}},
	})
	if len(v.Providers) != 1 || v.Providers[0].Provider != store.ProviderAgy {
		t.Fatalf("providers = %+v", v.Providers)
	}
	item := v.Providers[0]
	if len(item.Meters) != 2 {
		t.Fatalf("meters = %+v, want exactly the two Gemini windows", item.Meters)
	}
	// 5-hour first, then weekly, both from the Gemini group (used 4% and 25%,
	// not the untouched other-models pools at 100%).
	if item.Meters[0].Label != zh.Text(l10n.KeyFiveHourUsage) || item.Meters[0].RemainingPercent != 96 {
		t.Errorf("first meter = %+v, want the Gemini 5-hour window", item.Meters[0])
	}
	if item.Meters[1].Label != zh.Text(l10n.KeyWeeklyUsage) || item.Meters[1].RemainingPercent != 75 {
		t.Errorf("second meter = %+v, want the Gemini weekly window", item.Meters[1])
	}
	if item.Status != "" {
		t.Errorf("status = %q, want none with meters present", item.Status)
	}
}

// A CLI output without the Gemini group keeps every pool: the window must not
// go blank over a renamed group.
func TestFloatingAgyWithoutGeminiGroupKeepsEveryPool(t *testing.T) {
	five := testMeter("Five Hour Limit Remaining", 10, FiveHourMinutes, SourceStandard)
	five.Group = "Renamed group"
	p := store.DefaultPreferences()
	p.FloatingWindow.Providers = []store.Provider{store.ProviderAgy}
	v := BuildFloating(zh, StatusInputs{
		Prefs: p, Now: time.Now(),
		Meters: map[store.Provider]MeterState{store.ProviderAgy: {Availability: MeterReady, Meters: []Meter{five}}},
	})
	if len(v.Providers) != 1 || len(v.Providers[0].Meters) != 1 || v.Providers[0].Meters[0].RemainingPercent != 90 {
		t.Fatalf("providers = %+v", v.Providers)
	}
}

// DeepSeek has balances but no quota pools; its floating item shows the money
// instead of the unknown placeholder.
func TestFloatingBalanceOnlyProviderShowsBalanceText(t *testing.T) {
	p := store.DefaultPreferences()
	p.FloatingWindow.Providers = []store.Provider{store.ProviderDeepSeek}
	v := BuildFloating(zh, StatusInputs{
		Prefs: p, Now: time.Now(),
		Meters: map[store.Provider]MeterState{store.ProviderDeepSeek: {
			Availability: MeterReady, BalanceText: "110 CNY · 12.5 USD",
			Detail: []DetailLine{{Title: "Balance (CNY)", Value: "110"}},
		}},
	})
	if len(v.Providers) != 1 || v.Providers[0].Provider != store.ProviderDeepSeek {
		t.Fatalf("providers = %+v", v.Providers)
	}
	item := v.Providers[0]
	if len(item.Meters) != 0 || item.Status != "110 CNY · 12.5 USD" {
		t.Fatalf("item = %+v, want the balance text as the status", item)
	}
}
