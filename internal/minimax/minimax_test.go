package minimax

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

const testSecret = "minimax-secret-key-1234"

var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// serveRemains spins up a test server serving body/status and returning the
// received Authorization header.
func serveRemains(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != remainsPath {
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

func TestBaseURL(t *testing.T) {
	for region, want := range map[string]string{
		RegionCN: "https://api.minimaxi.com",
		"":       "https://api.minimaxi.com",
		"bogus":  "https://api.minimaxi.com",
		"intl":   "https://api.minimax.io",
	} {
		if got := BaseURL(region); got != want {
			t.Fatalf("BaseURL(%q) = %q, want %q", region, got, want)
		}
	}
}

func TestFetchTokenPlan(t *testing.T) {
	srv, gotAuth := serveRemains(t, http.StatusOK, fixture(t, "success-token-plan.json"))
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if *gotAuth != "Bearer "+testSecret {
		t.Fatalf("Authorization = %q, want Bearer <secret>", *gotAuth)
	}
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s), want ok", state.Status, state.Message)
	}
	if !state.FetchedAt.Equal(testNow) {
		t.Fatalf("FetchedAt = %v, want %v", state.FetchedAt, testNow)
	}
	five := byID(t, state.Limits, "general:5h")
	if five.Unit != "percent" || !five.HasPercent || math.Abs(five.Percent-10) > 1e-9 {
		t.Fatalf("general:5h = %+v, want 10%%", five)
	}
	if five.WindowMinutes != meter.FiveHourMinutes {
		t.Fatalf("general:5h window minutes = %d, want 300", five.WindowMinutes)
	}
	if !five.HasReset || !five.ResetsAt.Equal(time.UnixMilli(1791349200000)) {
		t.Fatalf("general:5h reset = %v has %v", five.ResetsAt, five.HasReset)
	}
	if meter.Has(five.Used) || meter.Has(five.LimitValue) || meter.Has(five.Remaining) {
		t.Fatalf("general:5h counts must be NaN, got %+v", five)
	}
	weekly := byID(t, state.Limits, "general:7d")
	if !weekly.HasPercent || math.Abs(weekly.Percent-22) > 1e-9 {
		t.Fatalf("general:7d = %+v, want 22%%", weekly)
	}
	if weekly.WindowMinutes != meter.WeeklyMinutes {
		t.Fatalf("general:7d window minutes = %d, want 10080", weekly.WindowMinutes)
	}
	if !weekly.HasReset || !weekly.ResetsAt.Equal(time.UnixMilli(1791849600000)) {
		t.Fatalf("general:7d reset = %v has %v", weekly.ResetsAt, weekly.HasReset)
	}
}

func TestFetchBusinessFailureAuth(t *testing.T) {
	srv, _ := serveRemains(t, http.StatusOK, fixture(t, "key-rejected-http-200.json"))
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusAuth {
		t.Fatalf("status = %q (%s), want auth", state.Status, state.Message)
	}
	if !strings.Contains(state.Message, "login fail") {
		t.Fatalf("message = %q, want base_resp status_msg", state.Message)
	}
	if strings.Contains(state.Message, testSecret) {
		t.Fatalf("message leaks key: %q", state.Message)
	}
}

func TestFetchBusinessFailureAPI(t *testing.T) {
	srv, _ := serveRemains(t, http.StatusOK, `{"base_resp":{"status_code":1003,"status_msg":"quota exhausted"}}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusAPI {
		t.Fatalf("status = %q (%s), want api", state.Status, state.Message)
	}
}

func TestFetchUnauthorized(t *testing.T) {
	srv, _ := serveRemains(t, http.StatusUnauthorized, `{"base_resp":{"status_code":1004}}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusAuth {
		t.Fatalf("status = %q (%s), want auth", state.Status, state.Message)
	}
	if strings.Contains(state.Message, testSecret) {
		t.Fatalf("message leaks key: %q", state.Message)
	}
}

func TestFetchParseFailure(t *testing.T) {
	srv, _ := serveRemains(t, http.StatusOK, `{not json`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusParse {
		t.Fatalf("status = %q (%s), want parse", state.Status, state.Message)
	}
}

func TestNormalizeMissingModelName(t *testing.T) {
	srv, _ := serveRemains(t, http.StatusOK, `{"model_remains":[{"current_interval_remaining_percent":50}]}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusParse {
		t.Fatalf("status = %q (%s), want parse", state.Status, state.Message)
	}
}

func TestNormalizeSkipsWindowWithoutPercent(t *testing.T) {
	srv, _ := serveRemains(t, http.StatusOK,
		`{"model_remains":[{"model_name":"general","current_interval_remaining_percent":80}]}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s), want ok", state.Status, state.Message)
	}
	if len(state.Limits) != 1 || state.Limits[0].ID != "general:5h" {
		t.Fatalf("limits = %+v, want only the 5h window", state.Limits)
	}
}

func TestFetchNoKey(t *testing.T) {
	state := Fetch(context.Background(), nil, "", RegionCN, testNow)
	if state.Status != meter.StatusNotConfigured {
		t.Fatalf("status = %q, want notConfigured", state.Status)
	}
}
