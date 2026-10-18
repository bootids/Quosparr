package export

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/export/swiftjson"
	"github.com/licoy/quosparr/internal/sessions"
)

// statusProvidersSnapshot is a full status snapshot with all three providers
// and the reset-today summary, the shape testdata/status_providers_full.json
// spells out.
func statusProvidersSnapshot() StatusSnapshot {
	at := func(h, m int) *time.Time {
		t := utc(2026, 10, 18, h, m, 0)
		return &t
	}
	window := func(title string, minutes int, source QuotaSource, used int, resets *time.Time) WidgetQuota {
		return NewWidgetQuota(title, &minutes, source, used, 100-used, resets)
	}
	balance := swiftjson.MustDecimal("1.25")
	plan := "plus"
	return StatusSnapshot{
		GeneratedAt: utc(2026, 10, 18, 9, 0, 0),
		Sessions:    &sessions.Summary{},
		Providers: &StatusProviders{
			Codex: &ProviderSnapshot{
				Provider: ProviderCodex, Availability: Available, Plan: &plan,
				UpdatedAt: at(8, 59),
				Quota: []WidgetQuota{
					window("5 hours", 300, QuotaStandard, 40, at(11, 30)),
					window("gpt-5.1-codex", 0, QuotaModelSpecific, 10, nil),
				},
				BalanceUSD:  &balance,
				TokenSource: TokensAllDevices,
				DailyTokens: []DailyTokens{
					NewDailyTokens("2026-10-17", 12000), NewDailyTokens("2026-10-18", 3400),
				},
				ResetCredits: ptr(NewResetCredits(3, 1)),
			},
			Grok: &ProviderSnapshot{
				Provider: ProviderGrok, Availability: Available, Plan: ptr("heavy"),
				Quota:       []WidgetQuota{window("7 days", 10080, QuotaStandard, 25, nil)},
				TokenSource: TokensThisMac,
				DailyTokens: []DailyTokens{},
			},
			Claude: &ProviderSnapshot{
				Provider: ProviderClaude, Availability: Available,
				Quota:       []WidgetQuota{window("5 hours", 300, QuotaStandard, 80, at(10, 15))},
				TokenSource: TokensThisMac,
				DailyTokens: []DailyTokens{},
			},
			ResetToday: &ResetTodaySnapshot{
				State: ResetNo, FetchedAt: utc(2026, 10, 18, 8, 55, 0),
				Timeline: []ResetTodayEntry{{EffectiveAt: utc(2026, 10, 18, 8, 55, 0), Reason: ReasonNone, State: ResetNo}},
			},
		},
	}
}

func TestStatusProvidersEncodeDecodeRoundTrip(t *testing.T) {
	snap := statusProvidersSnapshot()
	data, err := snap.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatalf("encoded status is not valid JSON: %s", data)
	}
	var back StatusSnapshot
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Providers == nil {
		t.Fatal("providers lost in round trip")
	}
	if got := back.Providers.Codex; got == nil || got.Provider != ProviderCodex || got.Availability != Available ||
		got.Plan == nil || *got.Plan != "plus" || len(got.Quota) != 2 || got.TokenSource != TokensAllDevices ||
		len(got.DailyTokens) != 2 || got.ResetCredits == nil || got.ResetCredits.AvailableCount != 3 {
		t.Fatalf("codex round trip: %+v", got)
	}
	if got := back.Providers.Grok; got == nil || got.Provider != ProviderGrok || len(got.Quota) != 1 {
		t.Fatalf("grok round trip: %+v", got)
	}
	if got := back.Providers.Claude; got == nil || got.Provider != ProviderClaude || len(got.Quota) != 1 {
		t.Fatalf("claude round trip: %+v", got)
	}
	if got := back.Providers.ResetToday; got == nil || got.State != ResetNo || len(got.Timeline) != 1 {
		t.Fatalf("resetToday round trip: %+v", got)
	}
	if back.Providers.Codex.BalanceUSD == nil || !back.Providers.Codex.BalanceUSD.Equal(swiftjson.MustDecimal("1.25")) {
		t.Fatalf("balance lost: %+v", back.Providers.Codex.BalanceUSD)
	}
}

func TestStatusProvidersFullFixtureDecodes(t *testing.T) {
	var snap StatusSnapshot
	if err := json.Unmarshal(fixture(t, "status_providers_full.json"), &snap); err != nil {
		t.Fatal(err)
	}
	p := snap.Providers
	if p == nil || p.Codex == nil || p.Grok == nil || p.Claude == nil || p.ResetToday == nil {
		t.Fatalf("fixture providers incomplete: %+v", p)
	}
	if q := p.Codex.Quota; len(q) != 2 || q[1].Source != QuotaModelSpecific || q[1].WindowMinutes != nil {
		t.Fatalf("codex quota rows: %+v", q)
	}
	if p.Grok.Plan == nil || *p.Grok.Plan != "heavy" {
		t.Fatalf("grok plan: %+v", p.Grok.Plan)
	}
	if p.ResetToday.State != ResetNo || len(p.ResetToday.Timeline) != 1 {
		t.Fatalf("resetToday: %+v", p.ResetToday)
	}
}

// The frozen Swift fixtures predate the providers block; they must keep
// decoding with a nil one.
func TestLegacyStatusFixtureDecodesWithoutProviders(t *testing.T) {
	for _, name := range []string{"swift_status_full.json", "swift_status_empty.json", "swift_status_minimal.json"} {
		var snap StatusSnapshot
		if err := json.Unmarshal(fixture(t, name), &snap); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if snap.Providers != nil {
			t.Fatalf("%s: providers = %+v, want nil", name, snap.Providers)
		}
	}
}
