package app

import (
	"time"

	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/grok"
)

// grokBillingToPanel mirrors a decoded Grok billing snapshot into the export
// panel block of the status file. It never fails: fields without a remote
// counterpart stay absent.
func grokBillingToPanel(b *grok.BillingSnapshot) *export.PanelGrokBilling {
	if b == nil {
		return nil
	}
	p := &export.PanelGrokBilling{
		Plan:                 b.Plan,
		IncludedUsagePercent: b.IncludedUsagePercent,
		IncludedLimitCents:   b.IncludedLimitCents,
		IncludedUsedCents:    b.IncludedUsedCents,
		PrepaidBalanceCents:  b.PrepaidBalanceCents,
		OnDemandEnabled:      b.OnDemandEnabled,
		OnDemandUsedCents:    b.OnDemandUsedCents,
		OnDemandLimitCents:   b.OnDemandLimitCents,
		IsUnifiedBillingUser: b.IsUnifiedBillingUser,
		Source:               string(b.Source),
		UpdatedAt:            b.UpdatedAt,
	}
	if b.Period != nil {
		p.Period = &export.PanelBillingPeriod{
			Kind:     string(b.Period.Kind),
			StartsAt: b.Period.StartsAt,
			ResetsAt: b.Period.ResetsAt,
		}
	}
	for _, u := range b.ProductUsage {
		p.ProductUsage = append(p.ProductUsage, export.PanelProductUsage{Product: u.Product, UsagePercent: u.UsagePercent})
	}
	p.ResetCredits = grokResetCreditsToPanel(b.ResetCredits)
	return p
}

func grokResetCreditsToPanel(r *grok.ResetCreditSnapshot) *export.PanelResetCredits {
	if r == nil {
		return nil
	}
	p := &export.PanelResetCredits{AvailableCount: r.AvailableCount, UpdatedAt: r.UpdatedAt}
	for _, tok := range r.Tokens {
		p.Credits = append(p.Credits, export.PanelResetCredit{
			ID:        tok.TokenID,
			CreatedAt: panelTimeValue(tok.ValidityStart),
			ExpiresAt: panelTimeValue(tok.ValidityEnd),
		})
	}
	return p
}

// grokBillingFromPanel reverses grokBillingToPanel: it rebuilds the Grok
// billing snapshot the remote view type-asserts as Detail.
func grokBillingFromPanel(p *export.PanelGrokBilling) *grok.BillingSnapshot {
	if p == nil {
		return nil
	}
	b := &grok.BillingSnapshot{
		Plan:                 p.Plan,
		IncludedUsagePercent: p.IncludedUsagePercent,
		IncludedLimitCents:   p.IncludedLimitCents,
		IncludedUsedCents:    p.IncludedUsedCents,
		PrepaidBalanceCents:  p.PrepaidBalanceCents,
		OnDemandEnabled:      p.OnDemandEnabled,
		OnDemandUsedCents:    p.OnDemandUsedCents,
		OnDemandLimitCents:   p.OnDemandLimitCents,
		IsUnifiedBillingUser: p.IsUnifiedBillingUser,
		Source:               grok.BillingSource(p.Source),
		UpdatedAt:            p.UpdatedAt,
	}
	if p.Period != nil {
		b.Period = &grok.BillingPeriod{
			Kind:     grok.BillingPeriodKind(p.Period.Kind),
			StartsAt: p.Period.StartsAt,
			ResetsAt: p.Period.ResetsAt,
		}
	}
	for _, u := range p.ProductUsage {
		b.ProductUsage = append(b.ProductUsage, grok.BillingProductUsage{Product: u.Product, UsagePercent: u.UsagePercent})
	}
	b.ResetCredits = grokResetCreditsFromPanel(p.ResetCredits)
	return b
}

func grokResetCreditsFromPanel(p *export.PanelResetCredits) *grok.ResetCreditSnapshot {
	if p == nil {
		return nil
	}
	r := &grok.ResetCreditSnapshot{AvailableCount: p.AvailableCount, UpdatedAt: p.UpdatedAt}
	for _, c := range p.Credits {
		r.Tokens = append(r.Tokens, grok.ResetCreditToken{
			TokenID:       c.ID,
			ValidityStart: panelTimePtr(c.CreatedAt),
			ValidityEnd:   panelTimePtr(c.ExpiresAt),
		})
	}
	return r
}

// panelTimeValue flattens an optional time; the zero time marks "absent".
func panelTimeValue(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// panelTimePtr restores the optional time of a flattened panel value.
func panelTimePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
