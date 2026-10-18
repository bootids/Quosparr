package runway

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/export/swiftjson"
	"github.com/licoy/quosparr/internal/store"
)

type fakeMeter struct {
	mu      sync.Mutex
	state   MeterState
	refresh int
	local   int
}

func (m *fakeMeter) State(time.Time) MeterState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *fakeMeter) Refresh(context.Context) error {
	m.mu.Lock()
	m.refresh++
	m.mu.Unlock()
	return nil
}

func (m *fakeMeter) RefreshLocal(context.Context) error {
	m.mu.Lock()
	m.local++
	m.mu.Unlock()
	return nil
}

func (m *fakeMeter) SetOnChange(func()) {}

func (m *fakeMeter) counts() (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refresh, m.local
}

func meterTestData() MeterState {
	return MeterState{
		Availability: MeterReady, Plan: "pro",
		Meters: []Meter{{Title: "Requests", WindowMinutes: 300, Source: SourceStandard,
			UsedPercent: 40, RemainingPercent: 60, ResetsAt: t0.Add(2 * time.Hour)}},
		Detail:    []DetailLine{{Title: "Limit", Value: "500"}},
		FetchedAt: t0,
	}
}

// A quota-only provider is fetched only when something demands it: selected,
// in the status bar or floating selection, or in the panel's provider strip.
// An undemanded provider is never polled.
func TestMeterProviderRefreshFollowsDemand(t *testing.T) {
	f := newFake(t)
	m := &fakeMeter{state: meterTestData()}
	prefs := testPrefs()
	prefs.PanelProviders = []store.Provider{store.ProviderCodex}
	s, _ := newService(t, f, func(o *Options) {
		o.Preferences = prefs
		o.Meters = map[store.Provider]MeterProvider{store.ProviderCursor: m}
	})
	refreshAll(t, s)
	if r, l := m.counts(); r != 0 || l != 0 {
		t.Fatalf("undemanded meter refreshed %d, local %d", r, l)
	}

	f = newFake(t)
	m = &fakeMeter{state: meterTestData()}
	prefs = testPrefs()
	prefs.PanelProviders = []store.Provider{store.ProviderCodex}
	prefs.SelectedProvider = store.ProviderCursor
	prefs.ExportsStatusJSON = true
	s, _ = newService(t, f, func(o *Options) {
		o.Preferences = prefs
		o.Meters = map[store.Provider]MeterProvider{store.ProviderCursor: m}
	})
	refreshAll(t, s)
	if r, _ := m.counts(); r != 1 {
		t.Fatalf("selected meter refreshed %d times", r)
	}
	if _, l := m.counts(); l != 1 {
		t.Fatalf("meter credential re-detect ran %d times", l)
	}
	snap := s.Snapshot()
	if ms, ok := snap.Meters[store.ProviderCursor]; !ok || ms.Plan != "pro" || len(ms.Meters) != 1 {
		t.Fatalf("meter state missing: %+v", snap.Meters)
	}
}

// The manual quota refresh covers demanded meter providers too.
func TestManualQuotaRefreshCoversDemandedMeters(t *testing.T) {
	f := newFake(t)
	m := &fakeMeter{state: meterTestData()}
	s, _ := newService(t, f, func(o *Options) {
		o.Preferences.SelectedProvider = store.ProviderCursor
		o.Meters = map[store.Provider]MeterProvider{store.ProviderKimi: m}
	})
	refreshAll(t, s)
	wait(t, s.RefreshQuota())
	settle(t, s)
	if r, _ := m.counts(); r != 2 {
		t.Fatalf("kimi refreshed %d times", r)
	}
}

// status.json carries a provider block per meter provider with data, and
// omits the ones without a detected credential.
func TestStatusJSONMeterProviderRoundTrip(t *testing.T) {
	f := newFake(t)
	m := &fakeMeter{state: meterTestData()}
	off := &fakeMeter{state: MeterState{Availability: MeterNotConfigured}}
	s, _ := newService(t, f, func(o *Options) {
		o.Preferences.ExportsStatusJSON = true
		o.Meters = map[store.Provider]MeterProvider{
			store.ProviderCursor: m, store.ProviderZai: off,
		}
	})
	s.mu.Lock()
	snap := s.statusSnapshotLocked(time.Now())
	s.mu.Unlock()
	p := snap.Providers
	if p == nil || p.Cursor == nil {
		t.Fatal("cursor block missing")
	}
	if p.Cursor.Provider != export.ProviderCursor || p.Cursor.Availability != export.Available {
		t.Fatalf("cursor block = %+v", p.Cursor)
	}
	if p.Cursor.Plan == nil || *p.Cursor.Plan != "pro" || len(p.Cursor.Quota) != 1 {
		t.Fatalf("cursor payload = %+v", p.Cursor)
	}
	if p.Zai != nil {
		t.Fatalf("unconfigured zai exported: %+v", p.Zai)
	}
	data, err := snap.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"cursor"`) {
		t.Fatalf("cursor key absent: %s", data)
	}
	if strings.Contains(string(data), `"zai"`) {
		t.Fatalf("zai leaked: %s", data)
	}
}

// The panel block carries the meter providers keyed by provider string, the
// JSON round-trips, and the remote rebuild restores the meter states.
func TestPanelMetersRoundTripAndRemoteReconstruction(t *testing.T) {
	snap := Snapshot{Meters: map[store.Provider]MeterState{
		store.ProviderAgy: {Availability: MeterError},
		store.ProviderCursor: func() MeterState {
			m := meterTestData()
			return m
		}(),
	}}
	p := statusExportPanel(snap)
	if len(p.Meters) != 2 {
		t.Fatalf("meters block = %+v", p.Meters)
	}
	cur := p.Meters["cursor"]
	if cur.Availability != "ready" || cur.Plan != "pro" || len(cur.Meters) != 1 ||
		len(cur.Detail) != 1 || cur.Detail[0].Title != "Limit" || cur.FetchedAt.IsZero() {
		t.Fatalf("cursor panel block = %+v", cur)
	}
	if p.Meters["agy"].Availability != "error" {
		t.Fatalf("agy panel block = %+v", p.Meters["agy"])
	}
	data, err := swiftjson.Marshal(p, false)
	if err != nil {
		t.Fatal(err)
	}
	var back export.StatusPanel
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	again, err := swiftjson.Marshal(&back, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(again) {
		t.Fatalf("round-trip mismatch:\n%s\n%s", data, again)
	}

	prefs := testPrefs()
	remote := BuildRemoteSnapshot(export.StatusSnapshot{Panel: &back}, prefs, en, t0)
	ms, ok := remote.Meters[store.ProviderCursor]
	if !ok {
		t.Fatal("remote cursor meter missing")
	}
	if ms.Availability != MeterReady || ms.Plan != "pro" || len(ms.Meters) != 1 ||
		len(ms.Detail) != 1 || !ms.FetchedAt.Equal(t0) {
		t.Fatalf("remote cursor meter = %+v", ms)
	}
	if agy, ok := remote.Meters[store.ProviderAgy]; !ok || agy.Availability != MeterError {
		t.Fatalf("remote agy meter = %+v", remote.Meters[store.ProviderAgy])
	}

	// A status file without the meters block leaves them empty.
	empty := BuildRemoteSnapshot(export.StatusSnapshot{}, prefs, en, t0)
	if len(empty.Meters) != 0 {
		t.Fatalf("meters rebuilt from nothing: %+v", empty.Meters)
	}
}
