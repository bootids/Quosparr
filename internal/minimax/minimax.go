// Package minimax reads the token-plan quota of a MiniMax coding plan from
// /v1/token_plan/remains on the region endpoint (cn: api.minimaxi.com,
// intl: api.minimax.io) with a Bearer API key resolved from the explicit
// store, MINIMAX_CODING_API_KEY / MINIMAX_API_KEY or the read-only
// ~/.config/minimax/config.json discovery file.
//
// The package is pure business logic: no UI, no localization, no logging and
// no global state. HTTP goes through a caller supplied Doer so proxy and TLS
// policy stay with the caller. The key is only ever sent to the region
// endpoint it belongs to and is redacted from every error message.
package minimax

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

// Regions of the MiniMax API; anything but intl resolves to the cn base.
const (
	RegionCN   = "cn"
	RegionIntl = "intl"
)

const (
	baseCN      = "https://api.minimaxi.com"
	baseIntl    = "https://api.minimax.io"
	userAgent   = "Quosparr"
	remainsPath = "/v1/token_plan/remains"
)

// BaseURL returns the API base of the region; intl has its own domain,
// everything else (including the default empty region) uses the cn base.
func BaseURL(region string) string {
	if region == RegionIntl {
		return baseIntl
	}
	return baseCN
}

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

// tokenPlanResponse is the shape of /v1/token_plan/remains. Count and status
// fields are ignored.
type tokenPlanResponse struct {
	ModelRemains []modelRemain `json:"model_remains"`
	BaseResp     baseResp      `json:"base_resp"`
}

// baseResp carries the business status of a 2xx response.
type baseResp struct {
	StatusCode flexFloat `json:"status_code"`
	StatusMsg  string    `json:"status_msg"`
}

// modelRemain is one model's remaining quota in the 5h and 7d windows.
type modelRemain struct {
	ModelName         string    `json:"model_name"`
	StartTime         flexFloat `json:"start_time"`
	EndTime           flexFloat `json:"end_time"`
	IntervalRemaining flexFloat `json:"current_interval_remaining_percent"`
	WeeklyStartTime   flexFloat `json:"weekly_start_time"`
	WeeklyEndTime     flexFloat `json:"weekly_end_time"`
	WeeklyRemaining   flexFloat `json:"current_weekly_remaining_percent"`
}

// Fetch reads the MiniMax token plan of the region endpoint and returns the
// normalized state. region "intl" targets api.minimax.io, anything else
// (including "") targets api.minimaxi.com. A missing key is notConfigured; a
// 2xx business failure with base_resp.status_code 1004 is an auth failure,
// any other non-zero status an api failure. Failure messages are redacted
// and never contain the key or a response body.
func Fetch(ctx context.Context, client meter.Doer, key, region string, now time.Time) meter.State {
	if key == "" {
		return meter.NotConfigured()
	}
	return fetch(ctx, client, BaseURL(region), key, now)
}

// fetch is Fetch against an explicit base URL so tests can target a local
// server.
func fetch(ctx context.Context, client meter.Doer, base, key string, now time.Time) meter.State {
	headers := map[string]string{
		"Authorization": "Bearer " + key,
		"Content-Type":  "application/json",
	}
	body, _, err := meter.GetJSON(ctx, client, base+remainsPath, userAgent, headers)
	if err != nil {
		return meter.Failed(fetchStatus(err), meter.Redact(err.Error(), key))
	}
	var doc tokenPlanResponse
	if err := json.Unmarshal(body, &doc); err != nil {
		return meter.Failed(meter.StatusParse, meter.Redact("minimax: decode token plan response", key))
	}
	if doc.BaseResp.StatusCode.ok && doc.BaseResp.StatusCode.v != 0 {
		status := meter.StatusAPI
		if doc.BaseResp.StatusCode.v == 1004 {
			status = meter.StatusAuth
		}
		msg := doc.BaseResp.StatusMsg
		if msg == "" {
			msg = "token plan request rejected"
		}
		return meter.Failed(status, meter.Redact(msg, key))
	}
	limits, perr := normalize(doc.ModelRemains)
	if perr != nil {
		return meter.Failed(meter.StatusParse, meter.Redact(perr.Error(), key))
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

// normalize turns the model_remains array into percent rows: two windows per
// model, 5h and 7d. A window without percent data is skipped (not an error);
// a model without model_name fails the whole response.
func normalize(remains []modelRemain) ([]meter.Limit, error) {
	var limits []meter.Limit
	for _, m := range remains {
		if m.ModelName == "" {
			return nil, fmt.Errorf("minimax: model remain without model_name")
		}
		if m.IntervalRemaining.ok {
			limits = append(limits, percentLimit(m.ModelName+":5h", meter.FiveHourMinutes, m.IntervalRemaining.v, m.StartTime, m.EndTime))
		}
		if m.WeeklyRemaining.ok {
			limits = append(limits, percentLimit(m.ModelName+":7d", meter.WeeklyMinutes, m.WeeklyRemaining.v, m.WeeklyStartTime, m.WeeklyEndTime))
		}
	}
	return limits, nil
}

// percentLimit builds one window row: the percent shown is the used share
// (100 − remaining), the reset is the window end and the window length is
// derived from the end−start span when both timestamps are present.
func percentLimit(id string, defaultMinutes int, remaining float64, start, end flexFloat) meter.Limit {
	l := meter.Limit{
		ID:            id,
		Unit:          "percent",
		WindowMinutes: defaultMinutes,
		Used:          meter.NaN(),
		LimitValue:    meter.NaN(),
		Remaining:     meter.NaN(),
		Percent:       100 - remaining,
		HasPercent:    true,
	}
	if end.ok {
		l.ResetsAt, l.HasReset = stampTime(end.v), true
		if start.ok {
			l.WindowMinutes = int(stampTime(end.v).Sub(stampTime(start.v)).Minutes())
		}
	}
	return l
}

// stampTime converts an epoch timestamp: values of 1e12 and above are
// milliseconds, smaller ones seconds.
func stampTime(v float64) time.Time {
	if v >= 1e12 {
		return time.UnixMilli(int64(v)).UTC()
	}
	return time.Unix(int64(v), 0).UTC()
}
