package export

import (
	"math"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

const QuotaSchemaVersion = 1

// QuotaReport is the machine-readable, current-account quota contract.
// It intentionally excludes identities, URLs, credentials and free-form errors.
type QuotaReport struct {
	SchemaVersion int                      `json:"schema_version"`
	GeneratedAt   time.Time                `json:"generated_at"`
	Providers     map[string]QuotaProvider `json:"providers"`
}

type QuotaProvider struct {
	Status     string         `json:"status"`
	Error      string         `json:"error,omitempty"`
	Plan       string         `json:"plan,omitempty"`
	FetchedAt  *time.Time     `json:"fetched_at,omitempty"`
	AgeSeconds *int64         `json:"age_seconds,omitempty"`
	Limits     []QuotaPool    `json:"limits,omitempty"`
	Balances   []QuotaBalance `json:"balances,omitempty"`
}

// DurationSeconds is zero when unknown, never inferred from a title.
// Scope distinguishes shared subscription windows from model-specific pools.
type QuotaPool struct {
	ID              string     `json:"id"`
	Title           string     `json:"title,omitempty"`
	Scope           string     `json:"scope"`
	Group           string     `json:"group,omitempty"`
	Unit            string     `json:"unit"`
	UsedPercent     *float64   `json:"used_percent,omitempty"`
	Used            *float64   `json:"used,omitempty"`
	Limit           *float64   `json:"limit,omitempty"`
	Remaining       *float64   `json:"remaining,omitempty"`
	DurationSeconds int        `json:"duration_seconds,omitempty"`
	ResetsAt        *time.Time `json:"resets_at,omitempty"`
}

type QuotaBalance struct {
	Currency string   `json:"currency"`
	Total    *float64 `json:"total,omitempty"`
	Granted  *float64 `json:"granted,omitempty"`
	ToppedUp *float64 `json:"topped_up,omitempty"`
}

func quotaNumber(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

// MeterQuota preserves the business values rather than the UI's rounded bars.
func MeterQuota(s meter.State) QuotaProvider {
	p := QuotaProvider{Status: "error"}
	switch s.Status {
	case meter.StatusNotConfigured:
		p.Status = "not_configured"
		return p
	case meter.StatusOK:
		p.Status = "ok"
	default:
		// Only codes, never upstream messages or response bodies.
		p.Error = "fetch_failed"
		return p
	}
	p.Plan = s.Plan
	if !s.FetchedAt.IsZero() {
		at := s.FetchedAt.UTC()
		p.FetchedAt = &at
	}
	for _, l := range s.Limits {
		q := QuotaPool{ID: l.ID, Title: l.Title, Scope: "standard", Group: l.Group,
			Unit: l.Unit, Used: quotaNumber(l.Used), Limit: quotaNumber(l.LimitValue),
			Remaining: quotaNumber(l.Remaining), DurationSeconds: l.WindowMinutes * 60}
		if l.HasPercent {
			q.UsedPercent = quotaNumber(l.Percent)
		}
		if l.HasReset && !l.ResetsAt.IsZero() {
			at := l.ResetsAt.UTC()
			q.ResetsAt = &at
		}
		p.Limits = append(p.Limits, q)
	}
	for _, b := range s.Balances {
		p.Balances = append(p.Balances, QuotaBalance{Currency: b.Currency,
			Total: quotaNumber(b.Total), Granted: quotaNumber(b.Granted), ToppedUp: quotaNumber(b.ToppedUp)})
	}
	return p
}

// At copies the report and evaluates freshness at read time, not file-write time.
// A past reset cannot be interpreted as a newly replenished quota window.
func (r QuotaReport) At(now time.Time, maxAge time.Duration) QuotaReport {
	out := r
	out.Providers = make(map[string]QuotaProvider, len(r.Providers))
	for name, p := range r.Providers {
		p.AgeSeconds = nil
		if p.FetchedAt != nil {
			age := max(int64(0), int64(now.Sub(*p.FetchedAt).Seconds()))
			p.AgeSeconds = &age
		}
		if p.Status == "ok" {
			switch {
			case p.FetchedAt == nil || p.FetchedAt.IsZero():
				p.Status, p.Error = "stale", "missing_timestamp"
			case p.FetchedAt.After(now.Add(time.Minute)) || r.GeneratedAt.After(now.Add(time.Minute)):
				p.Status, p.Error = "stale", "clock_skew"
			case now.Sub(*p.FetchedAt) > maxAge || now.Sub(r.GeneratedAt) > maxAge:
				p.Status, p.Error = "stale", "max_age_exceeded"
			default:
				for _, l := range p.Limits {
					if l.ResetsAt != nil && !l.ResetsAt.After(now) {
						p.Status, p.Error = "stale", "reset_elapsed"
						break
					}
				}
			}
		}
		out.Providers[name] = p
	}
	return out
}
