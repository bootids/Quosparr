package runway

import (
	"time"

	"github.com/licoy/quosparr/internal/codexapi"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/export/swiftjson"
	"github.com/licoy/quosparr/internal/sessions"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func exportWindow(w codexapi.RateWindow) export.Window {
	return export.Window{UsedPercent: w.UsedPercent, ResetsAt: timePtr(w.ResetsAt)}
}

// statusWindowName labels a window for the status file by its real length;
// the legacy positional names ("primary is 5-hour") mislabel accounts whose
// primary window is the weekly one, which the remote view then shows under
// the wrong label.
func statusWindowName(w codexapi.RateWindow, fallback string) string {
	switch {
	case w.WindowMinutes >= 6*24*60:
		return "Weekly"
	case w.WindowMinutes > 0:
		return "5-hour"
	default:
		return fallback
	}
}

func statusQuota(q codexapi.QuotaSnapshot) export.StatusQuota {
	out := export.StatusQuota{
		Primary: export.StatusWindow{
			Name:        statusWindowName(q.Primary, "5-hour"),
			UsedPercent: q.Primary.UsedPercent,
			ResetsAt:    timePtr(q.Primary.ResetsAt),
		},
		AdditionalWindows: []export.StatusWindow{},
		CreditsBalance:    q.CreditsBalance,
		UpdatedAt:         q.UpdatedAt,
	}
	if q.Plan != "" {
		p := q.Plan
		out.Plan = &p
	}
	if q.Secondary != nil {
		out.Secondary = &export.StatusWindow{
			Name:        statusWindowName(*q.Secondary, "Weekly"),
			UsedPercent: q.Secondary.UsedPercent,
			ResetsAt:    timePtr(q.Secondary.ResetsAt),
		}
	}
	for _, a := range q.AdditionalWindows {
		out.AdditionalWindows = append(out.AdditionalWindows, export.StatusWindow{
			Name: a.Name, UsedPercent: a.Window.UsedPercent, ResetsAt: timePtr(a.Window.ResetsAt),
		})
	}
	return out
}

func swiftDecimal(d *decimal.Decimal) *swiftjson.Decimal {
	if d == nil {
		return nil
	}
	v, err := swiftjson.ParseDecimal(d.String())
	if err != nil {
		return nil
	}
	return &v
}

func sessionTotals(t usage.Totals) sessions.Totals {
	return sessions.Totals{
		TotalTokens: int(t.TotalTokens), UncachedInputTokens: int(t.UncachedInputTokens),
		CachedInputTokens: int(t.CachedInputTokens), OutputTokens: int(t.OutputTokens),
		Turns: int(t.Turns), Threads: int(t.Threads),
	}
}

func breakdown(rows []usage.BreakdownRow) []export.CostBreakdownRow {
	out := make([]export.CostBreakdownRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, export.CostBreakdownRow{
			Name: r.Name, Totals: sessionTotals(r.Totals),
			EstimatedUSD: swiftDecimal(r.EstimatedUSD), RawCredits: r.RawCredits,
		})
	}
	return out
}

// exportCost converts a cost summary to the status file form.
func exportCost(c usage.Summary) export.CostSummary {
	daily := make([]export.CostDailyRow, 0, len(c.DailyRows))
	for _, r := range c.DailyRows {
		daily = append(daily, export.CostDailyRow{
			Date: r.Date, Totals: sessionTotals(r.Totals), EstimatedUSD: swiftDecimal(r.EstimatedUSD),
			RawCredits: r.RawCredits, CreditsReported: r.CreditsReported, TotalsReported: r.TotalsReported,
		})
	}
	warnings := append([]string{}, c.Warnings...)
	return export.CostSummary{
		Source: string(c.Source), Confidence: string(c.Confidence),
		Window:       export.Interval{Start: c.Window.Start, Duration: c.Window.End.Sub(c.Window.Start).Seconds()},
		EstimatedUSD: swiftDecimal(c.EstimatedUSD), Totals: sessionTotals(c.Totals),
		DailyRows: daily, ModelRows: breakdown(c.ModelRows), ProjectRows: breakdown(c.ProjectRows),
		ClientRows: breakdown(c.ClientRows), RawCredits: c.RawCredits, Warnings: warnings,
		PricingVersion: c.PricingVersion, CalculatedAt: c.CalculatedAt,
	}
}

// StatusSnapshotLocked is the content of status.json for the current state.
func (s *Service) statusSnapshotLocked(now time.Time) export.StatusSnapshot {
	out := export.StatusSnapshot{GeneratedAt: now}
	if s.quota != nil {
		q := statusQuota(*s.quota)
		out.Quota = &q
	}
	if s.cost != nil {
		c := exportCost(*s.cost)
		out.Cost = &c
	}
	items := append([]sessions.Item{}, s.recent...)
	out.Sessions = &sessions.Summary{Items: items}
	built := s.buildLocked(now)
	out.QuotaReport = s.quotaReportLocked(built, now)
	out.Providers = statusExportProviders(built)
	out.Panel = statusExportPanel(built)
	return out
}

// statusExportProviders is the provider block of status.json: the same
// snapshots the widget shows, copied without the widget's model-specific quota
// filtering (each row carries its source; readers filter). Codex always says
// something (at least its availability) so remote readers can tell a
// not-logged-in provider from an absent one; the other blocks are omitted
// when the state has no widget snapshot.
func statusExportProviders(snap Snapshot) *export.StatusProviders {
	var p export.StatusProviders
	c := codexWidgetProvider(snap.Codex)
	p.Codex = &c
	if snap.Grok.Widget != nil {
		w := *snap.Grok.Widget
		p.Grok = &w
	}
	if snap.Claude.Widget != nil {
		w := *snap.Claude.Widget
		p.Claude = &w
	}
	if snap.ResetToday.Widget != nil {
		r := *snap.ResetToday.Widget
		p.ResetToday = &r
	}
	for _, sp := range MeterProviders() {
		if ms, ok := snap.Meters[sp]; ok {
			if w, has := meterWidgetSnapshot(sp, ms, true); has {
				switch sp {
				case store.ProviderCursor:
					p.Cursor = &w
				case store.ProviderKimi:
					p.Kimi = &w
				case store.ProviderZai:
					p.Zai = &w
				case store.ProviderAgy:
					p.Agy = &w
				case store.ProviderDeepSeek:
					p.DeepSeek = &w
				case store.ProviderMiniMax:
					p.MiniMax = &w
				case store.ProviderPixelLab:
					p.PixelLab = &w
				case store.ProviderElevenLabs:
					p.ElevenLabs = &w
				}
			}
		}
	}
	return &p
}

// exportStatusLocked writes status.json when the preference is on. The file
// holds the quota, the cost and the recent sessions, never credentials.
func (s *Service) exportStatusLocked() {
	if !s.prefs.ExportsStatusJSON || s.opts.StatusPath == "" {
		return
	}
	if err := (export.StatusExporter{Path: s.opts.StatusPath}).Save(s.statusSnapshotLocked(s.now())); err != nil {
		s.lastErr = err.Error()
	}
}
