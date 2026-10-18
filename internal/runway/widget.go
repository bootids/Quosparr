package runway

import (
	"strconv"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/apiquota"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/export/swiftjson"
	"github.com/licoy/quosparr/internal/store"
)

func widgetQuota(m Meter) export.WidgetQuota {
	var mins *int
	if m.WindowMinutes > 0 {
		v := m.WindowMinutes
		mins = &v
	}
	src := export.QuotaStandard
	if m.Source == SourceModelSpecific {
		src = export.QuotaModelSpecific
	}
	q := export.NewWidgetQuota(m.Title, mins, src, m.UsedPercent, m.RemainingPercent, timePtr(m.ResetsAt))
	if m.ValueText != "" {
		v := m.ValueText
		q.ValueText = &v
	}
	if m.Group != "" {
		q.Group = &m.Group
	}
	if m.GroupDescription != "" {
		q.GroupDescription = &m.GroupDescription
	}
	if m.RemainingCaption != "" {
		q.RemainingCaption = &m.RemainingCaption
	}
	return q
}

func widgetDailyTokens(m map[string]int64) []export.DailyTokens {
	out := make([]export.DailyTokens, 0, len(m))
	for d, t := range m {
		out = append(out, export.NewDailyTokens(d, int(t)))
	}
	return out
}

func floatDecimal(f float64) *swiftjson.Decimal {
	d, err := swiftjson.ParseDecimal(strconv.FormatFloat(f, 'f', -1, 64))
	if err != nil {
		return nil
	}
	return &d
}

// codexWidgetProvider is the Codex block of the widget snapshot.
func codexWidgetProvider(c CodexView) export.ProviderSnapshot {
	p := export.ProviderSnapshot{Provider: export.ProviderCodex, TokenSource: export.TokensThisMac}
	switch {
	case c.Quota != nil, c.APIKeyMode:
		// An API key has no quota to wait for; its balance, if any, is below.
		p.Availability = export.Available
	case c.Authenticated:
		p.Availability = export.Unavailable
	default:
		p.Availability = export.NotLoggedIn
	}
	if c.Quota != nil {
		if c.Quota.Plan != "" {
			plan := c.Quota.Plan
			p.Plan = &plan
		}
		p.UpdatedAt = timePtr(c.Quota.UpdatedAt)
		if c.Quota.CreditsBalance != nil {
			p.BalanceUSD = floatDecimal(*c.Quota.CreditsBalance)
		}
	}
	setAPIBalance(&p, c.APIBalance)
	for _, m := range c.Meters {
		p.Quota = append(p.Quota, widgetQuota(m))
	}
	if c.Cost != nil {
		p.APIEquivalentCostUSD = swiftDecimal(c.Cost.EstimatedUSD)
	}
	tokens := c.Heatmap.Local
	if len(c.Heatmap.AllDevices) > 0 {
		tokens, p.TokenSource = c.Heatmap.AllDevices, export.TokensAllDevices
	}
	p.DailyTokens = widgetDailyTokens(tokens)
	if c.ResetCreditSummary != nil {
		r := export.NewResetCredits(c.ResetCreditSummary.AvailableCount, c.ResetCreditSummary.ExpiringCount)
		p.ResetCredits = &r
	}
	return export.NewProviderSnapshot(p)
}

// setAPIBalance puts an API relay balance on the provider: the text in the
// site's own unit, and the number only when that unit is US dollars.
func setAPIBalance(p *export.ProviderSnapshot, q *apiquota.Quota) {
	if q == nil {
		return
	}
	text := APIBalanceText(*q)
	if text == "" {
		return
	}
	p.BalanceText = &text
	if !q.Unlimited && q.Remaining != nil && (q.Unit == "" || strings.EqualFold(q.Unit, apiquota.UnitUSD)) {
		p.BalanceUSD = floatDecimal(*q.Remaining)
	}
}

// grokWidgetProvider is the Grok block: the provider's own block, or an
// "unavailable" placeholder without a Grok provider.
func grokWidgetProvider(g GrokState, p store.Preferences) export.ProviderSnapshot {
	if g.Widget != nil {
		w := *g.Widget
		if !p.ShowsModelSpecificQuotaUsage {
			var std []export.WidgetQuota
			for _, q := range w.Quota {
				if q.Source == export.QuotaStandard {
					std = append(std, q)
				}
			}
			w.Quota = std
		}
		return export.NewProviderSnapshot(w)
	}
	return export.NewProviderSnapshot(export.ProviderSnapshot{
		Provider: export.ProviderGrok, Availability: export.Unavailable, TokenSource: export.TokensThisMac,
	})
}

// claudeWidgetProvider is the Claude block from the live Claude state.
func claudeWidgetProvider(g ClaudeState, p store.Preferences) export.ProviderSnapshot {
	if g.Widget != nil {
		w := *g.Widget
		if !p.ShowsModelSpecificQuotaUsage {
			var std []export.WidgetQuota
			for _, q := range w.Quota {
				if q.Source == export.QuotaStandard {
					std = append(std, q)
				}
			}
			w.Quota = std
		}
		return export.NewProviderSnapshot(w)
	}
	return export.NewProviderSnapshot(export.ProviderSnapshot{
		Provider: export.ProviderClaude, Availability: export.Unavailable, TokenSource: export.TokensThisMac,
	})
}

// meterExportProvider maps a quota-only provider to its export identifier.
func meterExportProvider(p store.Provider) export.Provider {
	switch p {
	case store.ProviderCursor:
		return export.ProviderCursor
	case store.ProviderKimi:
		return export.ProviderKimi
	case store.ProviderZai:
		return export.ProviderZai
	case store.ProviderAgy:
		return export.ProviderAgy
	}
	return export.Provider(p)
}

// meterAvailability maps a meter availability to the export value.
func meterAvailability(a MeterAvailability) export.Availability {
	switch a {
	case MeterReady:
		return export.Available
	case MeterLoading, MeterError:
		return export.Unavailable
	}
	return export.NotLoggedIn
}

// meterHasData reports whether a meter state carries anything to export; a
// provider without a detected credential stays absent from the files.
func meterHasData(m MeterState) bool {
	return m.Availability != MeterNotConfigured
}

// meterWidgetSnapshot is the export block of a quota-only provider. Model-
// specific rows are dropped when modelSpecific is false (the widget's
// preference filter); the status export passes true so readers filter.
func meterWidgetSnapshot(p store.Provider, m MeterState, modelSpecific bool) (export.ProviderSnapshot, bool) {
	if !meterHasData(m) {
		return export.ProviderSnapshot{}, false
	}
	out := export.ProviderSnapshot{
		Provider: meterExportProvider(p), Availability: meterAvailability(m.Availability),
		TokenSource: export.TokensThisMac,
	}
	if m.Plan != "" {
		plan := m.Plan
		out.Plan = &plan
	}
	out.UpdatedAt = timePtr(m.FetchedAt)
	for _, q := range m.Meters {
		if q.Source == SourceModelSpecific && !modelSpecific {
			continue
		}
		out.Quota = append(out.Quota, widgetQuota(q))
	}
	return export.NewProviderSnapshot(out), true
}

// meterWidgetProviders appends the quota-only providers that have data.
func meterWidgetProviders(snap Snapshot, prefs store.Preferences) []export.ProviderSnapshot {
	var out []export.ProviderSnapshot
	for _, p := range MeterProviders() {
		if ms, ok := snap.Meters[p]; ok {
			if w, has := meterWidgetSnapshot(p, ms, prefs.ShowsModelSpecificQuotaUsage); has {
				out = append(out, w)
			}
		}
	}
	return out
}

// makeWidgetSnapshot builds the widget file content from a snapshot.
func makeWidgetSnapshot(snap Snapshot, p store.Preferences, now time.Time) export.Snapshot {
	providers := []export.ProviderSnapshot{
		codexWidgetProvider(snap.Codex), grokWidgetProvider(snap.Grok, p), claudeWidgetProvider(snap.Claude, p),
	}
	providers = append(providers, meterWidgetProviders(snap, p)...)
	return export.NewSnapshot(now, snap.Language, providers, snap.ResetToday.Widget)
}

// WidgetSnapshot builds the widget snapshot for the current state.
func (s *Service) WidgetSnapshot(now time.Time) export.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return makeWidgetSnapshot(s.buildLocked(now), s.prefs, now)
}

// writeWidget hands the widget snapshot to the sink. Called without s.mu.
func (s *Service) writeWidget(snap Snapshot, now time.Time) {
	if s.opts.WidgetSink == nil {
		return
	}
	s.mu.Lock()
	p := s.prefs
	s.mu.Unlock()
	if err := s.opts.WidgetSink(makeWidgetSnapshot(snap, p, now)); err != nil {
		s.mu.Lock()
		s.lastErr = err.Error()
		s.publishLocked()
		s.mu.Unlock()
	}
}

// ReportWidgetError makes asynchronous WidgetKit failures visible in the panel.
func (s *Service) ReportWidgetError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	if s.lastErr != err.Error() {
		s.lastErr = err.Error()
		s.publishLocked()
	}
	s.mu.Unlock()
}
