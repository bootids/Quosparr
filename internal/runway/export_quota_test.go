package runway

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/licoy/quosparr/internal/codexapi"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/store"
)

func TestQuotaReadoutIndependentOfModelVisibilityAndErrors(t *testing.T) {
	f := newFake(t)
	s, _ := newService(t, f, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auth = &f.auth
	s.quota = &codexapi.QuotaSnapshot{UpdatedAt: t0,
		Primary:           codexapi.RateWindow{UsedPercent: 12, WindowMinutes: 300},
		AdditionalWindows: []codexapi.NamedRateWindow{{Name: "model", Window: codexapi.RateWindow{UsedPercent: 91}}},
	}
	s.prefs.ShowsModelSpecificQuotaUsage = false
	s.quotaErr = "SECRET"
	snap := s.buildLocked(t0)
	snap.Grok = GrokState{Availability: GrokReady, QuotaFailed: true, UpdatedAt: t0,
		Meters: []Meter{{UsedPercent: 12}}}
	snap.Claude = ClaudeState{Availability: ClaudeAPIKeyMode, QuotaFailed: true}
	r := s.quotaReportLocked(snap, t0)
	p := r.Providers["codex"]
	if len(p.Limits) != 2 || p.Limits[1].Scope != "model_specific" || p.Status != "stale" || p.Error != "fetch_failed" {
		t.Fatalf("codex = %+v", p)
	}
	cl := quotaFromMeters([]Meter{{UsedPercent: 20, WindowMinutes: 300}}, "pro", t0)
	quotaFetchFailed(&cl)
	if cl.Status != "stale" || cl.Limits[0].DurationSeconds != 18000 {
		t.Fatalf("native failure = %+v", cl)
	}
	if r.Providers["grok"].Status != "stale" || r.Providers["claude"].Status != "unsupported" {
		t.Fatalf("native states = %+v", r.Providers)
	}
	snap.Codex.APIKeyMode = true
	if p := s.quotaReportLocked(snap, t0).Providers["codex"]; p.Status != "unsupported" || len(p.Limits) != 0 {
		t.Fatalf("API mode = %+v", p)
	}
}

type notifyingMeter struct {
	fakeMeter
	changed func()
}

func (m *notifyingMeter) SetOnChange(fn func()) { m.changed = fn }

func TestQuotaReadoutUpdatesOnProviderCallbackAndRespectsOptIn(t *testing.T) {
	m := &notifyingMeter{}
	q := export.MeterQuota(meter.State{Status: meter.StatusOK, FetchedAt: t0,
		Balances: []meter.Balance{{Currency: "CNY", Total: 25.5}},
	})
	m.state = MeterState{Availability: MeterReady, Quota: &q}
	s, _ := newService(t, newFake(t), func(o *Options) {
		o.Meters = map[store.Provider]MeterProvider{store.ProviderDeepSeek: m}
	})
	m.changed()
	if _, err := os.Stat(s.opts.StatusPath); !os.IsNotExist(err) {
		t.Fatalf("export off: %v", err)
	}
	s.mu.Lock()
	s.prefs.ExportsStatusJSON = true
	s.mu.Unlock()
	m.changed()
	data, err := os.ReadFile(s.opts.StatusPath)
	if err != nil {
		t.Fatal(err)
	}
	var st export.StatusSnapshot
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	p := st.QuotaReport.Providers["deepseek"]
	if p.Status != "ok" || *p.Balances[0].Total != 25.5 || p.Balances[0].Currency != "CNY" {
		t.Fatalf("provider callback export = %+v", p)
	}
}
