package deepseek

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

const testKey = "deepseek-secret-key-1234"

var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// serveBalance spins up a test server serving body/status and returning the
// received Authorization header.
func serveBalance(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/user/balance" {
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

func TestFetchMulticurrencyBalances(t *testing.T) {
	srv, gotAuth := serveBalance(t, http.StatusOK, fixture(t, "success-multicurrency.json"))
	state := fetch(context.Background(), srv.Client(), srv.URL, testKey, testNow)
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %v (%s), want OK", state.Status, state.Message)
	}
	if got := *gotAuth; got != "Bearer "+testKey {
		t.Fatalf("Authorization = %q, want Bearer key", got)
	}
	if len(state.Balances) != 2 {
		t.Fatalf("balances = %+v, want 2 rows", state.Balances)
	}
	cny := state.Balances[0]
	if cny.Currency != "CNY" || cny.Total != 110 || cny.Granted != 10 || cny.ToppedUp != 100 {
		t.Fatalf("CNY = %+v, want 110/10/100", cny)
	}
	usd := state.Balances[1]
	if usd.Currency != "USD" || usd.Total != 12.5 || usd.Granted != 2.5 || usd.ToppedUp != 10 {
		t.Fatalf("USD = %+v, want 12.5/2.5/10", usd)
	}
	if !state.FetchedAt.Equal(testNow) {
		t.Fatalf("FetchedAt = %v, want %v", state.FetchedAt, testNow)
	}
	if state.Limits != nil {
		t.Fatalf("Limits = %+v, want none", state.Limits)
	}
}

func TestFetchUnauthorized(t *testing.T) {
	srv, _ := serveBalance(t, http.StatusUnauthorized, `{"error":"bad key"}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testKey, testNow)
	if state.Status != meter.StatusAuth {
		t.Fatalf("status = %v, want auth", state.Status)
	}
}

func TestFetchHTTPError(t *testing.T) {
	srv, _ := serveBalance(t, http.StatusInternalServerError, "boom")
	state := fetch(context.Background(), srv.Client(), srv.URL, testKey, testNow)
	if state.Status != meter.StatusHTTP {
		t.Fatalf("status = %v, want http", state.Status)
	}
}

func TestFetchParseFailure(t *testing.T) {
	srv, _ := serveBalance(t, http.StatusOK, `{not json`)
	if state := fetch(context.Background(), srv.Client(), srv.URL, testKey, testNow); state.Status != meter.StatusParse {
		t.Fatalf("status = %v, want parse", state.Status)
	}
	// A non-array balance_infos is also a parse failure.
	srv2, _ := serveBalance(t, http.StatusOK, `{"is_available": true}`)
	if state := fetch(context.Background(), srv2.Client(), srv2.URL, testKey, testNow); state.Status != meter.StatusParse {
		t.Fatalf("missing balance_infos: status = %v, want parse", state.Status)
	}
}

func TestFetchNoKey(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	state := fetch(context.Background(), srv.Client(), srv.URL, "", testNow)
	if state.Status != meter.StatusNotConfigured {
		t.Fatalf("status = %v, want not configured", state.Status)
	}
	if requests != 0 {
		t.Fatalf("server saw %d requests, want none", requests)
	}
}

// TestFetchRedactsKey feeds the raw key back in an error body and message and
// asserts it never appears in the state message.
func TestFetchRedactsKey(t *testing.T) {
	srv, _ := serveBalance(t, http.StatusInternalServerError, `{"error":"invalid key `+testKey+`"}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testKey, testNow)
	if state.Status != meter.StatusHTTP {
		t.Fatalf("status = %v, want http", state.Status)
	}
	if state.Message == "" {
		t.Fatal("empty message for failed fetch")
	}
	if strings.Contains(state.Message, testKey) {
		t.Fatalf("message %q leaks key", state.Message)
	}
}
