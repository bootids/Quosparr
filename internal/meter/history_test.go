package meter

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func histState(lims ...Limit) State { return State{Status: StatusOK, Limits: lims} }

func usedLimit(id string, used, limit float64) Limit {
	return Limit{ID: id, Title: id + " title", Unit: "tokens", Used: used, LimitValue: limit}
}

func rateOf(t *testing.T, rates []Rate, id string) Rate {
	t.Helper()
	for _, r := range rates {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no rate for pool %q in %+v", id, rates)
	return Rate{}
}

func TestHistoryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h := LoadHistory(path)
	if err := h.Record("p", histState(usedLimit("a", 10, 100)), now); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if err := h.Record("p", histState(usedLimit("a", 10+float64(i)*6, 100)), now.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	h2 := LoadHistory(path)
	pf := h2.providers["p"]["a"]
	if pf == nil || len(pf.Samples) != 6 {
		t.Fatalf("samples after reload = %+v", h2.providers)
	}
	if pf.Meta.Title != "a title" || pf.Meta.Unit != "tokens" {
		t.Fatalf("meta = %+v", pf.Meta)
	}
	r := rateOf(t, h2.Rates("p", now.Add(6*time.Hour)), "a")
	if !r.HasRate || r.Samples != 6 {
		t.Fatalf("rate = %+v", r)
	}
	if math.Abs(r.PerHour-6) > 1e-6 {
		t.Fatalf("PerHour = %v want 6", r.PerHour)
	}
	want := now.Add(5 * time.Hour).Add(time.Duration((100 - 40) / 6 * float64(time.Hour)))

	if !r.ExhaustsAt.Equal(want) {
		t.Fatalf("ExhaustsAt = %v want %v", r.ExhaustsAt, want)
	}
	if r.IsPercent {
		t.Fatal("IsPercent should be false")
	}
}

func TestHistoryResetSplitsSegment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := LoadHistory(path)
	// Pre-reset: 50..80 over 4h, then reset to 10.
	for i, u := range []float64{50, 60, 70, 80} {
		if err := h.Record("p", histState(usedLimit("a", u, 100)), now.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	for i, u := range []float64{10, 20} {
		if err := h.Record("p", histState(usedLimit("a", u, 100)), now.Add(time.Duration(4+i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	r := rateOf(t, h.Rates("p", now.Add(6*time.Hour)), "a")
	if r.Samples != 2 {
		t.Fatalf("Samples = %d want 2 (post-reset only)", r.Samples)
	}
	if !r.From.Equal(now.Add(4*time.Hour)) || !r.To.Equal(now.Add(5*time.Hour)) {
		t.Fatalf("span = %v..%v", r.From, r.To)
	}
	// 2-sample segment spans 1h: endpoint slope = 10/h.
	if r.PerHour != 10 {
		t.Fatalf("PerHour = %v want 10", r.PerHour)
	}
}

func TestHistoryMissedResetCutsSegment(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "h.json"))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 30 -> 5 is a real decrease (missed reset): segment starts at the drop.
	for i, u := range []float64{10, 20, 30, 5, 15, 25} {
		if err := h.Record("p", histState(usedLimit("a", u, 100)), now.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	r := rateOf(t, h.Rates("p", now.Add(6*time.Hour)), "a")
	if r.Samples != 3 {
		t.Fatalf("Samples = %d want 3 (post-decrease only)", r.Samples)
	}
	if !r.From.Equal(now.Add(3*time.Hour)) || !r.To.Equal(now.Add(5*time.Hour)) {
		t.Fatalf("span = %v..%v", r.From, r.To)
	}
	if r.PerHour != 10 {
		t.Fatalf("PerHour = %v want 10 (5->25 over 2h)", r.PerHour)
	}
}

func TestHistoryFloatNoiseTolerated(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "h.json"))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 0.3 dip < 0.5% of limit 100 = 0.5 tolerance: noise, not a reset.
	vals := []float64{20, 40.2, 39.9, 60}
	for i, u := range vals {
		if err := h.Record("p", histState(usedLimit("a", u, 100)), now.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	r := rateOf(t, h.Rates("p", now.Add(4*time.Hour)), "a")
	if r.Samples != 4 {
		t.Fatalf("Samples = %d want 4 (noise tolerated)", r.Samples)
	}
}

func TestHistoryPercentOnlyPool(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "h.json"))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= 3; i++ {
		st := histState(Limit{ID: "pct", Title: "Pct", Unit: "percent",
			Used: math.NaN(), LimitValue: math.NaN(),
			Percent: float64(10 + i*10), HasPercent: true})
		if err := h.Record("p", st, now.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	r := rateOf(t, h.Rates("p", now.Add(3*time.Hour)), "pct")
	if !r.IsPercent {
		t.Fatal("IsPercent should be true")
	}
	if r.PerHour != 10 {
		t.Fatalf("PerHour = %v want 10", r.PerHour)
	}
	if !r.HasExhaust {
		t.Fatalf("no exhaust: %+v", r)
	}
	// 40% at 10/h → 6h to 100%.
	want := now.Add(3 * time.Hour).Add(6 * time.Hour)
	if !r.ExhaustsAt.Equal(want) {
		t.Fatalf("ExhaustsAt = %v want %v", r.ExhaustsAt, want)
	}
}

func TestHistoryShortSpanNoRate(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "h.json"))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= 2; i++ {
		if err := h.Record("p", histState(usedLimit("a", float64(10+i*5), 100)), now.Add(time.Duration(i)*10*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	r := rateOf(t, h.Rates("p", now.Add(30*time.Minute)), "a")
	if r.HasRate || r.HasExhaust || r.HasAtReset {
		t.Fatalf("rate should be unusable: %+v", r)
	}
	if r.PerHour != 0 {
		t.Fatalf("PerHour = %v want 0", r.PerHour)
	}
}

func TestHistoryNegativeRateNoExhaust(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "h.json"))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Flat series: slope 0.
	for i := 0; i <= 2; i++ {
		if err := h.Record("p", histState(usedLimit("a", 50, 100)), now.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	r := rateOf(t, h.Rates("p", now.Add(3*time.Hour)), "a")
	if !r.HasRate || r.PerHour != 0 {
		t.Fatalf("rate = %+v", r)
	}
	if r.HasExhaust {
		t.Fatal("zero rate must not project exhaustion")
	}
}

func TestHistoryAtResetProjectionAndClamp(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	record := func(path string, vals []float64, resetsAt time.Time) *History {
		h := LoadHistory(path)
		for i, u := range vals {
			lim := usedLimit("a", u, 100)
			lim.HasReset = true
			lim.ResetsAt = resetsAt
			if err := h.Record("p", histState(lim), now.Add(time.Duration(i)*30*time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
		return h
	}
	end := now.Add(3 * time.Hour)

	// 20/h, newest 40 at +1h, reset +2h (1h ahead) → 40 + 20 = 60 (unclamped).
	r := rateOf(t, record(filepath.Join(t.TempDir(), "h.json"), []float64{20, 30, 40}, reset).Rates("p", end), "a")
	if !r.HasReset || !r.ResetsAt.Equal(reset) {
		t.Fatalf("reset fields = %+v", r)
	}
	if !r.HasAtReset || r.AtReset != 60 {
		t.Fatalf("AtReset = %v/%v want 60", r.AtReset, r.HasAtReset)
	}

	// 30/h overshoots the limit before reset: clamp at 100.
	r2 := rateOf(t, record(filepath.Join(t.TempDir(), "h2.json"), []float64{20, 50, 80}, reset).Rates("p", end), "a")
	if !r2.HasAtReset || r2.AtReset != 100 {
		t.Fatalf("AtReset = %v/%v want clamped 100", r2.AtReset, r2.HasAtReset)
	}

	// Reset already passed relative to newest sample: no AtReset.
	r3 := rateOf(t, record(filepath.Join(t.TempDir(), "h3.json"), []float64{20, 30, 40}, now.Add(30*time.Minute)).Rates("p", end), "a")
	if r3.HasAtReset {
		t.Fatalf("AtReset should be absent: %+v", r3)
	}
}

func TestHistoryMinSpacing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := LoadHistory(path)
	if err := h.Record("p", histState(usedLimit("a", 10, 100)), now); err != nil {
		t.Fatal(err)
	}
	if err := h.Record("p", histState(usedLimit("a", 20, 100)), now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(h.providers["p"]["a"].Samples) != 1 {
		t.Fatalf("samples = %d want 1 (4m spacing skipped)", len(h.providers["p"]["a"].Samples))
	}
	if err := h.Record("p", histState(usedLimit("a", 20, 100)), now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(h.providers["p"]["a"].Samples) != 2 {
		t.Fatalf("samples = %d want 2 (5m spacing recorded)", len(h.providers["p"]["a"].Samples))
	}
}

func TestHistoryPrunesOldSamples(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "h.json"))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := now.Add(-31 * 24 * time.Hour)
	if err := h.Record("p", histState(usedLimit("a", 1, 100)), old); err != nil {
		t.Fatal(err)
	}
	if err := h.Record("p", histState(usedLimit("a", 2, 100)), now.Add(-29*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.Record("p", histState(usedLimit("a", 3, 100)), now); err != nil {
		t.Fatal(err)
	}
	if n := len(h.providers["p"]["a"].Samples); n != 2 {
		t.Fatalf("samples = %d want 2 after 30-day prune", n)
	}
}

func TestHistorySampleCap(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "h.json"))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pf := &poolFile{Meta: PoolMeta{Title: "a", Unit: "tokens"}}
	for i := 0; i < historyMaxSamples+10; i++ {
		pf.Samples = append(pf.Samples, Sample{At: now.Add(time.Duration(i) * 5 * time.Minute), HasUsed: true, Used: float64(i), HasLimit: true, LimitValue: 1e9})
	}
	h.providers["p"] = map[string]*poolFile{"a": pf}
	if err := h.Record("p", histState(usedLimit("a", 1e6, 1e9)), now.Add(time.Duration(historyMaxSamples+10)*5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got := h.providers["p"]["a"]
	if n := len(got.Samples); n != historyMaxSamples {
		t.Fatalf("samples = %d want %d", n, historyMaxSamples)
	}
	if want := now.Add(11 * 5 * time.Minute); !got.Samples[0].At.Equal(want) {
		t.Fatalf("oldest = %v want %v", got.Samples[0].At, want)
	}
}

func TestHistoryCorruptFileTolerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"providers":{`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := LoadHistory(path)
	if h == nil {
		t.Fatal("history must be non-nil")
	}
	if rates := h.Rates("p", time.Now()); len(rates) != 0 {
		t.Fatalf("rates = %+v", rates)
	}
	// Still usable after corruption: Record works and rewrites the file.
	if err := h.Record("p", histState(usedLimit("a", 1, 10)), time.Now()); err != nil {
		t.Fatal(err)
	}
	h2 := LoadHistory(path)
	if len(h2.providers["p"]["a"].Samples) != 1 {
		t.Fatalf("samples = %+v", h2.providers)
	}
}

func TestHistorySkipsPoolWithoutUsedOrPercent(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "h.json"))
	lim := Limit{ID: "empty", Title: "Empty", Unit: "unknown", Used: math.NaN(), LimitValue: 100}
	if err := h.Record("p", histState(lim), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.providers["p"]["empty"]; ok {
		t.Fatal("pool without used/percent should be skipped")
	}
}

func TestHistoryFileSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := LoadHistory(path)
	if err := h.Record("p", histState(usedLimit("a", 10, 100)), now); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]any
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f["version"] != float64(1) {
		t.Fatalf("version = %v", f["version"])
	}
	pool, ok := f["providers"].(map[string]any)["p"].(map[string]any)["a"].(map[string]any)
	if !ok {
		t.Fatalf("schema = %s", data)
	}
	if meta, ok := pool["meta"].(map[string]any); !ok || meta["title"] != "a title" {
		t.Fatalf("meta = %+v", pool["meta"])
	}
	if _, ok := pool["samples"].([]any); !ok {
		t.Fatalf("samples missing: %s", data)
	}
}

func TestHistoryRatesSortedByID(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "h.json"))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"c", "a", "b"} {
		if err := h.Record("p", histState(usedLimit(id, float64(10+i), 100)), now); err != nil {
			t.Fatal(err)
		}
		if err := h.Record("p", histState(usedLimit(id, float64(20+i), 100)), now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	rates := h.Rates("p", now.Add(time.Hour))
	if len(rates) != 3 || rates[0].ID != "a" || rates[1].ID != "b" || rates[2].ID != "c" {
		t.Fatalf("rates order = %+v", rates)
	}
	h2 := LoadHistory(filepath.Join(t.TempDir(), "h2.json"))
	for step, u := range []float64{1, 2} {
		st := State{Status: StatusOK, Limits: []Limit{
			usedLimit("zz", u, 10), usedLimit("aa", u, 10), usedLimit("mm", u, 10),
		}}
		if err := h2.Record("q", st, now.Add(time.Duration(step)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	rates = h2.Rates("q", now.Add(2*time.Hour))
	if len(rates) != 3 || rates[0].ID != "aa" || rates[1].ID != "mm" || rates[2].ID != "zz" {
		t.Fatalf("rates order = %+v", rates)
	}
	if rates := h2.Rates("missing", now); len(rates) != 0 {
		t.Fatalf("rates for unknown provider = %+v", rates)
	}
}
