// Package pixellab reads the quota balance of PixelLab from
// https://api.pixellab.ai/v2/balance with a Bearer API key resolved from
// the explicit configuration, PIXELLAB_API_KEY or the CLI token file.
//
// The package is pure business logic: no UI, no localization, no logging and
// no global state. HTTP goes through a caller supplied Doer so proxy and TLS
// policy stay with the caller. The key is only ever sent to
// api.pixellab.ai and is redacted from every error message.
package pixellab

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

const (
	baseURL   = "https://api.pixellab.ai"
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

// balancePayload is the /v2/balance response shape. Unknown fields such as
// subscription.type and subscription.status are ignored; nil pointers mean
// the object itself is absent.
type balancePayload struct {
	Credits *struct {
		Type string    `json:"type"`
		USD  flexFloat `json:"usd"`
	} `json:"credits"`
	Subscription *struct {
		Plan        string    `json:"plan"`
		Generations flexFloat `json:"generations"`
		Total       flexFloat `json:"total"`
	} `json:"subscription"`
}

// Fetch reads the PixelLab balance endpoint and returns the normalized
// state. A missing key is notConfigured; an invalid key fails as auth.
// Failure states carry a redacted message and never the key or the response
// body.
func Fetch(ctx context.Context, client meter.Doer, key string, now time.Time) meter.State {
	return fetch(ctx, client, baseURL, key, now)
}

// fetch is Fetch against an explicit base URL so tests can target a local
// server.
func fetch(ctx context.Context, client meter.Doer, base, key string, now time.Time) meter.State {
	if key == "" {
		return meter.NotConfigured()
	}
	headers := map[string]string{"Authorization": "Bearer " + key}
	body, _, err := meter.GetJSON(ctx, client, base+"/v2/balance", userAgent, headers)
	if err != nil {
		return meter.Failed(fetchStatus(err), meter.Redact(err.Error(), key))
	}
	balances, limits, plan, perr := normalize(body)
	if perr != nil {
		return meter.Failed(meter.StatusParse, meter.Redact(perr.Error(), key))
	}
	return meter.State{
		Status:    meter.StatusOK,
		Plan:      plan,
		Limits:    limits,
		Balances:  balances,
		FetchedAt: now,
	}
}

// fetchStatus classifies a GetJSON failure; a non-FetchError defect defaults
// to parse so no unclassified status can leak.
func fetchStatus(err error) meter.Status {
	if fe, ok := err.(*meter.FetchError); ok {
		return fe.Status
	}
	return meter.StatusParse
}

// normalize turns the balance response into one balance row, at most one
// generations limit row and the plan name. The response must be a JSON
// object; a response yielding neither a balance nor a limit is a parse
// failure.
func normalize(body []byte) ([]meter.Balance, []meter.Limit, string, error) {
	var doc balancePayload
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, nil, "", err
	}
	var balances []meter.Balance
	if doc.Credits != nil && doc.Credits.USD.ok {
		balances = append(balances, meter.Balance{
			Currency: doc.Credits.Type,
			Total:    doc.Credits.USD.v,
			Granted:  math.NaN(),
			ToppedUp: math.NaN(),
		})
	}
	var limits []meter.Limit
	if doc.Subscription != nil {
		used := math.NaN()
		if doc.Subscription.Total.ok {
			used = doc.Subscription.Total.v
			if doc.Subscription.Generations.ok {
				used -= doc.Subscription.Generations.v
			}
		}
		limit := meter.Limit{
			ID:         "generations",
			Title:      "Generations",
			Unit:       "generations",
			Remaining:  doc.Subscription.Generations.value(),
			LimitValue: doc.Subscription.Total.value(),
			Used:       used,
		}
		if doc.Subscription.Total.ok && doc.Subscription.Total.v != 0 {
			limit.Percent = used / doc.Subscription.Total.v * 100
			limit.HasPercent = true
		}
		limits = append(limits, limit)
	}
	if len(balances) == 0 && len(limits) == 0 {
		return nil, nil, "", errEmptyBalance
	}
	return balances, limits, doc.plan(), nil
}

// plan reports the subscription plan name, empty when absent.
func (p *balancePayload) plan() string {
	if p.Subscription == nil {
		return ""
	}
	return p.Subscription.Plan
}

// value reports the number with NaN for absent.
func (f flexFloat) value() float64 {
	if !f.ok {
		return math.NaN()
	}
	return f.v
}

// errEmptyBalance marks a response that carried no usable row.
var errEmptyBalance = errors.New("pixellab: balance response has no balances or limits")
