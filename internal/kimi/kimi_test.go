package kimi

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

const testSecret = "kimi-secret-token-1234"

var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// serveUsage spins up a test server serving body/status and returning the
// received Authorization header.
func serveUsage(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/coding/v1/usages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &gotAuth
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(data)
}

func byID(t *testing.T, limits []meter.Limit, id string) meter.Limit {
	t.Helper()
	for _, l := range limits {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("no limit with id %q in %+v", id, limits)
	return meter.Limit{}
}

func TestFetchCounts(t *testing.T) {
	srv, gotAuth := serveUsage(t, http.StatusOK, fixture(t, "success-counts.json"))
	state := fetch(context.Background(), srv.Client(), srv.URL, Credential{Secret: testSecret}, testNow)
	if *gotAuth != "Bearer "+testSecret {
		t.Fatalf("Authorization = %q, want Bearer <secret>", *gotAuth)
	}
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s), want ok", state.Status, state.Message)
	}
	if !state.FetchedAt.Equal(testNow) {
		t.Fatalf("FetchedAt = %v, want %v", state.FetchedAt, testNow)
	}
	weekly := byID(t, state.Limits, "7d")
	if weekly.Unit != "" || weekly.WindowMinutes != meter.WeeklyMinutes {
		t.Fatalf("7d row = %+v", weekly)
	}
	if weekly.Used != 28 || weekly.LimitValue != 100 || weekly.Remaining != 72 {
		t.Fatalf("7d counts = used %v limit %v remaining %v, want 28/100/72", weekly.Used, weekly.LimitValue, weekly.Remaining)
	}
	if !weekly.HasPercent || math.Abs(weekly.Percent-28) > 1e-9 {
		t.Fatalf("7d percent = %v has %v, want 28", weekly.Percent, weekly.HasPercent)
	}
	if !weekly.HasReset || !weekly.ResetsAt.Equal(time.Date(2026, 7, 21, 7, 43, 35, 355947000, time.UTC)) {
		t.Fatalf("7d reset = %v has %v", weekly.ResetsAt, weekly.HasReset)
	}
	five := byID(t, state.Limits, "5h")
	if five.WindowMinutes != meter.FiveHourMinutes {
		t.Fatalf("5h window minutes = %d, want 300", five.WindowMinutes)
	}
	if five.Used != 0 { // limit 100 - remaining 100
		t.Fatalf("5h used = %v, want 0 (limit-remaining)", five.Used)
	}
	if !five.HasReset || !five.ResetsAt.Equal(time.Date(2026, 7, 18, 5, 43, 35, 355947000, time.UTC)) {
		t.Fatalf("5h reset = %v has %v", five.ResetsAt, five.HasReset)
	}
}

func TestFetchRatioPools(t *testing.T) {
	srv, _ := serveUsage(t, http.StatusOK, fixture(t, "success-ratio-pools.json"))
	state := fetch(context.Background(), srv.Client(), srv.URL, Credential{Secret: testSecret}, testNow)
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s)", state.Status, state.Message)
	}
	five := byID(t, state.Limits, "limit_5h")
	if five.Unit != "percent" || !five.HasPercent || five.Percent != 62.5 {
		t.Fatalf("limit_5h = %+v, want 62.5%%", five)
	}
	if five.WindowMinutes != meter.FiveHourMinutes {
		t.Fatalf("limit_5h window = %d, want 300", five.WindowMinutes)
	}
	if five.HasReset {
		t.Fatalf("limit_5h should have no reset")
	}
	weekly := byID(t, state.Limits, "limit_7d")
	if !weekly.HasPercent || weekly.Percent != 12.5 || weekly.WindowMinutes != meter.WeeklyMinutes {
		t.Fatalf("limit_7d = %+v, want 12.5%% / 7d", weekly)
	}
	if !weekly.HasReset || !weekly.ResetsAt.Equal(time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("limit_7d reset = %v", weekly.ResetsAt)
	}
	if len(state.Limits) != 2 {
		t.Fatalf("limits = %+v, want only the 5h and 7d pools", state.Limits)
	}
	for _, l := range state.Limits {
		if l.WindowMinutes != meter.FiveHourMinutes && l.WindowMinutes != meter.WeeklyMinutes {
			t.Fatalf("limit %q window %d survived the filter", l.ID, l.WindowMinutes)
		}
	}
}

// TestNormalizeKeepsOnly5hAnd7d feeds every representation of every window
// (usage block, limits[], usages map incl. monthly) and expects exactly one
// pool per kept window. Both the limits[] and usages rows carry percent data,
// so the limits[] entry wins the tie by rank.
func TestNormalizeKeepsOnly5hAnd7d(t *testing.T) {
	body := `{
		"usage": {"limit": 100, "used": 30, "remaining": 70},
		"limits": [
			{"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"},
			 "detail": {"limit": 100, "remaining": 40}},
			{"window": {"duration": 168, "timeUnit": "TIME_UNIT_HOUR"},
			 "detail": {"limit": 100, "remaining": 10}},
			{"window": {"duration": 1, "timeUnit": "TIME_UNIT_DAY"},
			 "detail": {"limit": 100, "remaining": 90}}
		],
		"usages": {
			"limit_5h": {"used_ratio": 0.625},
			"limit_7d": {"used_ratio": 0.125},
			"limit_month_total": {"used_ratio": 0.0056}
		}
	}`
	limits, err := normalize([]byte(body))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(limits) != 2 {
		t.Fatalf("limits = %+v, want exactly 5h and 7d", limits)
	}
	if limits[0].ID != "5h" || limits[1].ID != "7d" {
		t.Fatalf("ids = %q, %q; want the percent-bearing limits[] rows 5h, 7d", limits[0].ID, limits[1].ID)
	}
	if !limits[0].HasPercent || limits[0].Percent != 60 || !limits[1].HasPercent || limits[1].Percent != 90 {
		t.Fatalf("limits[] percent rows lost: %+v", limits)
	}
}

// TestNormalizeSingleWindow expects a lone 5h pool to pass through alone.
func TestNormalizeSingleWindow(t *testing.T) {
	limits, err := normalize([]byte(`{
		"limits": [
			{"window": {"duration": 5, "timeUnit": "TIME_UNIT_HOUR"},
			 "detail": {"limit": 100, "used": 1, "remaining": 99}}
		]
	}`))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(limits) != 1 || limits[0].ID != "5h" || limits[0].WindowMinutes != meter.FiveHourMinutes {
		t.Fatalf("limits = %+v, want single 5h pool", limits)
	}
}

// TestNormalizePrefersPercent checks a percent-bearing usages row beats a
// percent-less limits[] entry for the same window.
func TestNormalizePrefersPercent(t *testing.T) {
	limits, err := normalize([]byte(`{
		"limits": [
			{"window": {"duration": 168, "timeUnit": "TIME_UNIT_HOUR"},
			 "detail": {"remaining": 10}}
		],
		"usages": {"limit_7d": {"used_ratio": 0.125}}
	}`))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(limits) != 1 || limits[0].ID != "limit_7d" || !limits[0].HasPercent || limits[0].Percent != 12.5 {
		t.Fatalf("limits = %+v, want the percent-bearing limit_7d row", limits)
	}
}

func TestFetchUnauthorized(t *testing.T) {
	srv, _ := serveUsage(t, http.StatusUnauthorized, fixture(t, "unauthorized.json"))
	state := fetch(context.Background(), srv.Client(), srv.URL, Credential{Secret: testSecret}, testNow)
	if state.Status != meter.StatusAuth {
		t.Fatalf("status = %q (%s), want auth", state.Status, state.Message)
	}
	if strings.Contains(state.Message, testSecret) {
		t.Fatalf("message leaks secret: %q", state.Message)
	}
}

func TestFetchParseFailure(t *testing.T) {
	srv, _ := serveUsage(t, http.StatusOK, `{"error":"unauthorized"}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, Credential{Secret: testSecret}, testNow)
	if state.Status != meter.StatusParse {
		t.Fatalf("status = %q (%s), want parse", state.Status, state.Message)
	}
	if strings.Contains(state.Message, testSecret) || strings.Contains(state.Message, "unauthorized") {
		t.Fatalf("message leaks body or secret: %q", state.Message)
	}
}

// TestFetchDuplicateWindowKeepsOnePool feeds two 7d representations (usage
// block and limits[]) and expects the limits[] entry to win the single slot.
func TestFetchDuplicateWindowKeepsOnePool(t *testing.T) {
	body := `{
		"usage": {"limit": 10, "used": 1, "remaining": 9},
		"limits": [
			{"window": {"duration": 168, "timeUnit": "TIME_UNIT_HOUR"},
			 "detail": {"limit": 20, "used": 2, "remaining": 18}}
		]
	}`
	srv, _ := serveUsage(t, http.StatusOK, body)
	state := fetch(context.Background(), srv.Client(), srv.URL, Credential{Secret: testSecret}, testNow)
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s)", state.Status, state.Message)
	}
	if len(state.Limits) != 1 {
		t.Fatalf("limits = %+v, want a single 7d row", state.Limits)
	}
	if state.Limits[0].ID != "7d" || state.Limits[0].Used != 2 {
		t.Fatalf("7d row = %+v, want the limits[] entry with used 2", state.Limits[0])
	}
}

func TestFetchNoSecret(t *testing.T) {
	srv, _ := serveUsage(t, http.StatusOK, fixture(t, "success-counts.json"))
	state := fetch(context.Background(), srv.Client(), srv.URL, Credential{}, testNow)
	if state.Status != meter.StatusNotConfigured {
		t.Fatalf("status = %q, want notConfigured", state.Status)
	}
}

func TestFetchExpiredToken(t *testing.T) {
	srv, _ := serveUsage(t, http.StatusOK, fixture(t, "success-counts.json"))
	cred := Credential{Secret: testSecret, HasExpiry: true, ExpiresAt: testNow}
	state := fetch(context.Background(), srv.Client(), srv.URL, cred, testNow)
	if state.Status != meter.StatusAuth {
		t.Fatalf("status = %q, want auth", state.Status)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kimi-code.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCredentialAPIKeyPriority(t *testing.T) {
	path := writeTemp(t, `{"access_token":"file-token","expires_at":1791332100}`)
	cred, ok, err := LoadCredential("explicit-key", path)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if cred.Secret != "explicit-key" || cred.HasExpiry {
		t.Fatalf("cred = %+v, want explicit key without expiry", cred)
	}
}

func TestLoadCredentialFile(t *testing.T) {
	path := writeTemp(t, `{"access_token":"file-token","refresh_token":"x","expires_at":1791332100.5,"expires_in":900,"scope":"s","token_type":"Bearer"}`)
	cred, ok, err := LoadCredential("", path)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if cred.Secret != "file-token" {
		t.Fatalf("secret = %q", cred.Secret)
	}
	if !cred.HasExpiry || cred.ExpiresAt.Unix() != 1791332100 {
		t.Fatalf("expiry = %v has %v, want 1791332100", cred.ExpiresAt, cred.HasExpiry)
	}
	// No expiry field: usable without HasExpiry.
	path = writeTemp(t, `{"access_token":"file-token"}`)
	cred, ok, err = LoadCredential("", path)
	if err != nil || !ok || cred.HasExpiry || cred.Secret != "file-token" {
		t.Fatalf("no-expiry cred = %+v ok=%v err=%v", cred, ok, err)
	}
}

func TestLoadCredentialMissingFile(t *testing.T) {
	cred, ok, err := LoadCredential("", filepath.Join(t.TempDir(), "absent.json"))
	if ok || err != nil || cred.Secret != "" {
		t.Fatalf("cred=%+v ok=%v err=%v, want not found", cred, ok, err)
	}
}

func TestLoadCredentialEmptyToken(t *testing.T) {
	path := writeTemp(t, `{"access_token":"","expires_at":1791332100}`)
	cred, ok, err := LoadCredential("", path)
	if ok || err != nil || cred.Secret != "" {
		t.Fatalf("cred=%+v ok=%v err=%v, want not found", cred, ok, err)
	}
}

func TestLoadCredentialUndecodable(t *testing.T) {
	path := writeTemp(t, `{not json`)
	_, ok, err := LoadCredential("", path)
	if ok || err == nil {
		t.Fatalf("ok=%v err=%v, want decode error", ok, err)
	}
}

func TestCredentialsPath(t *testing.T) {
	if got := CredentialsPath("/opt/share", "/Users/u"); got != filepath.Join("/opt/share", "credentials", "kimi-code.json") {
		t.Fatalf("shareDir path = %q", got)
	}
	if got := CredentialsPath("", "/Users/u"); got != filepath.Join("/Users/u", ".kimi", "credentials", "kimi-code.json") {
		t.Fatalf("home path = %q", got)
	}
}
