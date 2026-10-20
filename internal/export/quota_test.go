package export

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

func TestMeterQuotaPreservesNumbersAndOmitsUnknowns(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	p := MeterQuota(meter.State{Status: meter.StatusOK, FetchedAt: now,
		Limits: []meter.Limit{{ID: "gemini-5h", Group: "Gemini Models", Unit: "percent",
			Percent: 22.25, HasPercent: true, Used: math.NaN(), LimitValue: math.NaN(),
			Remaining: math.NaN(), WindowMinutes: 300, ResetsAt: now.Add(time.Hour), HasReset: true}},
		Balances: []meter.Balance{{Currency: "USD", Total: 0, Granted: math.NaN(), ToppedUp: math.NaN()}},
	})
	if p.Status != "ok" || *p.Limits[0].UsedPercent != 22.25 || p.Limits[0].DurationSeconds != 18000 ||
		p.Limits[0].Used != nil || p.Balances[0].Total == nil || *p.Balances[0].Total != 0 {
		t.Fatalf("quota = %+v", p)
	}
	data, err := json.Marshal(p)
	if err != nil || strings.Contains(string(data), "NaN") || strings.Contains(string(data), `"used":`) {
		t.Fatalf("encoding = %s, %v", data, err)
	}
	errData, err := json.Marshal(MeterQuota(meter.Failed(meter.StatusAuth, "SECRET upstream body")))
	if err != nil || strings.Contains(string(errData), "SECRET") || !strings.Contains(string(errData), "fetch_failed") {
		t.Fatalf("error encoding = %s, %v", errData, err)
	}
}

func TestQuotaFreshnessAtReadTime(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	old, future, reset := now.Add(-16*time.Minute), now.Add(2*time.Minute), now
	for _, tc := range []struct {
		name   string
		at     *time.Time
		reset  *time.Time
		status string
		err    string
	}{
		{"fresh", &now, nil, "ok", ""},
		{"old", &old, nil, "stale", "max_age_exceeded"},
		{"missing", nil, nil, "stale", "missing_timestamp"},
		{"future", &future, nil, "stale", "clock_skew"},
		{"elapsed reset", &now, &reset, "stale", "reset_elapsed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := QuotaReport{SchemaVersion: 1, GeneratedAt: now, Providers: map[string]QuotaProvider{
				"codex": {Status: "ok", FetchedAt: tc.at, Limits: []QuotaPool{{ResetsAt: tc.reset}}},
			}}
			p := r.At(now, 15*time.Minute).Providers["codex"]
			if p.Status != tc.status || p.Error != tc.err || r.Providers["codex"].Status != "ok" {
				t.Fatalf("provider = %+v, original = %+v", p, r)
			}
		})
	}
	r := QuotaReport{GeneratedAt: old, Providers: map[string]QuotaProvider{
		"kimi": {Status: "ok", FetchedAt: &now},
		"zai":  {Status: "stale", Error: "fetch_failed", FetchedAt: &old},
	}}
	got := r.At(now, 15*time.Minute)
	if got.Providers["kimi"].Status != "stale" || got.Providers["zai"].Error != "fetch_failed" ||
		*got.Providers["zai"].AgeSeconds != 960 {
		t.Fatalf("freshness = %+v", got)
	}
}
