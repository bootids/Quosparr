package meter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetJSONSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("accept") != "application/json" {
			t.Errorf("accept = %q", r.Header.Get("accept"))
		}
		if r.Header.Get("user-agent") == "" {
			t.Error("user agent missing")
		}
		if got := r.Header.Get("x-test"); got != "v" {
			t.Errorf("x-test = %q", got)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	body, code, err := GetJSON(context.Background(), srv.Client(), srv.URL, "Quosparr-test", map[string]string{"x-test": "v"})
	if err != nil || code != 200 || string(body) != `{"ok":true}` {
		t.Fatalf("get = %q %d %v", body, code, err)
	}
}

func TestGetJSONErrorStatuses(t *testing.T) {
	for _, tc := range []struct {
		code int
		want Status
	}{
		{401, StatusAuth},
		{403, StatusAuth},
		{429, StatusRateLimited},
		{500, StatusHTTP},
		{302, StatusHTTP},
		{404, StatusHTTP},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.code)
		}))
		_, _, err := GetJSON(context.Background(), NoRedirectClient(srv.Client()), srv.URL, "Quosparr-test", nil)
		srv.Close()
		var fe *FetchError
		if !errors.As(err, &fe) || fe.Status != tc.want || fe.HTTP != tc.code {
			t.Errorf("code %d: err = %v want %s", tc.code, err, tc.want)
		}
		if fe != nil && strings.Contains(fe.Msg, "body") {
			t.Errorf("code %d: message leaks body wording: %q", tc.code, fe.Msg)
		}
	}
}

func TestGetJSONRedirectNotFollowed(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect was followed")
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()
	_, code, err := GetJSON(context.Background(), NoRedirectClient(srv.Client()), srv.URL, "Quosparr-test", nil)
	var fe *FetchError
	if code != 302 || !errors.As(err, &fe) || fe.Status != StatusHTTP {
		t.Fatalf("redirect: code %d err %v", code, err)
	}
}

func TestGetJSONTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := GetJSON(ctx, srv.Client(), srv.URL, "Quosparr-test", nil)
	var fe *FetchError
	if !errors.As(err, &fe) || fe.Status != StatusTimeout {
		t.Fatalf("timeout err = %v", err)
	}
}

func TestGetJSONOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, maxBodyBytes+2))
	}))
	defer srv.Close()
	_, _, err := GetJSON(context.Background(), srv.Client(), srv.URL, "Quosparr-test", nil)
	var fe *FetchError
	if !errors.As(err, &fe) || fe.Status != StatusParse {
		t.Fatalf("oversize err = %v", err)
	}
}

func TestPercentEncode(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"abcXYZ09-_.~", "abcXYZ09-_.~"},
		{"user@example.com", "user%40example.com"},
		{"a b/c+d", "a%20b%2Fc%2Bd"},
		{"用户", "%E7%94%A8%E6%88%B7"},
	} {
		if got := PercentEncode(tc.in); got != tc.want {
			t.Errorf("PercentEncode(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestRedactMasksAllForms(t *testing.T) {
	secret := "sk+abcdefghijklmnop"
	msg := "key sk+abcdefghijklmnop and sk%2Babcdefghijklmnop and tail"
	got := Redact(msg, secret)
	if strings.Contains(got, secret) || strings.Contains(got, "sk%2B") {
		t.Fatalf("leak: %q", got)
	}
	if !strings.Contains(got, "sk+a…") {
		t.Fatalf("mask missing: %q", got)
	}
}

func TestRedactLongestFirstAndShort(t *testing.T) {
	combined := "user%3A%3Ask-test-secret"
	got := Redact("x "+combined+" y", "user::sk-test-secret", "sk-test-secret")
	if strings.Contains(got, "sk-test-secret") {
		t.Fatalf("combined form leaked: %q", got)
	}
	if got := Redact("abc inside", "abc"); strings.Contains(got, "abc") {
		t.Fatalf("short secret leaked: %q", got)
	}
}

func TestKeyHintStableAndPrefixOnly(t *testing.T) {
	a, b := KeyHint("sk-verysecretkey"), KeyHint("sk-verysecretkey")
	if a != b || !strings.HasPrefix(a, "sk-v-") {
		t.Fatalf("hint = %q / %q", a, b)
	}
	if strings.Contains(a, "verysecretkey"[4:]) {
		t.Fatalf("hint leaks key: %q", a)
	}
	if KeyHint("") != "" {
		t.Fatal("empty key hint not empty")
	}
}

func TestFloatHelpers(t *testing.T) {
	if !Has(Float(1.5, true)) || Has(NaN()) || Has(Float(0, false)) {
		t.Fatal("float presence helpers disagree")
	}
}
