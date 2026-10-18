// Package meter holds the shared vocabulary of the read-only quota
// providers (cursor, kimi, zai, agy): the normalized result types, the
// failure classification of one fetch, and the HTTP and redaction helpers
// every provider shares. It is pure business logic: no UI, no localization,
// no file access and no global state. HTTP goes through a caller supplied
// Doer so proxy and TLS policy stay with the caller; quota providers never
// follow redirects (see NoRedirectClient). Secrets are redacted from every
// error message and result field.
package meter

import (
	"net/http"
	"time"
)

// Doer sends one HTTP request; *http.Client satisfies it. The caller owns
// proxy, TLS and redirect policy.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Status classifies the outcome of one quota fetch. NotConfigured means no
// credential was found and nothing was requested; the error classes mirror
// the reference implementation in ../aimeter.
type Status string

// Status values.
const (
	StatusOK            Status = "ok"
	StatusNotConfigured Status = "notConfigured"
	StatusAuth          Status = "auth"
	StatusRateLimited   Status = "rateLimited"
	StatusHTTP          Status = "http"
	StatusNetwork       Status = "network"
	StatusTimeout       Status = "timeout"
	StatusAPI           Status = "api"
	StatusParse         Status = "parse"
	StatusSubprocess    Status = "subprocess"
)

// Window lengths in minutes shared by the quota providers.
const (
	FiveHourMinutes  = 300
	WeeklyMinutes    = 7 * 24 * 60
	MonthlyMinutes   = 30 * 24 * 60
	maxBodyBytes     = 1 << 20
	defaultUserAgent = "Quosparr"
)

// Limit is one normalized quota pool. Numeric fields use NaN for "absent";
// Has reports presence. Percent pools (unit "percent") carry only Percent.
type Limit struct {
	// ID is the stable pool identifier (for example "plan", "tokens_5h");
	// Title is the display label.
	ID    string
	Title string
	// Unit is one of usd, tokens, credits, requests, percent, unknown.
	Unit          string
	WindowMinutes int
	Used          float64
	LimitValue    float64
	Remaining     float64
	Percent       float64
	HasPercent    bool
	ResetsAt      time.Time
	HasReset      bool
}

// UsageModel is one model's token total inside a Usage summary.
type UsageModel struct {
	Name   string
	Tokens float64
}

// UsageFeature is one feature's usage counter inside a Usage summary.
type UsageFeature struct {
	ID   string
	Used float64
}

// Usage is a fixed-lookback usage summary (zai model-usage); distinct from
// Limit, which is a quota pool.
type Usage struct {
	Start, End  time.Time
	Granularity string
	TotalCalls  float64
	TotalTokens float64
	Models      []UsageModel
	Features    []UsageFeature
}

// State is the business state of one quota provider. It never holds a
// credential and is safe to cache and serialize.
type State struct {
	Status  Status
	Message string
	Plan    string
	Limits  []Limit
	Usage   *Usage
	// FetchedAt is the completion time of the last successful fetch.
	FetchedAt time.Time
}

// NotConfigured is the state of a provider without a credential.
func NotConfigured() State { return State{Status: StatusNotConfigured} }

// Failed builds the error state with an already redacted message.
func Failed(s Status, message string) State {
	if s == StatusOK {
		s = StatusAPI
	}
	return State{Status: s, Message: message}
}
