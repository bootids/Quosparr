package runway

import (
	"github.com/licoy/quosparr/internal/codex"
	"github.com/licoy/quosparr/internal/codexapi"
	"github.com/licoy/quosparr/internal/estimate"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/sessions"
	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/usage"
	"sort"
	"strings"
	"time"
)

// statusExportPanel is the panel block of status.json: the derived data the
// read-only remote view needs beyond the widget summary. Like the provider
// block, Codex always says something and Grok / Claude appear only when their
// state has a widget snapshot. Nothing here carries credentials or emails;
// account labels resolve without the email fallback.
func statusExportPanel(snap Snapshot) *export.StatusPanel {
	p := &export.StatusPanel{Schema: export.PanelSchemaVersion, Codex: codexPanelBlock(snap)}
	if snap.Grok.Widget != nil {
		p.Grok = providerPanelBlock(snap.Grok.Accounts, snap.Grok.Cost,
			snap.Grok.Heatmap, snap.Grok.RecentSessions, snap.Grok.Speed,
			snap.Grok.ResetCredits, snap.Grok.AccountQuotas, grokQuotaLine, snap.Grok.Billing)
	}
	if snap.Claude.Widget != nil {
		b := providerPanelBlock(snap.Claude.Accounts, snap.Claude.Cost,
			snap.Claude.Heatmap, snap.Claude.RecentSessions, snap.Claude.Speed,
			snap.Claude.ResetCredits, snap.Claude.AccountQuotas, claudeQuotaLine, nil)
		b.ExtraUsage = snap.Claude.ExtraUsage
		if snap.Claude.API != nil {
			b.API = &export.PanelAPIView{
				BaseURL: snap.Claude.API.BaseURL, Quota: snap.Claude.API.Quota,
				ErrorCode: snap.Claude.API.ErrorCode,
			}
		}
		p.Claude = b
	}
	return p
}

// codexPanelBlock fills the Codex block from the snapshot's Codex view and
// managed account list.
func codexPanelBlock(snap Snapshot) *export.CodexPanelBlock {
	c := snap.Codex
	b := &export.CodexPanelBlock{
		APIKeyMode: c.APIKeyMode,
		APIBaseURL: c.APIBaseURL,
		APIBalance: c.APIBalance,
	}
	if c.Estimate != nil {
		b.Estimate = panelEstimate(*c.Estimate, c.EstimateError)
	}
	b.AllDevicesTokens = panelDailyTokens(c.Heatmap.AllDevices)
	b.LocalTokens = panelDailyTokens(c.Heatmap.Local)
	b.TokensAsOf = statsAsOf(c.Heatmap.OfficialStatsAsOf)
	b.TokensGenerated = timePtr(c.Heatmap.OfficialGeneratedAt)
	b.ResetCredits = panelResetCredits(c.ResetCredits)
	b.Speed = panelSpeed(c.Speed)
	b.Accounts = panelAccounts(snap.Accounts.Sidebar)
	b.ActiveAccountID = snap.Accounts.ActiveID
	if d := panelIdentity(snap.Accounts.Display); d != nil {
		b.Display = d
	}
	return b
}

// providerPanelBlock fills the shared Grok / Claude fields. The provider-only
// extras (Claude's ExtraUsage / API, Grok's Billing) are left to the caller;
// line adapts the provider's own quota line via the generic type.
func providerPanelBlock[G grokQuota | claudeQuota](accounts AccountsView, cost *usage.Summary,
	heatmap HeatmapData, recent []sessions.Item, sp *speed.Report,
	credits *codexapi.ResetCreditsSnapshot, quotas map[string]G,
	line func(G) export.PanelAccountQuota, billing *export.PanelGrokBilling,
) *export.ProviderPanelBlock {
	b := &export.ProviderPanelBlock{Billing: billing, Display: panelIdentity(accounts.Display)}
	if cost != nil {
		c := exportCost(*cost)
		b.Cost = &c
	}
	b.LocalTokens = panelDailyTokens(heatmap.Local)
	b.RecentSessions = append([]sessions.Item{}, recent...)
	b.Speed = panelSpeed(sp)
	b.ResetCredits = panelResetCredits(credits)
	b.Accounts = panelAccounts(accounts.Sidebar)
	b.ActiveAccountID = accounts.ActiveID
	b.AccountQuotas = panelAccountQuotas(quotas, line)
	return b
}

// panelIdentity mirrors the signed-in identity; the email never travels, so
// an email-shaped display name falls back to the username.
func panelIdentity(d codex.AccountDisplay) *export.PanelIdentity {
	name := d.DisplayName
	if strings.Contains(name, "@") {
		name = d.Username
	}
	if strings.Contains(name, "@") {
		name = ""
	}
	if !d.Authenticated && name == "" && d.Tier == "" && d.SubscriptionExpiresAt == nil {
		return nil
	}
	return &export.PanelIdentity{
		Authenticated:         d.Authenticated,
		DisplayName:           name,
		Tier:                  string(d.Tier),
		SubscriptionExpiresAt: d.SubscriptionExpiresAt,
	}
}

// grokQuota / claudeQuota alias the two quota line types so the shared block
// builder works for both providers.
type grokQuota = GrokAccountQuota
type claudeQuota = ClaudeAccountQuota

func grokQuotaLine(q GrokAccountQuota) export.PanelAccountQuota {
	return export.PanelAccountQuota{
		Plan: q.Plan, UsedPercent: q.UsedPercent, ResetsAt: timePtr(q.ResetsAt),
		IncludedUSD: q.IncludedUSD, UpdatedAt: q.UpdatedAt,
	}
}

func claudeQuotaLine(q ClaudeAccountQuota) export.PanelAccountQuota {
	return export.PanelAccountQuota{
		Plan: q.Plan, UsedPercent: q.UsedPercent, ResetsAt: timePtr(q.ResetsAt),
		ExtraUsage: q.ExtraUsage, Balance: q.Balance, UpdatedAt: q.UpdatedAt,
	}
}

func panelAccountQuotas[G grokQuota | claudeQuota](quotas map[string]G, line func(G) export.PanelAccountQuota) []export.PanelAccountQuota {
	if len(quotas) == 0 {
		return nil
	}
	ids := make([]string, 0, len(quotas))
	for id := range quotas {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]export.PanelAccountQuota, 0, len(ids))
	for _, id := range ids {
		q := line(quotas[id])
		q.ID = id
		out = append(out, q)
	}
	return out
}

// panelAccounts mirrors the managed account rows; the email never travels and
// Label is the email-free resolved display name.
func panelAccounts(rows []codex.ManagedAccount) []export.PanelAccount {
	if len(rows) == 0 {
		return nil
	}
	out := make([]export.PanelAccount, 0, len(rows))
	for _, a := range rows {
		out = append(out, export.PanelAccount{
			ID:                    a.ID,
			Label:                 a.DisplayNameWithoutEmail(),
			SortIndex:             a.SortIndex,
			AuthMode:              string(a.AuthMode),
			Username:              a.Username,
			UserID:                a.UserID,
			SubjectID:             a.SubjectID,
			AccountID:             a.AccountID,
			WorkspaceName:         a.WorkspaceName,
			DisplayName:           a.DisplayName,
			Alias:                 a.Alias,
			Note:                  a.Note,
			PlanType:              a.PlanType,
			SubscriptionExpiresAt: a.SubscriptionExpiresAt,
			CreatedAt:             a.CreatedAt,
			LastUsedAt:            a.LastUsedAt,
			LastQuotaAt:           a.LastQuotaAt,
			RequiresReauth:        a.RequiresReauth,
			APIBaseURL:            a.APIBaseURL,
			APIModel:              a.APIModel,
			APIQuota:              a.APIQuota,
			Quota:                 panelQuotaLine(a.CachedQuota),
		})
	}
	return out
}

func panelQuotaLine(q *codex.CachedAccountQuota) *export.PanelAccountQuotaLine {
	if q == nil {
		return nil
	}
	l := &export.PanelAccountQuotaLine{
		Plan:                   q.Plan,
		PrimaryUsedPercent:     q.PrimaryUsedPercent,
		PrimaryResetsAt:        q.PrimaryResetsAt,
		PrimaryWindowMinutes:   q.PrimaryWindowMinutes,
		SecondaryUsedPercent:   q.SecondaryUsedPercent,
		SecondaryResetsAt:      q.SecondaryResetsAt,
		SecondaryWindowMinutes: q.SecondaryWindowMinutes,
		UpdatedAt:              q.UpdatedAt,
	}
	for _, m := range q.AdditionalMeters {
		l.AdditionalMeters = append(l.AdditionalMeters, export.PanelQuotaMeterRow{
			ID: m.ID, WindowMinutes: m.WindowMinutes, Name: m.Name, UsedPercent: m.UsedPercent,
		})
	}
	return l
}

// panelEstimate mirrors an estimate snapshot; err is CodexView.EstimateError.
func panelEstimate(e estimate.Snapshot, err string) *export.PanelEstimate {
	p := &export.PanelEstimate{
		WindowMode:       string(e.WindowMode),
		CycleStartDate:   e.CycleStartDate,
		UsedPercent:      e.UsedPercent,
		UsedCredits:      e.UsedCredits,
		UsedUSD:          e.UsedUSD,
		DayCount:         e.DayCount,
		EstimatedCredits: e.EstimatedCredits,
		EstimatedUSD:     e.EstimatedUSD,
		CanExtrapolate:   e.CanExtrapolate,
		HasWeeklyWindow:  e.HasWeeklyWindow,
		PreviousEstimate: e.PreviousEstimate,
		ChangePercent:    e.ChangePercent,
		History:          e.History,
		CalculatedAt:     e.CalculatedAt,
		Error:            err,
	}
	if e.ChangeKind != nil {
		p.ChangeKind = string(*e.ChangeKind)
	}
	if e.UnavailableReason != nil {
		p.UnavailableReason = string(*e.UnavailableReason)
	}
	for _, r := range e.CurrentRows {
		p.CurrentRows = append(p.CurrentRows, export.PanelEstimateRow{
			Date: r.Date, Credits: r.Credits, Tokens: r.Tokens, Turns: r.Turns,
			USD: r.USD, CreditsReported: r.CreditsReported, TotalsReported: r.TotalsReported,
		})
	}
	return p
}

// statsAsOf parses the "yyyy-MM-dd" official-stats day into the panel's time.
func statsAsOf(day string) *time.Time {
	if day == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return nil
	}
	return &t
}

// panelDailyTokens converts a heatmap series into date-sorted rows.
func panelDailyTokens(m map[string]int64) []export.DailyTokens {
	if len(m) == 0 {
		return nil
	}
	out := widgetDailyTokens(m)
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// panelResetCredits mirrors the full credit list behind the widget counts.
func panelResetCredits(r *codexapi.ResetCreditsSnapshot) *export.PanelResetCredits {
	if r == nil {
		return nil
	}
	p := &export.PanelResetCredits{
		AvailableCount: r.AvailableCount,
		Credits:        make([]export.PanelResetCredit, 0, len(r.Credits)),
		UpdatedAt:      r.UpdatedAt,
	}
	for _, c := range r.Credits {
		p.Credits = append(p.Credits, export.PanelResetCredit{
			ID: c.ID, Status: c.Status, CreatedAt: c.CreatedAt, ExpiresAt: c.ExpiresAt,
			RemainingSeconds: c.RemainingSeconds,
		})
	}
	return p
}

// panelSpeed mirrors the model speed report; durations become nanoseconds.
func panelSpeed(r *speed.Report) *export.PanelSpeedReport {
	if r == nil {
		return nil
	}
	p := &export.PanelSpeedReport{
		Groups:       make([]export.PanelSpeedStat, 0, len(r.Groups)),
		NoTTFT:       r.NoTTFT,
		Tiered:       r.Tiered,
		From:         r.From,
		To:           r.To,
		CalculatedAt: r.CalculatedAt,
	}
	for _, g := range r.Groups {
		p.Groups = append(p.Groups, panelSpeedStat(g))
	}
	for _, s := range r.Sessions {
		row := export.PanelSpeedSession{
			ID: s.ID, Title: s.Title, ProjectName: s.ProjectName,
			UpdatedAt: s.UpdatedAt, Groups: make([]export.PanelSpeedStat, 0, len(s.Groups)),
		}
		for _, g := range s.Groups {
			row.Groups = append(row.Groups, panelSpeedStat(g))
		}
		p.Sessions = append(p.Sessions, row)
	}
	return p
}

func panelSpeedStat(s speed.Stat) export.PanelSpeedStat {
	return export.PanelSpeedStat{
		Model: s.Model, Effort: s.Effort, Tier: string(s.Tier), Calls: s.Calls,
		TTFTP50NS: int64(s.TTFTP50), TTFTP90NS: int64(s.TTFTP90),
		OutputTokens: s.OutputTokens, DecodeNS: int64(s.Decode),
	}
}
