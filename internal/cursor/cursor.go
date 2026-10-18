// Package cursor reads the subscription quota of a Cursor account from its
// usage-summary endpoint, authenticating with the session token stored in
// Cursor's local state database (see LoadCredential).
//
// no writes anywhere. HTTP goes through a caller supplied Doer; the session
// cookie value is percent-encoded and redacted from every error message.
package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

// usageSummaryURL is the read-only usage endpoint of Cursor.
var usageSummaryURL = "https://cursor.com/api/usage-summary"

// Fetch returns the normalized quota state of the Cursor account behind
// cred. A missing token is not configured; an invalid or expired token is
// an auth failure checked before any HTTP request. now is the clock used
// for expiry and the fetched-at stamp.
func Fetch(ctx context.Context, client meter.Doer, cred Credential, now time.Time) meter.State {
	switch {
	case cred.Token == "":
		return meter.NotConfigured()
	case cred.User == "":
		return meter.Failed(meter.StatusAuth, "cursor token invalid")
	case cred.HasExpiry && now.After(cred.ExpiresAt):
		return meter.Failed(meter.StatusAuth, "cursor token expired")
	}
	cookie := "WorkosCursorSessionToken=" + meter.PercentEncode(cred.User+"::"+cred.Token)
	body, _, err := meter.GetJSON(ctx, client, usageSummaryURL, "Quosparr",
		map[string]string{"cookie": cookie})
	if err != nil {
		return failed(cred, err)
	}
	state, perr := normalize(body, now)
	if perr != nil {
		return failed(cred, perr)
	}
	return state
}

// failed redacts the credential from the error message before it becomes a
// State. Messages never contain a response body.
func failed(cred Credential, err error) meter.State {
	return meter.Failed(statusOf(err), meter.Redact(err.Error(), cred.Token, cred.User+"::"+cred.Token))
}

// statusOf maps an error to its fetch classification.
func statusOf(err error) meter.Status {
	if fe, ok := err.(*meter.FetchError); ok {
		return fe.Status
	}
	return meter.StatusParse
}

// summary is the subset of Cursor's usage-summary response this package reads.
// Amounts are cents; percents are numbers.
type summary struct {
	BillingCycleEnd json.RawMessage `json:"billingCycleEnd"`
	MembershipType  string          `json:"membershipType"`
	IndividualUsage map[string]pool `json:"individualUsage"`
	TeamUsage       *struct {
		Pooled *pool `json:"pooled"`
	} `json:"teamUsage"`
}

// pool is one quota pool of the usage summary.
type pool struct {
	Enabled          *bool    `json:"enabled"`
	Used             *float64 `json:"used"`
	Limit            *float64 `json:"limit"`
	TotalPercentUsed *float64 `json:"totalPercentUsed"`
	AutoPercentUsed  *float64 `json:"autoPercentUsed"`
	APIPercentUsed   *float64 `json:"apiPercentUsed"`
}

// normalize decodes the usage summary into a State.
func normalize(body []byte, now time.Time) (meter.State, error) {
	var s summary
	if err := json.Unmarshal(body, &s); err != nil {
		return meter.State{}, fmt.Errorf("cursor usage summary: invalid JSON")
	}
	state := meter.State{Status: meter.StatusOK, Plan: s.MembershipType, FetchedAt: now}
	reset, hasReset := parseReset(s.BillingCycleEnd)
	limits := make([]meter.Limit, 0, 5)
	add := func(id, title string, p pool) {
		l := moneyLimit(id, title, p)
		l.ResetsAt, l.HasReset = reset, hasReset
		limits = append(limits, l)
	}
	if p, ok := s.IndividualUsage["plan"]; ok {
		add("plan", "Plan", p)
		if p.AutoPercentUsed != nil {
			limits = append(limits, percentLimit("plan_auto", *p.AutoPercentUsed, reset, hasReset))
		}
		if p.APIPercentUsed != nil {
			limits = append(limits, percentLimit("plan_api", *p.APIPercentUsed, reset, hasReset))
		}
	}
	if p, ok := s.IndividualUsage["overall"]; ok {
		add("overall", "", p)
	}
	if p, ok := s.IndividualUsage["onDemand"]; ok && (p.Enabled == nil || *p.Enabled) {
		add("on_demand", "", p)
	}
	if s.TeamUsage != nil && s.TeamUsage.Pooled != nil {
		add("team_pooled", "", *s.TeamUsage.Pooled)
	}
	state.Limits = limits
	return state, nil
}

// moneyLimit converts one pool's cents to a monthly USD limit row.
func moneyLimit(id, title string, p pool) meter.Limit {
	l := meter.Limit{ID: id, Title: title, Unit: "usd", WindowMinutes: meter.MonthlyMinutes,
		Used: meter.NaN(), LimitValue: meter.NaN(), Remaining: meter.NaN(), Percent: meter.NaN()}
	if p.Used != nil {
		l.Used = *p.Used / 100
	}
	if p.Limit != nil {
		l.LimitValue = *p.Limit / 100
	}
	if p.Used != nil && p.Limit != nil {
		l.Remaining = l.LimitValue - l.Used
	}
	switch {
	case p.TotalPercentUsed != nil:
		l.Percent, l.HasPercent = *p.TotalPercentUsed, true
	case p.Used != nil && p.Limit != nil && *p.Limit != 0:
		l.Percent, l.HasPercent = l.Used/l.LimitValue*100, true
	}
	return l
}

// percentLimit builds a monthly percent-only limit row.
func percentLimit(id string, percent float64, reset time.Time, hasReset bool) meter.Limit {
	return meter.Limit{ID: id, Unit: "percent", WindowMinutes: meter.MonthlyMinutes,
		Used: meter.NaN(), LimitValue: meter.NaN(), Remaining: meter.NaN(),
		Percent: percent, HasPercent: true, ResetsAt: reset, HasReset: hasReset}
}

// parseReset decodes billingCycleEnd as RFC3339 or a numeric epoch.
func parseReset(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 {
		return time.Time{}, false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC(), true
		}
		return time.Time{}, false
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		sec, frac := math.Modf(n)
		return time.Unix(int64(sec), int64(frac*1e9)).UTC(), true
	}
	return time.Time{}, false
}
