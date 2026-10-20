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
	"github.com/licoy/quosparr/internal/speed"
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
		Detail: []DetailLine{{Title: "Limit", Value: "500"}},
		Rates: []MeterRate{{
			ID: "5h", Title: "5-hour requests", Unit: "req",
			IsPercent: true, PerHour: 12.5, HasRate: true, Samples: 9,
			From: t0.Add(-time.Hour), To: t0,
			ExhaustsAt: t0.Add(90 * time.Minute), HasExhaust: true,
			ResetsAt: t0.Add(3 * time.Hour), HasReset: true,
			AtReset: 420, HasAtReset: true,
		}},
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
	if snap.QuotaReport.Providers["zai"].Status != "not_configured" {
		t.Fatal("quota readout must explicitly mark unconfigured providers")
	}
}

// The panel block carries the meter providers keyed by provider string, the
// JSON round-trips, and the remote rebuild restores the meter states.
func TestPanelMetersRoundTripAndRemoteReconstruction(t *testing.T) {
	snap := Snapshot{Meters: map[store.Provider]MeterState{
		store.ProviderAgy: {Availability: MeterError},
		store.ProviderCursor: func() MeterState {
			m := meterTestData()
			m.Speed = &speed.Report{
				Groups: []speed.Stat{{
					Model: "kimi-k2", Calls: 3,
					TTFTP50: 800 * time.Millisecond, TTFTP90: 2 * time.Second,
					OutputTokens: 120, Decode: 4 * time.Second,
				}},
				Sessions: []speed.SessionStat{{
					ID: "s1", Title: "fix login", ProjectName: "Quosparr",
					UpdatedAt: t0.Add(-time.Minute),
					Groups:    []speed.Stat{{Model: "kimi-k2", Calls: 2, TTFTP50: 900 * time.Millisecond}},
				}},
				From: t0.Add(-time.Hour), To: t0, CalculatedAt: t0,
			}
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
	if len(cur.Rates) != 1 || cur.Rates[0] != (export.MeterPanelRate{
		ID: "5h", Title: "5-hour requests", Unit: "req",
		IsPercent: true, PerHour: 12.5, HasRate: true, Samples: 9,
		From: t0.Add(-time.Hour), To: t0,
		ExhaustsAt: t0.Add(90 * time.Minute), HasExhaust: true,
		ResetsAt: t0.Add(3 * time.Hour), HasReset: true,
		AtReset: 420, HasAtReset: true,
	}) {
		t.Fatalf("cursor rates = %+v", cur.Rates)
	}
	if cur.Speed == nil || len(cur.Speed.Groups) != 1 || len(cur.Speed.Sessions) != 1 ||
		cur.Speed.Groups[0].Model != "kimi-k2" || cur.Speed.Groups[0].TTFTP50NS != int64(800*time.Millisecond) {
		t.Fatalf("cursor speed = %+v", cur.Speed)
	}
	if p.Meters["agy"].Speed != nil {
		t.Fatalf("agy carried a speed report: %+v", p.Meters["agy"].Speed)
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
	if len(ms.Rates) != 1 || ms.Rates[0] != meterTestData().Rates[0] {
		t.Fatalf("remote cursor rates = %+v", ms.Rates)
	}
	if ms.Speed == nil || ms.Speed.Groups[0].Model != "kimi-k2" ||
		ms.Speed.Groups[0].TTFTP50 != 800*time.Millisecond || ms.Speed.Groups[0].Calls != 3 {
		t.Fatalf("remote cursor speed = %+v", ms.Speed)
	}
	agy, ok := remote.Meters[store.ProviderAgy]
	if !ok || agy.Availability != MeterError {
		t.Fatalf("remote agy meter = %+v", remote.Meters[store.ProviderAgy])
	}
	if agy.Speed != nil {
		t.Fatalf("remote agy speed = %+v", agy.Speed)
	}

	// A status file without the meters block leaves them empty.
	empty := BuildRemoteSnapshot(export.StatusSnapshot{}, prefs, en, t0)
	if len(empty.Meters) != 0 {
		t.Fatalf("meters rebuilt from nothing: %+v", empty.Meters)
	}
}

// A panel block produced without the rates field (an older producer) decodes
// to an empty Rates slice, and a meter state without anything to export stays
// out of the files.
func TestPanelMeterRatesBackCompat(t *testing.T) {
	prefs := testPrefs()
	panel := &export.StatusPanel{Meters: map[string]export.MeterPanelBlock{
		"cursor": {Availability: "ready", Plan: "pro"},
	}}
	remote := BuildRemoteSnapshot(export.StatusSnapshot{Panel: panel}, prefs, en, t0)
	ms, ok := remote.Meters[store.ProviderCursor]
	if !ok || ms.Availability != MeterReady || len(ms.Rates) != 0 {
		t.Fatalf("rates from older producer = %+v", remote.Meters)
	}
	if meterHasData(MeterState{}) {
		t.Fatal("empty meter state counted as data")
	}
	if meterHasData(MeterState{Availability: MeterNotConfigured, Rates: []MeterRate{{ID: "x"}}}) {
		t.Fatal("unconfigured meter state counted as data")
	}
	if p := statusExportPanel(Snapshot{Meters: map[store.Provider]MeterState{
		store.ProviderCursor: {Availability: MeterNotConfigured},
	}}); len(p.Meters) != 0 {
		t.Fatalf("unconfigured provider exported: %+v", p.Meters)
	}
}

// The agy panel meters keep their model-group section and the exact
// remaining-percent caption through export and the remote rebuild.
func TestPanelMeterGroupFieldsRoundTrip(t *testing.T) {
	agyMeter := Meter{
		Title: "Weekly Limit Remaining", WindowMinutes: 10080, Source: SourceStandard,
		Group: "Gemini Models", GroupDescription: "Models within this group: Gemini Flash, Gemini Pro",
		RemainingCaption: "77.75", UsedPercent: 22, RemainingPercent: 77,
		ResetsAt: t0.Add(26 * time.Hour), Health: HealthForUsedPercent(22), MarkerPercents: DefaultMarkers(),
	}
	snap := Snapshot{Meters: map[store.Provider]MeterState{
		store.ProviderAgy: {Availability: MeterReady, Meters: []Meter{agyMeter}, FetchedAt: t0},
	}}
	p := statusExportPanel(snap)
	b := p.Meters["agy"]
	if len(b.Meters) != 1 {
		t.Fatalf("agy meters = %+v", b.Meters)
	}
	q := b.Meters[0]
	if q.Group == nil || *q.Group != agyMeter.Group || q.GroupDescription == nil || *q.GroupDescription != agyMeter.GroupDescription ||
		q.RemainingCaption == nil || *q.RemainingCaption != agyMeter.RemainingCaption {
		t.Fatalf("exported row = %+v", q)
	}
	data, err := swiftjson.Marshal(p, false)
	if err != nil {
		t.Fatal(err)
	}
	var back export.StatusPanel
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	remote := BuildRemoteSnapshot(export.StatusSnapshot{Panel: &back}, testPrefs(), en, t0)
	ms, ok := remote.Meters[store.ProviderAgy]
	if !ok || len(ms.Meters) != 1 {
		t.Fatalf("remote agy meter = %+v", remote.Meters[store.ProviderAgy])
	}
	m := ms.Meters[0]
	if m.Group != agyMeter.Group || m.GroupDescription != agyMeter.GroupDescription || m.RemainingCaption != agyMeter.RemainingCaption {
		t.Fatalf("remote row = %+v", m)
	}
}

// The compact balance summary of a pool-less provider survives export and the
// remote rebuild, so the remote floating window shows it too.
func TestPanelMeterBalanceTextRoundTrip(t *testing.T) {
	snap := Snapshot{Meters: map[store.Provider]MeterState{
		store.ProviderDeepSeek: {Availability: MeterReady, BalanceText: "110 CNY · 12.5 USD", FetchedAt: t0},
	}}
	p := statusExportPanel(snap)
	b, ok := p.Meters["deepseek"]
	if !ok || b.BalanceText != "110 CNY · 12.5 USD" {
		t.Fatalf("panel block = %+v", p.Meters["deepseek"])
	}
	data, err := swiftjson.Marshal(p, false)
	if err != nil {
		t.Fatal(err)
	}
	var back export.StatusPanel
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	remote := BuildRemoteSnapshot(export.StatusSnapshot{Panel: &back}, testPrefs(), en, t0)
	ms, ok := remote.Meters[store.ProviderDeepSeek]
	if !ok || ms.BalanceText != "110 CNY · 12.5 USD" {
		t.Fatalf("remote balance text = %+v", remote.Meters[store.ProviderDeepSeek])
	}
}
