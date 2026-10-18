// Package kimi reads the quota usage of Kimi Code from
// https://api.kimi.com/coding/v1/usages with a Bearer access token resolved
// from the KIMI_CODE_API_KEY or the CLI credentials file.
//
// The package is pure business logic: no UI, no localization, no logging and
// no global state. HTTP goes through a caller supplied Doer so proxy and TLS
// policy stay with the caller. The token is only ever sent to api.kimi.com
// and is redacted from every error message.
package kimi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

const (
	baseURL   = "https://api.kimi.com"
	userAgent = "Quosparr"
)

// flexFloat is a JSON number or numeric string; ok reports presence.
type flexFloat struct {
	v  float64
	ok bool
}

// UnmarshalJSON accepts a JSON number or a string holding a decimal number;
// anything else decodes as absent.
func (f *flexFloat) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" {
		return nil
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	f.v, f.ok = v, true
	return nil
}

// countsPayload is the limit/used/remaining shape shared by the usage object
// and each limits[] detail.
type countsPayload struct {
	Limit     flexFloat `json:"limit"`
	Used      flexFloat `json:"used"`
	Remaining flexFloat `json:"remaining"`
	ResetTime string    `json:"resetTime"`
}

type limitEntry struct {
	Window struct {
		Duration flexFloat `json:"duration"`
		TimeUnit string    `json:"timeUnit"`
	} `json:"window"`
	Detail countsPayload `json:"detail"`
}

// ratioPayload is one usages[key] entry.
type ratioPayload struct {
	UsedRatio flexFloat `json:"used_ratio"`
	ResetTime string    `json:"reset_time"`
}

// Fetch reads the Kimi Code usage endpoint and returns the normalized state.
// A missing secret is notConfigured; an expired token fails as auth. Failure
// states carry a redacted message and never the token or the response body.
func Fetch(ctx context.Context, client meter.Doer, cred Credential, now time.Time) meter.State {
	return fetch(ctx, client, baseURL, cred, now)
}

// fetch is Fetch against an explicit base URL so tests can target a local
// server.
func fetch(ctx context.Context, client meter.Doer, base string, cred Credential, now time.Time) meter.State {
	if cred.Secret == "" {
		return meter.NotConfigured()
	}
	if cred.HasExpiry && !now.Before(cred.ExpiresAt) {
		return meter.Failed(meter.StatusAuth, "kimi token expired")
	}
	headers := map[string]string{"Authorization": "Bearer " + cred.Secret}
	body, _, err := meter.GetJSON(ctx, client, base+"/coding/v1/usages", userAgent, headers)
	if err != nil {
		return meter.Failed(fetchStatus(err), meter.Redact(err.Error(), cred.Secret))
	}
	limits, perr := normalize(body)
	if perr != nil {
		return meter.Failed(meter.StatusParse, meter.Redact(perr.Error(), cred.Secret))
	}
	return meter.State{Status: meter.StatusOK, Limits: limits, FetchedAt: now}
}

// fetchStatus classifies a GetJSON failure; a non-FetchError defect defaults
// to parse so no unclassified status can leak.
func fetchStatus(err error) meter.Status {
	if fe, ok := err.(*meter.FetchError); ok {
		return fe.Status
	}
	return meter.StatusParse
}

// normalize turns the usage response into limit rows.
func normalize(body []byte) ([]meter.Limit, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("kimi: decode usage response")
	}
	_, hasUsage := root["usage"]
	_, hasLimits := root["limits"]
	_, hasUsages := root["usages"]
	if !hasUsage && !hasLimits && !hasUsages {
		return nil, fmt.Errorf("kimi: usage response has no usage, limits or usages")
	}
	ids := make(map[string]int)
	var limits []meter.Limit
	if raw, ok := root["usage"]; ok {
		var usage countsPayload
		if err := json.Unmarshal(raw, &usage); err == nil {
			limits = append(limits, uniqueID(countsLimit("7d", meter.WeeklyMinutes, usage), ids))
		}
	}
	if raw, ok := root["limits"]; ok {
		var entries []limitEntry
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("kimi: decode limits")
		}
		for _, entry := range entries {
			l, err := windowedLimit(entry)
			if err != nil {
				return nil, err
			}
			limits = append(limits, uniqueID(l, ids))
		}
	}
	if raw, ok := root["usages"]; ok {
		pairs, err := ratioPairs(raw)
		if err != nil {
			return nil, err
		}
		for _, pair := range pairs {
			limits = append(limits, uniqueID(ratioLimit(pair.key, pair.value), ids))
		}
	}
	return limits, nil
}

// countsLimit builds a counts row (the 7d usage object or a limits[] detail).
func countsLimit(id string, windowMinutes int, p countsPayload) meter.Limit {
	l := meter.Limit{
		ID: id, Unit: "unknown", WindowMinutes: windowMinutes,
		Used: meter.NaN(), LimitValue: meter.NaN(), Remaining: meter.NaN(), Percent: meter.NaN(),
	}
	if p.Limit.ok {
		l.LimitValue = p.Limit.v
	}
	if p.Remaining.ok {
		l.Remaining = p.Remaining.v
	}
	if p.Used.ok {
		l.Used = p.Used.v
	} else if p.Limit.ok && p.Remaining.ok {
		l.Used = p.Limit.v - p.Remaining.v
	}
	if meter.Has(l.Used) && meter.Has(l.LimitValue) && l.LimitValue != 0 {
		l.Percent = l.Used / l.LimitValue * 100
		l.HasPercent = true
	}
	l.ResetsAt, l.HasReset = parseReset(p.ResetTime)
	return l
}

// windowedLimit builds a row from one limits[] entry: window seconds are
// duration × timeUnit (minute/hour/day only).
func windowedLimit(entry limitEntry) (meter.Limit, error) {
	var factor float64
	switch entry.Window.TimeUnit {
	case "TIME_UNIT_MINUTE":
		factor = 60
	case "TIME_UNIT_HOUR":
		factor = 3600
	case "TIME_UNIT_DAY":
		factor = 86400
	default:
		return meter.Limit{}, fmt.Errorf("kimi: unknown time unit")
	}
	if !entry.Window.Duration.ok {
		return meter.Limit{}, fmt.Errorf("kimi: limits entry without duration")
	}
	seconds := int64(entry.Window.Duration.v * factor)
	id := fmt.Sprintf("%ds", seconds)
	switch seconds {
	case 18000:
		id = "5h"
	case 604800:
		id = "7d"
	}
	return countsLimit(id, int(seconds/60), entry.Detail), nil
}

// ratioLimit builds a percent row from one usages[key] entry.
func ratioLimit(key string, p ratioPayload) meter.Limit {
	l := meter.Limit{
		ID: key, Unit: "percent",
		Used: meter.NaN(), LimitValue: meter.NaN(), Remaining: meter.NaN(),
		HasPercent: true, Percent: meter.NaN(),
	}
	switch {
	case strings.Contains(key, "5h"):
		l.WindowMinutes = meter.FiveHourMinutes
	case strings.Contains(key, "7d"):
		l.WindowMinutes = meter.WeeklyMinutes
	case strings.Contains(key, "month"):
		l.WindowMinutes = meter.MonthlyMinutes
	}
	if p.UsedRatio.ok {
		l.Percent = p.UsedRatio.v * 100
	}
	l.ResetsAt, l.HasReset = parseReset(p.ResetTime)
	return l
}

// ratioPair is one usages object entry in encounter order.
type ratioPair struct {
	key   string
	value ratioPayload
}

// ratioPairs decodes the usages object preserving key order.
func ratioPairs(raw json.RawMessage) ([]ratioPair, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // opening {
		return nil, fmt.Errorf("kimi: decode usages")
	}
	var pairs []ratioPair
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("kimi: decode usages")
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("kimi: decode usages")
		}
		var value ratioPayload
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("kimi: decode usages")
		}
		pairs = append(pairs, ratioPair{key: key, value: value})
	}
	return pairs, nil
}

// uniqueID suffixes duplicate IDs with #2, #3, ... in encounter order.
func uniqueID(l meter.Limit, ids map[string]int) meter.Limit {
	ids[l.ID]++
	if n := ids[l.ID]; n > 1 {
		l.ID = fmt.Sprintf("%s#%d", l.ID, n)
	}
	return l
}

// parseReset parses an RFC3339 reset timestamp into UTC.
func parseReset(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
