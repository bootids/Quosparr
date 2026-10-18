// Package zai queries the quota and the 7-day model usage of a Z.ai coding
// plan from an API key, and keeps the local library of Z.ai API-key accounts.
//
// The package is pure business logic: no UI, no localization, no logging and
// no global state. HTTP goes through a caller supplied Doer so proxy and TLS
// policy stay with the caller. The API key is only ever sent to the region
// endpoint it belongs to and is redacted from every error message and result
// field. Values are copied verbatim; upstream inconsistencies (for example a
// remaining that is not limit minus used) are preserved, never repaired.
package zai

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

const (
	baseIntl  = "https://api.z.ai"
	baseCN    = "https://open.bigmodel.cn"
	userAgent = "Quosparr"

	quotaPath       = "/api/monitor/usage/quota/limit"
	modelUsagePath  = "/api/monitor/usage/model-usage"
	usageLookback7d = 7 * 24 * time.Hour
	// naiveStampLayout is the naive timestamp format the model-usage
	// endpoint expects ("YYYY-MM-DD+HH:MM:SS"); it is percent-encoded on
	// the wire so '+' becomes %2B and ':' becomes %3A.
	naiveStampLayout = "2006-01-02+15:04:05"
)

// EnvKey returns the configured Z.ai API key from ZAI_API_KEY, falling back
// to Z_AI_API_KEY. It is empty when neither variable holds a key.
func EnvKey() string {
	if k := strings.TrimSpace(os.Getenv("ZAI_API_KEY")); k != "" {
		return k
	}
	return strings.TrimSpace(os.Getenv("Z_AI_API_KEY"))
}

func baseURL(region string) string {
	if region == RegionCN {
		return baseCN
	}
	return baseIntl
}

func authValue(scheme, key string) string {
	if scheme == SchemeBearer {
		return "Bearer " + key
	}
	return key
}

func quotaHeaders(key, scheme string) map[string]string {
	return map[string]string{
		"authorization": authValue(scheme, key),
		"content-type":  "application/json",
	}
}

// Fetch reads the quota of one Z.ai coding plan. region is "cn" or "intl"
// (anything else is treated as intl); authScheme is "bearer" or "raw".
// Failures come back as a meter.State classified by meter.Status with a
// message that never contains the key or a response body.
func Fetch(ctx context.Context, client meter.Doer, key, region, authScheme string, now time.Time) meter.State {
	base := baseURL(region)
	body, _, err := meter.GetJSON(ctx, client, base+quotaPath, userAgent, quotaHeaders(key, authScheme))
	if err != nil {
		return meter.Failed(fetchStatus(err), meter.Redact(err.Error(), key))
	}
	doc, ok := parseQuota(body)
	if !ok {
		return meter.Failed(meter.StatusParse, meter.Redact("zai quota response is unreadable", key))
	}
	if !doc.success() {
		status := meter.StatusAPI
		if doc.Code != nil && *doc.Code == 401 {
			status = meter.StatusAuth
		}
		msg := doc.Msg
		if msg == "" {
			msg = "zai quota request failed"
		}
		return meter.Failed(status, meter.Redact(msg, key))
	}
	if doc.Data == nil || doc.Data.Limits == nil {
		return meter.Failed(meter.StatusParse, meter.Redact("zai quota response missing data.limits", key))
	}
	state := meter.State{Status: meter.StatusOK}
	state.Plan = doc.Data.Level
	state.Limits = make([]meter.Limit, 0, len(doc.Data.Limits))
	var usageDetails []json.RawMessage
	for _, l := range doc.Data.Limits {
		state.Limits = append(state.Limits, normalizeLimit(l))
		usageDetails = append(usageDetails, l.UsageDetails...)
	}
	state.Usage = fetchModelUsage(ctx, client, base, key, authScheme, now, usageDetails)
	state.FetchedAt = now
	return state
}

func fetchStatus(err error) meter.Status {
	if fe, ok := err.(*meter.FetchError); ok {
		return fe.Status
	}
	return meter.StatusNetwork
}

// fetchModelUsage is best-effort: any failure leaves a nil usage so the
// quota state is unaffected.
func fetchModelUsage(ctx context.Context, client meter.Doer, base, key, authScheme string, now time.Time, quotaDetails []json.RawMessage) *meter.Usage {
	end := now.UTC().Truncate(time.Second)
	start := end.Add(-usageLookback7d)
	url := base + modelUsagePath +
		"?startTime=" + meter.PercentEncode(start.Format(naiveStampLayout)) +
		"&endTime=" + meter.PercentEncode(end.Format(naiveStampLayout))
	body, _, err := meter.GetJSON(ctx, client, url, userAgent, quotaHeaders(key, authScheme))
	if err != nil {
		return nil
	}
	var doc struct {
		Success *bool `json:"success"`
		Data    *struct {
			Granularity      string          `json:"granularity"`
			TotalUsage       *usageTotalsRaw `json:"totalUsage"`
			ModelSummaryList []usageModelRaw `json:"modelSummaryList"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Success == nil || !*doc.Success || doc.Data == nil {
		return nil
	}
	totals := doc.Data.TotalUsage
	if totals == nil {
		totals = &usageTotalsRaw{ModelSummaryList: doc.Data.ModelSummaryList}
	}
	u := &meter.Usage{
		Start:       start,
		End:         end,
		Granularity: doc.Data.Granularity,
		TotalCalls:  meter.Float(valp(totals.TotalModelCallCount), totals.TotalModelCallCount != nil),
		TotalTokens: meter.Float(valp(totals.TotalTokensUsage), totals.TotalTokensUsage != nil),
	}
	for _, m := range totals.ModelSummaryList {
		if m.ModelName != "" {
			u.Models = append(u.Models, meter.UsageModel{
				Name:   m.ModelName,
				Tokens: meter.Float(valp(m.TotalTokens), m.TotalTokens != nil),
			})
		}
	}
	for _, d := range quotaDetails {
		var f struct {
			ModelCode string   `json:"modelCode"`
			Usage     *float64 `json:"usage"`
		}
		if json.Unmarshal(d, &f) != nil || f.ModelCode == "" {
			continue
		}
		u.Features = append(u.Features, meter.UsageFeature{ID: f.ModelCode, Used: *f.Usage})
	}
	if totals.TotalModelCallCount == nil && totals.TotalTokensUsage == nil && len(u.Models) == 0 {
		return nil
	}
	return u
}

// parseQuota decodes the shared envelope. ok is false only when the body is
// not JSON; business-level checks stay with the caller.
func parseQuota(body []byte) (quotaDoc, bool) {
	var doc quotaDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return doc, false
	}
	return doc, true
}

type quotaDoc struct {
	Success *bool      `json:"success"`
	Code    *float64   `json:"code"`
	Msg     string     `json:"msg"`
	Data    *quotaData `json:"data"`
}

func (d quotaDoc) success() bool { return d.Success != nil && *d.Success }

type quotaData struct {
	Level  string       `json:"level"`
	Limits []quotaLimit `json:"limits"`
}

type quotaLimit struct {
	Type         string            `json:"type"`
	Usage        *float64          `json:"usage"`
	CurrentValue *float64          `json:"currentValue"`
	Remaining    *float64          `json:"remaining"`
	Percentage   *float64          `json:"percentage"`
	NextReset    json.RawMessage   `json:"nextResetTime"`
	Unit         *float64          `json:"unit"`
	Number       *float64          `json:"number"`
	UsageDetails []json.RawMessage `json:"usageDetails"`
}

type usageTotalsRaw struct {
	TotalModelCallCount *float64        `json:"totalModelCallCount"`
	TotalTokensUsage    *float64        `json:"totalTokensUsage"`
	ModelSummaryList    []usageModelRaw `json:"modelSummaryList"`
}

type usageModelRaw struct {
	ModelName   string   `json:"modelName"`
	TotalTokens *float64 `json:"totalTokens"`
}

func valp(p *float64) float64 {
	if p == nil {
		return meter.NaN()
	}
	return *p
}

func normalizeLimit(l quotaLimit) meter.Limit {
	prefix, unit := limitPrefix(l.Type)
	window, windowMinutes := limitWindow(l.Unit, l.Number)
	used := meter.Float(valp(l.CurrentValue), l.CurrentValue != nil)
	limitValue := meter.Float(valp(l.Usage), l.Usage != nil)
	out := meter.Limit{
		ID:            prefix + "_" + window,
		Title:         prefix + "_" + window,
		Unit:          unit,
		WindowMinutes: windowMinutes,
		Used:          used,
		LimitValue:    limitValue,
		Remaining:     meter.Float(valp(l.Remaining), l.Remaining != nil),
	}
	if l.Percentage != nil {
		out.Percent, out.HasPercent = *l.Percentage, true
	} else if meter.Has(used) && meter.Has(limitValue) && limitValue != 0 {
		out.Percent, out.HasPercent = used/limitValue, true
	}
	if t, ok := parseReset(l.NextReset); ok {
		out.ResetsAt, out.HasReset = t, true
	}
	return out
}

func limitPrefix(typ string) (prefix, unit string) {
	switch typ {
	case "TOKENS_LIMIT":
		return "tokens", "tokens"
	case "CREDIT_LIMIT":
		return "credits", "credits"
	case "TIME_LIMIT":
		return "tools", "requests"
	}
	return strings.ToLower(typ), "unknown"
}

// limitWindow maps the (unit, number) pair to the window label and its
// length in minutes; 0 means no known window length. Only the 5h, 7d and
// monthly windows carry a length.
func limitWindow(unit, number *float64) (string, int) {
	if unit == nil || number == nil {
		return "unknown", 0
	}
	u, n := int(*unit), int(*number)
	switch {
	case u == 3:
		window := strconv.Itoa(n) + "h"
		if n == 5 {
			return window, meter.FiveHourMinutes
		}
		return window, 0
	case u == 4:
		return strconv.Itoa(n) + "d", 0
	case u == 5 && n == 1:
		return "monthly", meter.MonthlyMinutes
	case u == 5:
		return strconv.Itoa(n) + "mo", 0
	case u == 6:
		return "7d", meter.WeeklyMinutes
	}
	return "u" + strconv.Itoa(u) + "n" + strconv.Itoa(n), 0
}

// parseReset accepts an epoch number (milliseconds when >= 1e12, otherwise
// seconds) or an RFC3339 string, both resolved to UTC.
func parseReset(raw json.RawMessage) (time.Time, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return time.Time{}, false
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if json.Unmarshal(raw, &str) != nil {
			return time.Time{}, false
		}
		t, err := time.Parse(time.RFC3339, str)
		if err != nil {
			return time.Time{}, false
		}
		return t.UTC(), true
	}
	var num float64
	if json.Unmarshal(raw, &num) != nil || math.IsNaN(num) || num <= 0 {
		return time.Time{}, false
	}
	if num >= 1e12 {
		return time.UnixMilli(int64(num)).UTC(), true
	}
	sec, frac := math.Modf(num)
	return time.Unix(int64(sec), int64(frac*1e9)).UTC(), true
}
