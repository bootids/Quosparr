package app

import (
	"context"
	"errors"
	"time"

	"github.com/licoy/quosparr/internal/claude"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/grok"
	"github.com/licoy/quosparr/internal/resettoday"
	"github.com/licoy/quosparr/internal/sessions"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

// CostQuery is a detail-only selection; it never changes summary preferences.
type CostQuery struct {
	Range  store.CostRange
	Custom *usage.CostRange
}

var (
	errDetailUnavailable    = errors.New("detail service unavailable")
	errGrokCycleUnavailable = errors.New("Grok billing cycle unavailable")
	errInvalidCostRange     = errors.New("invalid cost range")
)

func (rt *liveApp) queryDetailCost(ctx context.Context, provider store.Provider, q CostQuery) (usage.Summary, error) {
	now := time.Now()
	switch provider {
	case store.ProviderGrok:
		return rt.queryGrokDetailCost(ctx, q, now)
	case store.ProviderClaude:
		return rt.queryClaudeDetailCost(ctx, q, now)
	}
	ls := rt.localService()
	if ls == nil {
		return usage.Summary{}, errDetailUnavailable
	}
	if q.Custom != nil {
		return ls.QueryCost(ctx, *q.Custom)
	}
	switch q.Range {
	case store.CostRangeCurrent:
		return rt.localService().QueryCurrentCycleCost(ctx)
	case store.CostRangePrevious:
		r, err := rt.localService().ResolvePreviousCycleRange(ctx)
		if err != nil {
			return usage.Summary{}, err
		}
		return rt.localService().QueryCost(ctx, r)
	case store.CostRangeToday:
		return rt.localService().QueryCost(ctx, usage.Today(now, time.Local))
	case store.CostRangeThisMonth:
		return rt.localService().QueryCost(ctx, usage.ThisMonth(now, time.Local))
	default:
		return usage.Summary{}, errInvalidCostRange
	}
}

func (rt *liveApp) queryGrokDetailCost(ctx context.Context, q CostQuery, now time.Time) (usage.Summary, error) {
	if rt.grokMod == nil || rt.grokMod.Store == nil || rt.grok == nil {
		return usage.Summary{}, errDetailUnavailable
	}
	var window usage.Window
	if q.Custom != nil {
		window = q.Custom.Window
	} else {
		rt.grok.mu.Lock()
		detail := rt.grok.state.Detail
		rt.grok.mu.Unlock()
		// A billing cycle must be reported; never silently invent a seven-day cycle.
		if q.Range == store.CostRangeCurrent || q.Range == store.CostRangePrevious {
			billing, ok := detail.(*grok.BillingSnapshot)
			if !ok || billing.Period == nil || billing.Period.StartsAt == nil || billing.Period.ResetsAt == nil || !billing.Period.ResetsAt.After(*billing.Period.StartsAt) {
				return usage.Summary{}, errGrokCycleUnavailable
			}
		}
		if !q.Range.Valid() {
			return usage.Summary{}, errInvalidCostRange
		}
		window = grokCostWindow(q.Range, detail, now)
	}
	if !window.End.After(window.Start) {
		return usage.Summary{}, errInvalidCostRange
	}
	return (grok.SessionScanner{GrokHome: rt.grokMod.Store.OfficialHome(), Location: time.Local}).ScanCost(ctx, window, now)
}

// queryClaudeDetailCost scans the Claude Code logs for one range. The
// current and previous cycles follow the cached 7-day window.
func (rt *liveApp) queryClaudeDetailCost(ctx context.Context, q CostQuery, now time.Time) (usage.Summary, error) {
	g := rt.claudeB
	if rt.claudeMod == nil || rt.claudeMod.Store == nil || g == nil {
		return usage.Summary{}, errDetailUnavailable
	}
	var window usage.Window
	switch {
	case q.Custom != nil:
		window = q.Custom.Window
	case q.Range == store.CostRangeToday, q.Range == store.CostRangeThisMonth, q.Range == store.CostRangeCurrent, q.Range == store.CostRangePrevious:
		g.mu.Lock()
		detail := g.state.Detail
		g.mu.Unlock()
		window = claudeCostWindow(q.Range, detail, now)
	default:
		return usage.Summary{}, errInvalidCostRange
	}
	if !window.End.After(window.Start) {
		return usage.Summary{}, errInvalidCostRange
	}
	scanner := claude.SessionScanner{ConfigDir: rt.claudeMod.Store.ConfigDir(), Home: rt.claudeMod.Store.HomeDir(), Location: time.Local, Cache: &g.sessions}
	return scanner.ScanCost(ctx, window, now)
}

func installDetailActions(rt *liveApp, a *PanelActions) {
	// Remote mode writes nothing locally: the cost detail answers from the
	// exported panel mirror, the reaction store and the session repair stay
	// local-only and disabled; the skills page keeps its read-only actions.
	if rt.remoteMode() {
		installSkillActions(rt, &a.Skills)
		a.QueryCost = rt.queryRemoteDetailCost
		return
	}
	a.QueryCost = rt.queryDetailCost
	client := &resettoday.ReactionClient{Store: resettoday.DefaultReactionStore()}
	a.QueryReaction = client.Fetch
	a.React = client.Click
	a.RepairSessions = func() { rt.queuePreferences(livePreferenceRequest{repair: true}) }
	installSkillActions(rt, &a.Skills)
}

// queryRemoteDetailCost answers a cost query from the remote status mirror:
// it filters the exported daily rows of the provider's cost block to the
// requested window and sums them, instead of scanning local sessions.
func (rt *liveApp) queryRemoteDetailCost(ctx context.Context, provider store.Provider, q CostQuery) (usage.Summary, error) {
	rs := rt.remoteSvc
	if rs == nil || rs.RemoteService == nil {
		return usage.Summary{}, errDetailUnavailable
	}
	now := time.Now()
	block := remoteCostBlock(rs.RawStatus(), provider)
	if block == nil {
		return usage.Summary{}, errDetailUnavailable
	}
	window, ok := remoteCostQueryWindow(q, block.Window, now)
	if !ok {
		return usage.Summary{}, errInvalidCostRange
	}
	return remoteCostFilter(block, window, now), nil
}

// remoteCostBlock picks the cost mirror of one provider: the panel block for
// Grok and Claude, the top-level cost for Codex.
func remoteCostBlock(st export.StatusSnapshot, provider store.Provider) *export.CostSummary {
	switch provider {
	case store.ProviderGrok:
		if st.Panel != nil && st.Panel.Grok != nil {
			return st.Panel.Grok.Cost
		}
		return nil
	case store.ProviderClaude:
		if st.Panel != nil && st.Panel.Claude != nil {
			return st.Panel.Claude.Cost
		}
		return nil
	}
	return st.Cost
}

// remoteCostQueryWindow resolves the requested range. The current and
// previous cycles follow the exported summary window; the calendar ranges
// use the local clock.
func remoteCostQueryWindow(q CostQuery, exported export.Interval, now time.Time) (usage.Window, bool) {
	if q.Custom != nil {
		return q.Custom.Window, true
	}
	full := exportIntervalWindow(exported)
	switch q.Range {
	case store.CostRangeCurrent:
		return full, true
	case store.CostRangePrevious:
		return usage.PreviousCycle(full, time.Local).Window, true
	case store.CostRangeToday:
		return usage.Today(now, time.Local).Window, true
	case store.CostRangeThisMonth:
		return usage.ThisMonth(now, time.Local).Window, true
	}
	return usage.Window{}, false
}

func exportIntervalWindow(in export.Interval) usage.Window {
	return usage.Window{Start: in.Start, End: in.Start.Add(time.Duration(in.Duration * float64(time.Second)))}
}

// remoteCostFilter sums the daily rows of one window. The model, project and
// client breakdowns are only valid for the exported window as a whole, so
// they are copied only when the request matches it exactly.
func remoteCostFilter(c *export.CostSummary, window usage.Window, now time.Time) usage.Summary {
	out := usage.Summary{
		Source:         usage.Source(c.Source),
		Confidence:     usage.Confidence(c.Confidence),
		Window:         window,
		Warnings:       append([]string{}, c.Warnings...),
		PricingVersion: c.PricingVersion,
		CalculatedAt:   now,
	}
	full := exportIntervalWindow(c.Window)
	if full.Start.Equal(window.Start) && full.End.Equal(window.End) {
		out.ModelRows = remoteBreakdownRows(c.ModelRows)
		out.ProjectRows = remoteBreakdownRows(c.ProjectRows)
		out.ClientRows = remoteBreakdownRows(c.ClientRows)
	}
	startDay := window.Start.UTC().Format("2006-01-02")
	endDay := window.End.UTC().Format("2006-01-02")
	var sum decimal.Decimal
	for _, row := range c.DailyRows {
		if row.Date < startDay || row.Date >= endDay {
			continue
		}
		dr := usage.DailyRow{
			Date:            row.Date,
			Totals:          remoteTotals(row.Totals),
			RawCredits:      row.RawCredits,
			CreditsReported: row.CreditsReported,
			TotalsReported:  row.TotalsReported,
		}
		if row.EstimatedUSD != nil {
			if v, err := decimal.Parse(row.EstimatedUSD.String()); err == nil {
				dr.EstimatedUSD = &v
				sum = sum.Add(v)
			}
		}
		out.DailyRows = append(out.DailyRows, dr)
		out.Totals.TotalTokens += dr.Totals.TotalTokens
		out.Totals.UncachedInputTokens += dr.Totals.UncachedInputTokens
		out.Totals.CachedInputTokens += dr.Totals.CachedInputTokens
		out.Totals.OutputTokens += dr.Totals.OutputTokens
		out.Totals.Turns += dr.Totals.Turns
		out.Totals.Threads += dr.Totals.Threads
		out.RawCredits += row.RawCredits
	}
	out.EstimatedUSD = &sum
	return out
}

func remoteBreakdownRows(rows []export.CostBreakdownRow) []usage.BreakdownRow {
	out := make([]usage.BreakdownRow, 0, len(rows))
	for _, r := range rows {
		row := usage.BreakdownRow{Name: r.Name, Totals: remoteTotals(r.Totals), RawCredits: r.RawCredits}
		if r.EstimatedUSD != nil {
			if v, err := decimal.Parse(r.EstimatedUSD.String()); err == nil {
				row.EstimatedUSD = &v
			}
		}
		out = append(out, row)
	}
	return out
}

func remoteTotals(t sessions.Totals) usage.Totals {
	return usage.Totals{
		TotalTokens:         int64(t.TotalTokens),
		UncachedInputTokens: int64(t.UncachedInputTokens),
		CachedInputTokens:   int64(t.CachedInputTokens),
		OutputTokens:        int64(t.OutputTokens),
		Turns:               int64(t.Turns),
		Threads:             int64(t.Threads),
	}
}
