package pixellab

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

const testSecret = "pixellab-secret-key-1234"

var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// serveBalance serves status/body and captures the received Authorization
// header.
func serveBalance(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v2/balance" {
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

func TestFetchSuccess(t *testing.T) {
	srv, gotAuth := serveBalance(t, http.StatusOK, fixture(t, "balance-success.json"))
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if *gotAuth != "Bearer "+testSecret {
		t.Fatalf("Authorization = %q, want Bearer <secret>", *gotAuth)
	}
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s)", state.Status, state.Message)
	}
	if state.Plan != "Tier 1: Pixel Apprentice" {
		t.Fatalf("plan = %q", state.Plan)
	}
	if !state.FetchedAt.Equal(testNow) {
		t.Fatalf("FetchedAt = %v, want %v", state.FetchedAt, testNow)
	}
	if len(state.Balances) != 1 {
		t.Fatalf("balances = %+v, want one row", state.Balances)
	}
	bal := state.Balances[0]
	if bal.Currency != "usd" || bal.Total != 0.0 {
		t.Fatalf("balance = %+v, want usd 0.0", bal)
	}
	if !math.IsNaN(bal.Granted) || !math.IsNaN(bal.ToppedUp) {
		t.Fatalf("balance granted/topped_up = %v/%v, want NaN", bal.Granted, bal.ToppedUp)
	}
	if len(state.Limits) != 1 {
		t.Fatalf("limits = %+v, want one row", state.Limits)
	}
	limit := state.Limits[0]
	if limit.ID != "generations" || limit.Title != "Generations" || limit.Unit != "generations" {
		t.Fatalf("limit identity = %+v", limit)
	}
	if limit.WindowMinutes != 0 || limit.HasReset {
		t.Fatalf("limit window/reset = %d/%v, want 0/false", limit.WindowMinutes, limit.HasReset)
	}
	if limit.Used != 78 || limit.LimitValue != 2000 || limit.Remaining != 1922 {
		t.Fatalf("limit counts = used %v limit %v remaining %v", limit.Used, limit.LimitValue, limit.Remaining)
	}
	if !limit.HasPercent || math.Abs(limit.Percent-3.9) > 1e-9 {
		t.Fatalf("limit percent = %v (has %v), want 3.9", limit.Percent, limit.HasPercent)
	}
}

func TestFetchAuthFailure(t *testing.T) {
	srv, _ := serveBalance(t, http.StatusUnauthorized, `{"message":"invalid api key"}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusAuth {
		t.Fatalf("status = %q (%s), want auth", state.Status, state.Message)
	}
	if strings.Contains(state.Message, testSecret) {
		t.Fatalf("message leaks secret: %q", state.Message)
	}
}

func TestFetchBadJSON(t *testing.T) {
	srv, _ := serveBalance(t, http.StatusOK, `not-json`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusParse {
		t.Fatalf("status = %q (%s), want parse", state.Status, state.Message)
	}
	if strings.Contains(state.Message, testSecret) {
		t.Fatalf("message leaks secret: %q", state.Message)
	}
}

func TestFetchNonObjectResponse(t *testing.T) {
	srv, _ := serveBalance(t, http.StatusOK, `[1,2,3]`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusParse {
		t.Fatalf("status = %q (%s), want parse", state.Status, state.Message)
	}
}

func TestFetchEmptyObject(t *testing.T) {
	srv, _ := serveBalance(t, http.StatusOK, `{}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusParse {
		t.Fatalf("status = %q (%s), want parse", state.Status, state.Message)
	}
}

func TestFetchZeroLimitHasNoPercent(t *testing.T) {
	body := `{"subscription":{"plan":"free","generations":0,"total":0}}`
	srv, _ := serveBalance(t, http.StatusOK, body)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s)", state.Status, state.Message)
	}
	if len(state.Limits) != 1 || state.Limits[0].HasPercent {
		t.Fatalf("limits = %+v, want one row without percent", state.Limits)
	}
}

func TestFetchMissingKey(t *testing.T) {
	state := Fetch(context.Background(), nil, "", testNow)
	if state.Status != meter.StatusNotConfigured {
		t.Fatalf("status = %q, want notConfigured", state.Status)
	}
}
