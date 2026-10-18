// Package deepseek reads the prepaid balances of DeepSeek from
// https://api.deepseek.com/user/balance with a Bearer API key resolved from
// the explicit configuration or DEEPSEEK_API_KEY.
//
// The package is pure business logic: no UI, no localization, no logging and
// no global state. HTTP goes through a caller supplied Doer so proxy and TLS
// policy stay with the caller. The key is only ever sent to
// api.deepseek.com and is redacted from every error message.
package deepseek

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
	baseURL   = "https://api.deepseek.com"
	userAgent = "Quosparr"
)

// flexFloat is a JSON number or numeric string; ok reports presence.
type flexFloat struct {
	v  float64
	ok bool
}

// UnmarshalJSON accepts a JSON number or a string holding a decimal number;
// anything else decodes as absent (NaN).
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

// value returns the parsed number or NaN when absent.
func (f flexFloat) value() float64 {
	if !f.ok {
		return math.NaN()
	}
	return f.v
}

// balanceEntry is one balance_infos[] item.
type balanceEntry struct {
	Currency        string    `json:"currency"`
	TotalBalance    flexFloat `json:"total_balance"`
	GrantedBalance  flexFloat `json:"granted_balance"`
	ToppedUpBalance flexFloat `json:"topped_up_balance"`
}

// Fetch reads the DeepSeek balance endpoint and returns the normalized
// state: one Balance row per balance_infos[] entry, no limits and no plan.
// A missing key is notConfigured. Failure states carry a redacted message
// and never the key or the response body.
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
	body, _, err := meter.GetJSON(ctx, client, base+"/user/balance", userAgent, headers)
	if err != nil {
		return meter.Failed(fetchStatus(err), meter.Redact(err.Error(), key))
	}
	balances, perr := normalize(body)
	if perr != nil {
		return meter.Failed(meter.StatusParse, meter.Redact(perr.Error(), key))
	}
	return meter.State{Status: meter.StatusOK, Balances: balances, FetchedAt: now}
}

// fetchStatus classifies a GetJSON failure; a non-FetchError defect defaults
// to parse so no unclassified status can leak.
func fetchStatus(err error) meter.Status {
	if fe, ok := err.(*meter.FetchError); ok {
		return fe.Status
	}
	return meter.StatusParse
}

// normalize turns the balance response into balance rows. A missing or
// non-array balance_infos is a parse failure.
func normalize(body []byte) ([]meter.Balance, error) {
	var doc struct {
		BalanceInfos []balanceEntry `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if doc.BalanceInfos == nil {
		return nil, errNoBalanceInfos
	}
	balances := make([]meter.Balance, 0, len(doc.BalanceInfos))
	for _, e := range doc.BalanceInfos {
		balances = append(balances, meter.Balance{
			Currency: e.Currency,
			Total:    e.TotalBalance.value(),
			Granted:  e.GrantedBalance.value(),
			ToppedUp: e.ToppedUpBalance.value(),
		})
	}
	return balances, nil
}

// errNoBalanceInfos marks a response without the balance_infos array.
var errNoBalanceInfos = errors.New("deepseek: balance_infos missing or not an array")
