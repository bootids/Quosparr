package runway

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/apiquota"
	"github.com/licoy/quosparr/internal/codex"
	"github.com/licoy/quosparr/internal/codexapi"
	"github.com/licoy/quosparr/internal/estimate"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/export/swiftjson"
	"github.com/licoy/quosparr/internal/sessions"
	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/usage"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

func est2(v int) *int        { return &v }
func pct(v float64) *float64 { return &v }

func panelTestSnapshot() Snapshot {
	now := time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)
	resets := now.Add(2 * time.Hour)
	est := 42.5
	usd := 1.7
	kind := estimate.ChangeIncreased
	reason := estimate.ReasonNoUsage
	costUSD := decimal.MustParse("12.34")
	quotaRemaining := 7.5
	mins := 300
	apiQuotaRemaining := 9.25
	rowUSD := 0.4
	return Snapshot{
		GeneratedAt: now,
		Accounts: AccountsView{
			Sidebar: []codex.ManagedAccount{
				{
					ID: "acc-1", SortIndex: 1, AuthMode: codex.AuthModeOAuth,
					Email: "secret@chatgpt.com", Username: "alice", UserID: "u1",
					SubscriptionExpiresAt: &resets, CreatedAt: now, LastUsedAt: &resets,
					LastQuotaAt: &resets, PlanType: "plus", Alias: "Main",
					CachedQuota: &codex.CachedAccountQuota{
						Plan: "plus", PrimaryUsedPercent: 40, PrimaryResetsAt: &resets,
						PrimaryWindowMinutes: &mins, SecondaryUsedPercent: est2(20),
						SecondaryResetsAt: &resets, SecondaryWindowMinutes: &mins,
						AdditionalMeters: []codex.CachedQuotaMeterRow{
							{ID: "gpt-5", WindowMinutes: &mins, Name: "GPT-5", UsedPercent: 55},
						},
						UpdatedAt: now,
					},
				},
				{
					// Email-only account: the label must fall back past the email.
					ID: "acc-2", AuthMode: codex.AuthModeAPIKey, Email: "ghost@example.com",
					APIBaseURL: "https://relay.example/v1", APIModel: "gpt-5",
					APIQuota: &apiquota.Quota{Status: "ok", Remaining: &apiQuotaRemaining},
				},
			},
			ActiveID: "acc-1",
			Display:  codex.AccountDisplay{Authenticated: true, DisplayName: "Alice", Email: "secret@chatgpt.com"},
		},
		Codex: CodexView{
			Authenticated: true,
			APIBalance:    &apiquota.Quota{Status: "ok", Remaining: &quotaRemaining},
			Estimate: &estimate.Snapshot{
				WindowMode: estimate.ModeAuto, CycleStartDate: "2026-10-12",
				UsedPercent: 40, UsedCredits: 10, UsedUSD: 0.4, DayCount: 2,
				EstimatedCredits: &est, EstimatedUSD: &usd, CanExtrapolate: true,
				HasWeeklyWindow: true,
				CurrentRows: []estimate.Row{
					{Date: "2026-10-17", Credits: 6, Tokens: 1200, Turns: 8, USD: &rowUSD, CreditsReported: true},
					{Date: "2026-10-18", Credits: 4, Tokens: 800, Turns: 5, TotalsReported: true},
				},
				PreviousEstimate: &estimate.HistorySample{CycleStartDate: "2026-10-05", EstimatedCredits: 38, UsedPercent: 35, UsedCredits: 9, RecordedAt: now},
				ChangePercent:    pct(5.5), ChangeKind: &kind,
				History: []estimate.HistorySample{
					{CycleStartDate: "2026-10-05", EstimatedCredits: 38, UsedPercent: 35, UsedCredits: 9, RecordedAt: now},
				},
				CalculatedAt: now, UnavailableReason: &reason,
			},
			Heatmap: HeatmapData{
				AllDevices:        map[string]int64{"2026-10-17": 900, "2026-10-18": 700},
				Local:             map[string]int64{"2026-10-18": 650},
				OfficialStatsAsOf: "2026-10-18", OfficialGeneratedAt: now,
			},
			ResetCredits: &codexapi.ResetCreditsSnapshot{
				AvailableCount: 1,
				Credits: []codexapi.ResetCredit{
					{ID: "rc-1", Status: "available", CreatedAt: now, ExpiresAt: resets, RemainingSeconds: 7200},
				},
				UpdatedAt: now,
			},
			Speed: &speed.Report{
				Groups: []speed.Stat{
					{Model: "gpt-5", Effort: "medium", Tier: speed.TierFast, Calls: 4,
						TTFTP50: 1200 * time.Millisecond, TTFTP90: 2 * time.Second,
						OutputTokens: 900, Decode: 18 * time.Second},
				},
				Sessions: []speed.SessionStat{
					{ID: "s1", Title: "Chat", ProjectName: "Quosparr", UpdatedAt: now, Groups: []speed.Stat{
						{Model: "gpt-5", Calls: 4, TTFTP50: time.Second, Decode: 9 * time.Second, OutputTokens: 450},
					}},
				},
				Tiered: true, From: now.Add(-time.Hour), To: now, CalculatedAt: now,
			},
		},
		Grok: GrokState{
			Widget:  &export.ProviderSnapshot{},
			Billing: &export.PanelGrokBilling{Source: "grok", UpdatedAt: now, IncludedUsagePercent: 12.5},
			Cost: &usage.Summary{
				EstimatedUSD: &costUSD, CalculatedAt: now,
				Totals: usage.Totals{TotalTokens: 5000, Turns: 12},
			},
			Heatmap:        HeatmapData{Local: map[string]int64{"2026-10-18": 320}},
			RecentSessions: []sessions.Item{{ID: "g1", Title: "Grok chat"}},
			Speed: &speed.Report{
				Groups: []speed.Stat{{Model: "grok-4", Calls: 2, TTFTP50: 900 * time.Millisecond, Decode: 5 * time.Second, OutputTokens: 300}},
				From:   now.Add(-time.Hour), To: now, CalculatedAt: now,
			},
			ResetCredits: &codexapi.ResetCreditsSnapshot{
				AvailableCount: 2,
				Credits: []codexapi.ResetCredit{
					{ID: "grc-1", Status: "available", CreatedAt: now, ExpiresAt: resets, RemainingSeconds: 3600},
				},
				UpdatedAt: now,
			},
			AccountQuotas: map[string]GrokAccountQuota{
				"g1": {Plan: "grok-heavy", Title: "Weekly", UsedPercent: 30, ResetsAt: resets, IncludedUSD: "$3 / $10", UpdatedAt: now},
			},
			Accounts: AccountsView{
				Sidebar:  []codex.ManagedAccount{{ID: "g1", Email: "grok@example.com", Username: "guser"}},
				ActiveID: "g1",
			},
		},
		Claude: ClaudeState{
			Widget:     &export.ProviderSnapshot{},
			ExtraUsage: "18% of monthly limit",
			API: &ClaudeAPIView{BaseURL: "https://relay.example", ErrorCode: "api_failed",
				Quota: &apiquota.Quota{Status: "ok", Remaining: &quotaRemaining}},
			Cost: &usage.Summary{
				EstimatedUSD: &costUSD, CalculatedAt: now,
				Totals: usage.Totals{TotalTokens: 3000, Turns: 7},
			},
			Heatmap:        HeatmapData{Local: map[string]int64{"2026-10-17": 210}},
			RecentSessions: []sessions.Item{{ID: "c1", Title: "Claude chat"}},
			ResetCredits: &codexapi.ResetCreditsSnapshot{
				AvailableCount: 0, UpdatedAt: now,
			},
			AccountQuotas: map[string]ClaudeAccountQuota{
				"c1": {Plan: "pro", Title: "Monthly", UsedPercent: 60, ResetsAt: resets, ExtraUsage: "18%", Balance: "$4 / $5", UpdatedAt: now},
			},
			Accounts: AccountsView{
				Sidebar:  []codex.ManagedAccount{{ID: "c1", Email: "claude@example.com", DisplayName: "Claude Main"}},
				ActiveID: "c1",
			},
		},
	}
}

func TestStatusExportPanelCodexMapping(t *testing.T) {
	snap := panelTestSnapshot()
	p := statusExportPanel(snap)
	if p.Schema != export.PanelSchemaVersion {
		t.Fatalf("schema = %d, want %d", p.Schema, export.PanelSchemaVersion)
	}
	if p.Codex == nil {
		t.Fatal("codex block must always be present")
	}
	c := p.Codex
	e := c.Estimate
	if e == nil {
		t.Fatal("estimate missing")
	}
	if e.WindowMode != "auto" || e.CycleStartDate != "2026-10-12" || e.UsedPercent != 40 ||
		e.UsedCredits != 10 || e.UsedUSD != 0.4 || e.DayCount != 2 || !e.CanExtrapolate || !e.HasWeeklyWindow {
		t.Fatalf("estimate scalars wrong: %+v", e)
	}
	if e.EstimatedCredits == nil || *e.EstimatedCredits != 42.5 || e.EstimatedUSD == nil || *e.EstimatedUSD != 1.7 {
		t.Fatalf("estimate totals wrong: %+v", e)
	}
	if len(e.CurrentRows) != 2 || e.CurrentRows[0].Date != "2026-10-17" || e.CurrentRows[0].Credits != 6 ||
		e.CurrentRows[0].Tokens != 1200 || e.CurrentRows[0].Turns != 8 || e.CurrentRows[0].USD == nil ||
		!e.CurrentRows[0].CreditsReported || !e.CurrentRows[1].TotalsReported {
		t.Fatalf("current rows wrong: %+v", e.CurrentRows)
	}
	if e.PreviousEstimate == nil || e.PreviousEstimate.CycleStartDate != "2026-10-05" || len(e.History) != 1 {
		t.Fatalf("history wrong: %+v", e)
	}
	if e.ChangeKind != "increased" || e.ChangePercent == nil || *e.ChangePercent != 5.5 {
		t.Fatalf("change wrong: %+v", e)
	}
	if e.UnavailableReason != "noUsage" {
		t.Fatalf("unavailableReason = %q", e.UnavailableReason)
	}
	if len(c.AllDevicesTokens) != 2 || c.AllDevicesTokens[0].Date != "2026-10-17" || c.AllDevicesTokens[0].Tokens != 900 ||
		c.AllDevicesTokens[1].Date != "2026-10-18" || c.AllDevicesTokens[1].Tokens != 700 {
		t.Fatalf("allDevicesTokens wrong: %+v", c.AllDevicesTokens)
	}
	if len(c.LocalTokens) != 1 || c.LocalTokens[0].Tokens != 650 {
		t.Fatalf("localTokens wrong: %+v", c.LocalTokens)
	}
	if c.TokensAsOf == nil || !c.TokensAsOf.Equal(time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("tokensAsOf wrong: %v", c.TokensAsOf)
	}
	if c.TokensGenerated == nil || !c.TokensGenerated.Equal(snap.GeneratedAt) {
		t.Fatalf("tokensGenerated wrong: %v", c.TokensGenerated)
	}
	r := c.ResetCredits
	if r == nil || r.AvailableCount != 1 || len(r.Credits) != 1 || r.Credits[0].ID != "rc-1" ||
		r.Credits[0].Status != "available" || r.Credits[0].RemainingSeconds != 7200 ||
		!r.Credits[0].ExpiresAt.Equal(snap.Codex.ResetCredits.Credits[0].ExpiresAt) {
		t.Fatalf("resetCredits wrong: %+v", r)
	}
	s := c.Speed
	if s == nil || len(s.Groups) != 1 || s.Groups[0].Model != "gpt-5" || s.Groups[0].Tier != "fast" ||
		s.Groups[0].Calls != 4 || s.Groups[0].TTFTP50NS != int64(1200*time.Millisecond) ||
		s.Groups[0].TTFTP90NS != int64(2*time.Second) || s.Groups[0].DecodeNS != int64(18*time.Second) ||
		s.Groups[0].OutputTokens != 900 || !s.Tiered {
		t.Fatalf("speed wrong: %+v", s)
	}
	if len(s.Sessions) != 1 || s.Sessions[0].ID != "s1" || len(s.Sessions[0].Groups) != 1 {
		t.Fatalf("speed sessions wrong: %+v", s.Sessions)
	}
	if c.APIKeyMode || c.APIBalance == nil || c.APIBalance.Remaining == nil || *c.APIBalance.Remaining != 7.5 {
		t.Fatalf("api key fields wrong: %+v", c)
	}
	if len(c.Accounts) != 2 {
		t.Fatalf("accounts wrong: %+v", c.Accounts)
	}
	a := c.Accounts[0]
	if a.ID != "acc-1" || a.Label != "Main" || a.AuthMode != "oauth" || a.Username != "alice" ||
		a.PlanType != "plus" || a.SortIndex != 1 || a.SubscriptionExpiresAt == nil {
		t.Fatalf("account 0 wrong: %+v", a)
	}
	if a.Quota == nil || a.Quota.Plan != "plus" || a.Quota.PrimaryUsedPercent != 40 ||
		a.Quota.PrimaryWindowMinutes == nil || *a.Quota.PrimaryWindowMinutes != 300 ||
		a.Quota.SecondaryUsedPercent == nil || *a.Quota.SecondaryUsedPercent != 20 ||
		len(a.Quota.AdditionalMeters) != 1 || a.Quota.AdditionalMeters[0].ID != "gpt-5" ||
		a.Quota.AdditionalMeters[0].UsedPercent != 55 {
		t.Fatalf("account 0 quota wrong: %+v", a.Quota)
	}
	if c.Accounts[1].APIQuota == nil || c.Accounts[1].APIBaseURL != "https://relay.example/v1" || c.Accounts[1].APIModel != "gpt-5" {
		t.Fatalf("account 1 relay fields wrong: %+v", c.Accounts[1])
	}
	if c.ActiveAccountID != "acc-1" {
		t.Fatalf("activeAccountId = %q", c.ActiveAccountID)
	}
	if c.Display == nil || !c.Display.Authenticated || c.Display.DisplayName != "Alice" {
		t.Fatalf("display wrong: %+v", c.Display)
	}
}

func TestStatusExportPanelProviders(t *testing.T) {
	snap := panelTestSnapshot()
	now := snap.GeneratedAt
	p := statusExportPanel(snap)
	g := p.Grok
	if g == nil {
		t.Fatal("grok block missing")
	}
	if g.Cost == nil || g.Cost.EstimatedUSD == nil || g.Cost.EstimatedUSD.String() != "12.34" {
		t.Fatalf("grok cost wrong: %+v", g.Cost)
	}
	if len(g.LocalTokens) != 1 || g.LocalTokens[0].Tokens != 320 {
		t.Fatalf("grok localTokens wrong: %+v", g.LocalTokens)
	}
	if len(g.RecentSessions) != 1 || g.RecentSessions[0].ID != "g1" {
		t.Fatalf("grok sessions wrong: %+v", g.RecentSessions)
	}
	if g.Speed == nil || len(g.Speed.Groups) != 1 || g.Speed.Groups[0].TTFTP50NS != int64(900*time.Millisecond) {
		t.Fatalf("grok speed wrong: %+v", g.Speed)
	}
	if g.ResetCredits == nil || g.ResetCredits.AvailableCount != 2 || len(g.ResetCredits.Credits) != 1 || g.ResetCredits.Credits[0].ID != "grc-1" {
		t.Fatalf("grok resetCredits wrong: %+v", g.ResetCredits)
	}
	if len(g.Accounts) != 1 || g.Accounts[0].ID != "g1" || g.ActiveAccountID != "g1" {
		t.Fatalf("grok accounts wrong: %+v", g.Accounts)
	}
	if len(g.AccountQuotas) != 1 || g.AccountQuotas[0].ID != "g1" || g.AccountQuotas[0].Plan != "grok-heavy" ||
		g.AccountQuotas[0].UsedPercent != 30 || g.AccountQuotas[0].IncludedUSD != "$3 / $10" ||
		g.AccountQuotas[0].ResetsAt == nil || !g.AccountQuotas[0].UpdatedAt.Equal(now) {
		t.Fatalf("grok accountQuotas wrong: %+v", g.AccountQuotas)
	}
	if g.Billing == nil || g.Billing.Source != "grok" || g.Billing.IncludedUsagePercent != 12.5 {
		t.Fatalf("grok billing wrong: %+v", g.Billing)
	}

	cl := p.Claude
	if cl == nil {
		t.Fatal("claude block missing")
	}
	if cl.Billing != nil {
		t.Fatal("claude must not carry billing")
	}
	if cl.ExtraUsage != "18% of monthly limit" {
		t.Fatalf("claude extraUsage = %q", cl.ExtraUsage)
	}
	if cl.API == nil || cl.API.BaseURL != "https://relay.example" || cl.API.ErrorCode != "api_failed" ||
		cl.API.Quota == nil || *cl.API.Quota.Remaining != 7.5 {
		t.Fatalf("claude api wrong: %+v", cl.API)
	}
	if len(cl.AccountQuotas) != 1 || cl.AccountQuotas[0].ID != "c1" || cl.AccountQuotas[0].ExtraUsage != "18%" ||
		cl.AccountQuotas[0].Balance != "$4 / $5" || cl.AccountQuotas[0].Plan != "pro" {
		t.Fatalf("claude accountQuotas wrong: %+v", cl.AccountQuotas)
	}
	if cl.Accounts[0].Label != "Claude Main" {
		t.Fatalf("claude account label = %q", cl.Accounts[0].Label)
	}
}

func TestStatusExportPanelRoundTrip(t *testing.T) {
	p := statusExportPanel(panelTestSnapshot())
	data, err := swiftjson.Marshal(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "@chatgpt.com") || strings.Contains(string(data), "@example.com") {
		t.Fatalf("email leaked: %s", data)
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
}

func TestStatusExportPanelOmitsUnusedProviders(t *testing.T) {
	snap := panelTestSnapshot()
	snap.Grok.Widget = nil
	snap.Claude = ClaudeState{}
	p := statusExportPanel(snap)
	if p.Grok != nil {
		t.Fatal("grok block must be omitted without a widget snapshot")
	}
	if p.Claude != nil {
		t.Fatal("claude block must be omitted without a widget snapshot")
	}
	if p.Codex == nil {
		t.Fatal("codex block must stay")
	}
}

func TestDisplayNameWithoutEmail(t *testing.T) {
	base := codex.ManagedAccount{ID: "id", DisplayName: "D", Alias: "A", Username: "U", AccountID: "0123456789ABC"}
	if got := base.DisplayNameWithoutEmail(); got != "A" {
		t.Fatalf("alias fallback = %q", got)
	}
	base.Alias = ""
	if got := base.DisplayNameWithoutEmail(); got != "D" {
		t.Fatalf("displayName fallback = %q", got)
	}
	base.DisplayName = ""
	base.Email = "x@y.z"
	if got := base.DisplayNameWithoutEmail(); got != "U" {
		t.Fatalf("email must be skipped, got %q", got)
	}
	base.Username = ""
	if got := base.DisplayNameWithoutEmail(); got != "01234...9ABC" {
		t.Fatalf("shortened account id fallback = %q", got)
	}
	base.AccountID = ""
	if got := base.DisplayNameWithoutEmail(); got != "id" {
		t.Fatalf("id fallback = %q", got)
	}
	only := codex.ManagedAccount{ID: "only", Email: "only@x.y"}
	if got := only.DisplayNameWithoutEmail(); got != "only" {
		t.Fatalf("email-only account label = %q", got)
	}
}
