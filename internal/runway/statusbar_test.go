package runway

import (
	"reflect"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

var zh = l10n.New(l10n.SimplifiedChinese)

func testMeter(title string, used, minutes int, src MeterSource) Meter {
	now := time.Now()
	return NewMeter(title, MeterWindow{UsedPercent: used, WindowMinutes: minutes,
		ResetsAt: now.Add(time.Duration(minutes) * time.Minute)}, now, DefaultMarkers(), src)
}

var allStyles = []store.StatusBarStyle{
	store.StatusBarStyleMeters, store.StatusBarStyleCountdown, store.StatusBarStyleBattery,
	store.StatusBarStyleRings, store.StatusBarStyleText,
}

func TestWeeklyOnlyQuotaCompactsEveryStyle(t *testing.T) {
	weekly := testMeter("每周", 11, 10080, SourceStandard)
	for _, st := range allStyles {
		p := MakeRenderPlan(st, store.BatteryScopeBoth, []Meter{weekly})
		if !reflect.DeepEqual(p.Meters, []Meter{weekly}) || !reflect.DeepEqual(p.Columns(), [][]Meter{{weekly}}) {
			t.Errorf("style %s: %+v", st, p)
		}
	}
}

func TestBatteryScopesUseWindowDuration(t *testing.T) {
	weekly := testMeter("每周", 11, 10080, SourceStandard)
	spark := testMeter("GPT-5.3-Codex-Spark", 0, 10080, SourceModelSpecific)
	for _, scope := range []store.BatteryScope{store.BatteryScopeWeekly, store.BatteryScopeFiveHour} {
		p := MakeRenderPlan(store.StatusBarStyleBattery, scope, []Meter{weekly, spark})
		if !reflect.DeepEqual(p.Meters, []Meter{weekly, spark}) {
			t.Errorf("scope %s: %v", scope, p.Meters)
		}
	}
}

func TestThreeVisibleQuotasAreGrouped(t *testing.T) {
	five := testMeter("5小时", 20, 300, SourceStandard)
	weekly := testMeter("每周", 11, 10080, SourceStandard)
	spark := testMeter("GPT-5.3-Codex-Spark", 0, 10080, SourceModelSpecific)
	all := []Meter{five, weekly, spark}
	for _, st := range allStyles {
		p := MakeRenderPlan(st, store.BatteryScopeBoth, all)
		want := [][]Meter{{five}, {weekly}, {spark}}
		if st == store.StatusBarStyleMeters || st == store.StatusBarStyleBattery {
			want = [][]Meter{{five, weekly}, {spark}}
		}
		if !reflect.DeepEqual(p.Meters, all) || !reflect.DeepEqual(p.Columns(), want) {
			t.Errorf("style %s: columns %v", st, p.Columns())
		}
	}
	fiveBattery := MakeRenderPlan(store.StatusBarStyleBattery, store.BatteryScopeFiveHour, all)
	weeklyBattery := MakeRenderPlan(store.StatusBarStyleBattery, store.BatteryScopeWeekly, all)
	if !reflect.DeepEqual(fiveBattery.Meters, []Meter{five, spark}) {
		t.Errorf("five-hour battery: %v", fiveBattery.Meters)
	}
	if !reflect.DeepEqual(weeklyBattery.Meters, []Meter{weekly, spark}) {
		t.Errorf("weekly battery: %v", weeklyBattery.Meters)
	}
}

func TestTextStyleUsesRingPercentages(t *testing.T) {
	five := testMeter("5小时", 20, 300, SourceStandard)
	weekly := testMeter("每周", 11, 10080, SourceStandard)
	prefs := store.DefaultPreferences()
	prefs.StatusBarDisplayStyle = store.StatusBarStyleText
	v := BuildStatusBar(zh, StatusInputs{Prefs: prefs, Provider: store.ProviderCodex,
		CodexText: "6天23小时", CodexMeters: []Meter{five, weekly}, Now: time.Now()})
	if !reflect.DeepEqual(v.TextCaptions, []string{"80%", "89%"}) {
		t.Errorf("captions = %v", v.TextCaptions)
	}
	if RingText(&five) != "80" {
		t.Errorf("ring text = %q", RingText(&five))
	}
	none := BuildStatusBar(zh, StatusInputs{Prefs: prefs, Provider: store.ProviderCodex, CodexText: "x", Now: time.Now()})
	if !reflect.DeepEqual(none.TextCaptions, []string{"--"}) {
		t.Errorf("no-quota captions = %v", none.TextCaptions)
	}
	if v.Tooltip == "" || none.Tooltip != "Quosparr · x" {
		t.Errorf("tooltips %q / %q", v.Tooltip, none.Tooltip)
	}
}

func TestCodexPrefersFiveHourOverWeekly(t *testing.T) {
	five := testMeter("5h", 20, 300, SourceStandard)
	weekly := testMeter("weekly", 40, 10080, SourceStandard)
	pick, ok := PrimaryStandardMeter([]Meter{weekly, five}, codexPreferredWindows)
	if !ok || pick.Title != "5h" {
		t.Fatalf("pick = %+v", pick)
	}
}

func TestCodexFallsBackToWeekly(t *testing.T) {
	weekly := testMeter("weekly", 40, 10080, SourceStandard)
	model := testMeter("gpt", 10, 10080, SourceModelSpecific)
	pick, _ := PrimaryStandardMeter([]Meter{model, weekly}, codexPreferredWindows)
	if pick.Title != "weekly" || pick.Source != SourceStandard {
		t.Fatalf("pick = %+v", pick)
	}
}

func TestGrokPrefersWeeklyOverMonthly(t *testing.T) {
	weekly := testMeter("week", 18, 10080, SourceStandard)
	monthly := testMeter("month", 30, 30*24*60, SourceStandard)
	pick, _ := PrimaryStandardMeter([]Meter{monthly, weekly}, grokPreferredWindows)
	if pick.Title != "week" {
		t.Fatalf("pick = %+v", pick)
	}
}

func TestDualStackOrderAndPrefix(t *testing.T) {
	five := testMeter("5-hour", 20, 300, SourceStandard)
	weekly := testMeter("Weekly", 11, 10080, SourceStandard)
	grokWeek := testMeter("周度包含额度", 42, 10080, SourceStandard)
	grokProduct := testMeter("Build", 10, 10080, SourceStandard)
	m := DualProviderMeters(zh, []Meter{five, weekly}, []Meter{grokWeek, grokProduct}, "Codex", "Grok")
	if len(m) != 2 || m[0].Title != "Codex · 5小时" || m[0].WindowMinutes != 300 ||
		m[1].Title != "Grok · 每周" || m[1].WindowMinutes != 10080 {
		t.Fatalf("meters = %+v", m)
	}
	only := DualProviderMeters(zh, nil, []Meter{grokWeek}, "Codex", "Grok")
	if len(only) != 1 || only[0].Title != "Grok · 每周" {
		t.Fatalf("grok only = %+v", only)
	}
}

func TestClaudeStatusPrefersFiveHour(t *testing.T) {
	five := testMeter("5-hour", 20, 300, SourceStandard)
	week := testMeter("7-day", 11, 10080, SourceStandard)
	in := StatusInputs{
		Prefs:       store.Preferences{StatusBarProviderScope: store.ProviderScopeCustom, StatusBarProviders: []store.Provider{store.ProviderCodex, store.ProviderClaude}},
		Provider:    store.ProviderCodex,
		CodexMeters: []Meter{five},
		Claude:      ClaudeState{Meters: []Meter{week, five}},
		Now:         time.Now(),
	}
	meters := SelectedMeters(zh, in)
	if len(meters) != 2 || meters[1].WindowMinutes != 300 || meters[1].Title != "Claude · 5小时" {
		t.Fatalf("meters = %+v", meters)
	}
}

func TestClaudeAloneShowsStandardMeters(t *testing.T) {
	five := testMeter("5-hour", 20, 300, SourceStandard)
	week := testMeter("7-day", 11, 10080, SourceStandard)
	opus := testMeter("Opus", 5, 10080, SourceModelSpecific)
	in := StatusInputs{
		Prefs:    store.Preferences{StatusBarProviderScope: store.ProviderScopeCustom, StatusBarProviders: []store.Provider{store.ProviderClaude}},
		Provider: store.ProviderCodex,
		Claude:   ClaudeState{Meters: []Meter{five, week, opus}},
		Now:      time.Now(),
	}
	meters := SelectedMeters(zh, in)
	if len(meters) != 2 || meters[0].Title != "5小时" || meters[1].Title != "每周" {
		t.Fatalf("meters = %+v", meters)
	}
}

func TestPairTextFallsBackToSecondProvider(t *testing.T) {
	five := testMeter("5-hour", 20, 300, SourceStandard)
	in := StatusInputs{
		Prefs:     store.Preferences{StatusBarProviderScope: store.ProviderScopeCustom, StatusBarProviders: []store.Provider{store.ProviderGrok, store.ProviderClaude}},
		Provider:  store.ProviderCodex,
		Grok:      GrokState{Availability: GrokNotLoggedIn},
		Claude:    ClaudeState{Availability: ClaudeReady, Meters: []Meter{five}},
		CodexText: "codex",
		Now:       time.Now(),
	}
	if got, want := SelectedStatusText(zh, in), ClaudeStatusText(zh, in.Claude, in.Now); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestMeterProviderSingleSelectionShowsFiveHour(t *testing.T) {
	five := testMeter("5小时", 20, 300, SourceStandard)
	weekly := testMeter("每周", 40, 10080, SourceStandard)
	in := StatusInputs{
		Prefs:    store.Preferences{StatusBarProviderScope: store.ProviderScopeCustom, StatusBarProviders: []store.Provider{store.ProviderZai}},
		Provider: store.ProviderCodex,
		Meters:   map[store.Provider]MeterState{store.ProviderZai: {Availability: MeterReady, Meters: []Meter{five, weekly}}},
		Now:      time.Now(),
	}
	meters := SelectedMeters(zh, in)
	if len(meters) != 1 || meters[0].WindowMinutes != 300 || meters[0].Title != "5小时" {
		t.Fatalf("meters = %+v", meters)
	}
	if got, want := SelectedStatusText(zh, in), MeterStatusText(zh, in.Meters[store.ProviderZai], in.Now); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestMeterProviderPairCombinesLabeledMeters(t *testing.T) {
	five := testMeter("5小时", 20, 300, SourceStandard)
	weekly := testMeter("每周", 40, 10080, SourceStandard)
	in := StatusInputs{
		Prefs:       store.Preferences{StatusBarProviderScope: store.ProviderScopeCustom, StatusBarProviders: []store.Provider{store.ProviderCodex, store.ProviderZai}},
		Provider:    store.ProviderCodex,
		CodexText:   "80%",
		CodexMeters: []Meter{five},
		Meters:      map[store.Provider]MeterState{store.ProviderZai: {Availability: MeterReady, Meters: []Meter{weekly}}},
		Now:         time.Now(),
	}
	meters := SelectedMeters(zh, in)
	if len(meters) != 2 ||
		meters[0].Title != zh.Text(l10n.KeyProviderCodex)+" · 5小时" ||
		meters[1].Title != zh.Text(l10n.KeyProviderZai)+" · 每周" ||
		meters[1].WindowMinutes != 10080 {
		t.Fatalf("meters = %+v", meters)
	}
}

func TestMeterProviderNotConfiguredShowsHint(t *testing.T) {
	in := StatusInputs{
		Prefs:    store.Preferences{StatusBarProviderScope: store.ProviderScopeCustom, StatusBarProviders: []store.Provider{store.ProviderZai}},
		Provider: store.ProviderCodex,
		Meters:   map[store.Provider]MeterState{store.ProviderZai: {Availability: MeterNotConfigured}},
		Now:      time.Now(),
	}
	if got, want := SelectedStatusText(zh, in), zh.Text(l10n.KeyMeterNotConfigured); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if meters := SelectedMeters(zh, in); len(meters) != 0 {
		t.Fatalf("meters = %+v, want none", meters)
	}
}

func TestMeterPairWithoutMetersFallsBackToSecond(t *testing.T) {
	five := testMeter("5-hour", 20, 300, SourceStandard)
	in := StatusInputs{
		Prefs:     store.Preferences{StatusBarProviderScope: store.ProviderScopeCustom, StatusBarProviders: []store.Provider{store.ProviderZai, store.ProviderClaude}},
		Provider:  store.ProviderCodex,
		Meters:    map[store.Provider]MeterState{store.ProviderZai: {Availability: MeterNotConfigured}},
		Claude:    ClaudeState{Availability: ClaudeReady, Meters: []Meter{five}},
		CodexText: "codex",
		Now:       time.Now(),
	}
	if got, want := SelectedStatusText(zh, in), ClaudeStatusText(zh, in.Claude, in.Now); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestMeterAmountPoolShowsValueText(t *testing.T) {
	amount := testMeter("Amount", 0, 0, SourceStandard)
	amount.ValueText = "$3.20"
	amount.ResetsAt = time.Time{}
	if got := percentText(amount); got != "$3.20" {
		t.Fatalf("percentText = %q", got)
	}
	if got := MeterStatusText(zh, MeterState{Availability: MeterReady, Meters: []Meter{amount}}, time.Now()); got != "$3.20" {
		t.Fatalf("status text = %q", got)
	}
}

func TestShortWindowTitlesMatchCodexWording(t *testing.T) {
	for minutes, want := range map[int]string{300: "5小时", 10080: "每周", 30 * 24 * 60: "每月"} {
		if got := ShortWindowTitle(zh, minutes); got != want {
			t.Errorf("%d min = %q, want %q", minutes, got, want)
		}
	}
	long := testMeter("周度包含额度", 27, 10080, SourceStandard)
	if WithShortTitle(zh, long).Title != "每周" {
		t.Error("long title not shortened")
	}
}

func TestShortestWindowMeterPrefersShortestStandardWindow(t *testing.T) {
	weekly := testMeter("weekly", 10, WeeklyMinutes, SourceStandard)
	five := testMeter("5h", 40, FiveHourMinutes, SourceStandard)
	model := testMeter("model", 90, 60, SourceModelSpecific)
	unknown := testMeter("unknown", 20, 0, SourceStandard)
	cases := []struct {
		in   []Meter
		want string
	}{
		{[]Meter{weekly, five, model}, "5h"},
		{[]Meter{unknown, weekly}, "weekly"},
		{[]Meter{unknown}, "unknown"},
		{[]Meter{model}, "model"},
	}
	for _, tc := range cases {
		if m, ok := ShortestWindowMeter(tc.in); !ok || m.Title != tc.want {
			t.Errorf("got %q, want %q", m.Title, tc.want)
		}
	}
	if _, ok := ShortestWindowMeter(nil); ok {
		t.Error("empty meters picked a meter")
	}
}
