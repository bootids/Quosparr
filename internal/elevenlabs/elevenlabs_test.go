package elevenlabs

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

const testSecret = "elevenlabs-secret-key-1234"

var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// serveSubscription spins up a test server serving body/status and returning
// the received xi-api-key header.
func serveSubscription(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("xi-api-key")
		if r.URL.Path != "/v1/user/subscription" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &gotKey
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

func hasID(limits []meter.Limit, id string) bool {
	for _, l := range limits {
		if l.ID == id {
			return true
		}
	}
	return false
}

func TestFetchSubscription(t *testing.T) {
	srv, gotKey := serveSubscription(t, http.StatusOK, fixture(t, "subscription-success.json"))
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if *gotKey != testSecret {
		t.Fatalf("xi-api-key = %q, want the secret (never a Bearer header)", *gotKey)
	}
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s), want ok", state.Status, state.Message)
	}
	if state.Plan != "creator" {
		t.Fatalf("plan = %q, want creator", state.Plan)
	}
	if !state.FetchedAt.Equal(testNow) {
		t.Fatalf("FetchedAt = %v, want %v", state.FetchedAt, testNow)
	}
	characters := byID(t, state.Limits, "characters")
	if characters.Title != "Characters" || characters.Unit != "characters" || characters.WindowMinutes != 0 {
		t.Fatalf("characters row = %+v", characters)
	}
	if characters.Used != 23243 || characters.LimitValue != 131000 || characters.Remaining != 107757 {
		t.Fatalf("characters counts = used %v limit %v remaining %v, want 23243/131000/107757",
			characters.Used, characters.LimitValue, characters.Remaining)
	}
	if !characters.HasPercent || math.Abs(characters.Percent-23243.0/131000*100) > 1e-9 {
		t.Fatalf("characters percent = %v has %v", characters.Percent, characters.HasPercent)
	}
	if !characters.HasReset || !characters.ResetsAt.Equal(time.Date(2026, 11, 7, 9, 27, 44, 0, time.UTC)) {
		t.Fatalf("characters reset = %v has %v", characters.ResetsAt, characters.HasReset)
	}
	voiceSlots := byID(t, state.Limits, "voice_slots")
	if voiceSlots.Title != "Voice slots" || voiceSlots.Unit != "voices" {
		t.Fatalf("voice_slots row = %+v", voiceSlots)
	}
	if voiceSlots.Used != 4 || voiceSlots.LimitValue != 30 || voiceSlots.Remaining != 26 {
		t.Fatalf("voice_slots counts = %+v", voiceSlots)
	}
	if voiceSlots.HasReset || voiceSlots.WindowMinutes != 0 {
		t.Fatalf("voice_slots window/reset = %+v", voiceSlots)
	}
	prof := byID(t, state.Limits, "professional_voice_slots")
	if prof.Title != "Professional voices" || prof.Unit != "voices" {
		t.Fatalf("professional_voice_slots row = %+v", prof)
	}
	if prof.Used != 0 || prof.LimitValue != 1 || prof.Remaining != 1 {
		t.Fatalf("professional_voice_slots counts = %+v", prof)
	}
	edits := byID(t, state.Limits, "voice_add_edits")
	if edits.Title != "Voice edits" || edits.Unit != "edits" {
		t.Fatalf("voice_add_edits row = %+v", edits)
	}
	if edits.Used != 4 || edits.LimitValue != 95 || edits.Remaining != 91 {
		t.Fatalf("voice_add_edits counts = %+v", edits)
	}
}

// TestFetchSkipsAbsentStaticPools feeds a response whose static pool keys
// are all missing: only the characters row survives.
func TestFetchSkipsAbsentStaticPools(t *testing.T) {
	srv, _ := serveSubscription(t, http.StatusOK, `{"tier":"free","character_count":"10","character_limit":100,"next_character_count_reset_unix":1794043664000}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s), want ok", state.Status, state.Message)
	}
	if hasID(state.Limits, "voice_slots") || hasID(state.Limits, "professional_voice_slots") || hasID(state.Limits, "voice_add_edits") {
		t.Fatalf("absent static pools not skipped: %+v", state.Limits)
	}
	characters := byID(t, state.Limits, "characters")
	if characters.Used != 10 || characters.LimitValue != 100 {
		t.Fatalf("characters counts = %+v (numeric string must be accepted)", characters)
	}
	// Millisecond timestamps (>= 1e12) resolve to the same instant as seconds.
	if !characters.HasReset || !characters.ResetsAt.Equal(time.Date(2026, 11, 7, 9, 27, 44, 0, time.UTC)) {
		t.Fatalf("characters reset = %v has %v", characters.ResetsAt, characters.HasReset)
	}
}

func TestFetchUnauthorized(t *testing.T) {
	srv, _ := serveSubscription(t, http.StatusUnauthorized, `{"detail":{"status":"invalid_api_key"}}`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusAuth {
		t.Fatalf("status = %q (%s), want auth", state.Status, state.Message)
	}
	if strings.Contains(state.Message, testSecret) {
		t.Fatalf("message %q leaks the secret", state.Message)
	}
}

func TestFetchParseFailure(t *testing.T) {
	srv, _ := serveSubscription(t, http.StatusOK, `{not json`)
	state := fetch(context.Background(), srv.Client(), srv.URL, testSecret, testNow)
	if state.Status != meter.StatusParse {
		t.Fatalf("status = %q (%s), want parse", state.Status, state.Message)
	}
	if strings.Contains(state.Message, testSecret) {
		t.Fatalf("message %q leaks the secret", state.Message)
	}
}

func TestFetchNoSecret(t *testing.T) {
	state := fetch(context.Background(), http.DefaultClient, "https://api.elevenlabs.io", "", testNow)
	if state.Status != meter.StatusNotConfigured {
		t.Fatalf("status = %q, want not_configured", state.Status)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api-key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCredentialAPIKeyPriority(t *testing.T) {
	path := writeTemp(t, "  file-key  ")
	key, ok, err := LoadCredential(testSecret, path)
	if err != nil || !ok || key != testSecret {
		t.Fatalf("LoadCredential = (%q, %v, %v), want explicit key", key, ok, err)
	}
}

func TestLoadCredentialFileTrimsWhitespace(t *testing.T) {
	path := writeTemp(t, "\n  elevenlabs-file-key  \n")
	key, ok, err := LoadCredential("", path)
	if err != nil || !ok || key != "elevenlabs-file-key" {
		t.Fatalf("LoadCredential = (%q, %v, %v), want trimmed file key", key, ok, err)
	}
}

func TestLoadCredentialMissingFile(t *testing.T) {
	key, ok, err := LoadCredential("", filepath.Join(t.TempDir(), "absent"))
	if ok || err != nil || key != "" {
		t.Fatalf("LoadCredential = (%q, %v, %v), want not found", key, ok, err)
	}
}

func TestLoadCredentialEmptyFile(t *testing.T) {
	path := writeTemp(t, "   \n")
	key, ok, err := LoadCredential("", path)
	if ok || err != nil || key != "" {
		t.Fatalf("LoadCredential = (%q, %v, %v), want not found", key, ok, err)
	}
}

func TestAPIKeyFilePath(t *testing.T) {
	if got := APIKeyFilePath("/home/u"); got != filepath.Join("/home/u", ".config", "elevenlabs", "api-key") {
		t.Fatalf("APIKeyFilePath = %q", got)
	}
}
