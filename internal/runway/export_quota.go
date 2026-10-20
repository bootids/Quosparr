package runway

import (
	"fmt"
	"time"

	"github.com/licoy/quosparr/internal/codexapi"
	"github.com/licoy/quosparr/internal/export"
)

func (s *Service) quotaReportLocked(snap Snapshot, now time.Time) *export.QuotaReport {
	r := &export.QuotaReport{SchemaVersion: export.QuotaSchemaVersion,
		GeneratedAt: now.UTC(), Providers: map[string]export.QuotaProvider{}}
	c := export.QuotaProvider{Status: "loading"}
	switch {
	case snap.Codex.APIKeyMode:
		c.Status = "unsupported"
	case !snap.Codex.Authenticated:
		c.Status = "not_configured"
	case s.quota != nil:
		q := s.quota
		c.Status, c.Plan, c.FetchedAt = "ok", q.Plan, timePtr(q.UpdatedAt)
		c.Limits = append(c.Limits, quotaRateWindow("primary", "standard", q.Primary))
		if q.Secondary != nil {
			c.Limits = append(c.Limits, quotaRateWindow("secondary", "standard", *q.Secondary))
		}
		for i, w := range q.AdditionalWindows {
			l := quotaRateWindow(fmt.Sprintf("additional_%d", i+1), "model_specific", w.Window)
			l.Title = w.Name
			c.Limits = append(c.Limits, l)
		}
	}
	if c.Status != "unsupported" && (s.quotaErr != "" || s.statusFail) {
		quotaFetchFailed(&c)
	}
	r.Providers["codex"] = c

	g := snap.Grok
	gp := quotaFromMeters(g.Meters, g.Plan, g.UpdatedAt)
	switch g.Availability {
	case GrokReady:
	case GrokLoading:
		gp.Status = "loading"
	case GrokNotLoggedIn:
		gp.Status = "not_configured"
	default:
		quotaFetchFailed(&gp)
	}
	if g.QuotaFailed {
		quotaFetchFailed(&gp)
	}
	r.Providers["grok"] = gp

	cl := snap.Claude
	cp := quotaFromMeters(cl.Meters, cl.Plan, cl.UpdatedAt)
	switch {
	case cl.API != nil || cl.Availability == ClaudeAPIKeyMode:
		cp = export.QuotaProvider{Status: "unsupported"}
	case cl.Availability == ClaudeReady:
	case cl.Availability == ClaudeLoading:
		cp.Status = "loading"
	case cl.Availability == ClaudeNotLoggedIn:
		cp.Status = "not_configured"
	default:
		quotaFetchFailed(&cp)
	}
	if cl.QuotaFailed && cp.Status != "unsupported" {
		quotaFetchFailed(&cp)
	}
	r.Providers["claude"] = cp
	for _, name := range MeterProviders() {
		ms, ok := snap.Meters[name]
		p := export.QuotaProvider{Status: "not_configured"}
		if ok && ms.Quota != nil {
			p = *ms.Quota
		} else if ok && ms.Availability != MeterNotConfigured {
			p.Status = "loading"
		}
		r.Providers[string(name)] = p
	}
	return r
}

func quotaFetchFailed(p *export.QuotaProvider) {
	p.Status, p.Error = "error", "fetch_failed"
	if p.FetchedAt != nil {
		p.Status = "stale"
	}
}

func quotaRateWindow(id, scope string, w codexapi.RateWindow) export.QuotaPool {
	used := float64(w.UsedPercent)
	return export.QuotaPool{ID: id, Scope: scope, Unit: "percent", UsedPercent: &used,
		DurationSeconds: w.WindowMinutes * 60, ResetsAt: timePtr(w.ResetsAt)}
}

func quotaFromMeters(meters []Meter, plan string, at time.Time) export.QuotaProvider {
	p := export.QuotaProvider{Status: "ok", Plan: plan, FetchedAt: timePtr(at)}
	for i, m := range meters {
		if m.Balance {
			continue
		}
		scope := "standard"
		if m.Source == SourceModelSpecific {
			scope = "model_specific"
		}
		used := float64(m.UsedPercent)
		p.Limits = append(p.Limits, export.QuotaPool{
			ID: fmt.Sprintf("window_%d", i+1), Title: m.Title, Scope: scope, Group: m.Group,
			Unit: "percent", UsedPercent: &used, DurationSeconds: m.WindowMinutes * 60,
			ResetsAt: timePtr(m.ResetsAt),
		})
	}
	return p
}
