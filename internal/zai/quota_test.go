package zai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(data)
}

// rewriter forwards every request to the test server while keeping the
// original URL (host included) for assertions.
type rewriter struct {
	server *httptest.Server
	urls   []string
	auths  []string
}

func (r *rewriter) Do(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL.String())
	r.auths = append(r.auths, req.Header.Get("Authorization"))
	target := *req.URL
	target.Scheme, target.Host = "http", strings.TrimPrefix(r.server.URL, "http://")
	forwarded, err := http.NewRequestWithContext(req.Context(), req.Method, target.String(), nil)
	if err != nil {
		return nil, err
	}
	forwarded.Header = req.Header
	return r.server.Client().Do(forwarded)
}

// newZaiServer serves the quota and model-usage fixtures and records the
// requests seen.
func newZaiServer(t *testing.T, quotaBody, quotaStatus, usageBody, usageStatus string) (*httptest.Server, *rewriter) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/monitor/usage/quota/limit":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(parseStatus(quotaStatus))
			fmt.Fprint(w, quotaBody)
		case "/api/monitor/usage/model-usage":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(parseStatus(usageStatus))
			fmt.Fprint(w, usageBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &rewriter{server: srv}
}

func parseStatus(s string) int {
	if s == "" {
		return http.StatusOK
	}
	var code int
	fmt.Sscanf(s, "%d", &code)
	return code
}

func findLimit(t *testing.T, st meter.State, id string) meter.Limit {
	t.Helper()
	for _, l := range st.Limits {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("limit %q not found in %v", id, st.Limits)
	return meter.Limit{}
}

func TestFetchTokensAndTools(t *testing.T) {
	_, rw := newZaiServer(t, fixture(t, "success-tokens-and-tools.json"), "", "", "")
	st := Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if st.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s), want ok", st.Status, st.Message)
	}
	if st.Plan != "pro" {
		t.Fatalf("plan = %q, want pro", st.Plan)
	}
	if len(st.Limits) != 2 {
		t.Fatalf("limits = %d, want 2 (monthly dropped)", len(st.Limits))
	}
	for _, l := range st.Limits {
		if l.ID == "tools_monthly" {
			t.Fatalf("tools_monthly survived the filter: %+v", st.Limits)
		}
	}
	h5 := findLimit(t, st, "tokens_5h")
	if h5.WindowMinutes != meter.FiveHourMinutes || h5.Percent != 82 || !h5.HasPercent {
		t.Fatalf("tokens_5h = %+v", h5)
	}
	if meter.Has(h5.Used) || meter.Has(h5.LimitValue) || meter.Has(h5.Remaining) {
	}
	if h5.ResetsAt.UnixMilli() != 1782656863894 {
		t.Fatalf("tokens_5h reset = %v", h5.ResetsAt)
	}
	d7 := findLimit(t, st, "tokens_7d")
	if d7.WindowMinutes != meter.WeeklyMinutes || d7.Percent != 38 {
		t.Fatalf("tokens_7d = %+v", d7)
	}
	if st.Usage != nil {
		t.Fatalf("usage should be nil when model-usage is empty, got %+v", st.Usage)
	}
}

func TestFetchCreditsKeepsMismatch(t *testing.T) {
	_, rw := newZaiServer(t, fixture(t, "success-credits.json"), "", "", "")
	st := Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if st.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s)", st.Status, st.Message)
	}
	c5 := findLimit(t, st, "credits_5h")
	if c5.Used != 1438 || c5.LimitValue != 12000 || c5.Remaining != 10561 {
		t.Fatalf("credits_5h = %+v", c5)
	}
	if c5.Remaining == c5.LimitValue-c5.Used {
		t.Fatal("remaining was repaired; mismatch must be preserved verbatim")
	}
	if c5.Percent != 11 || !c5.HasPercent {
		t.Fatalf("credits_5h percent = %+v", c5)
	}
	d7 := findLimit(t, st, "credits_7d")
	if d7.Percent != 3 || d7.Used != 2254 || d7.LimitValue != 60000 || d7.Remaining != 57745 {
		t.Fatalf("credits_7d = %+v", d7)
	}
}

func TestFetchBusinessFailureIsAPI(t *testing.T) {
	_, rw := newZaiServer(t, fixture(t, "no-coding-plan.json"), "200", "", "")
	st := Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if st.Status != meter.StatusAPI {
		t.Fatalf("status = %q, want api", st.Status)
	}
	if st.Message == "" || st.Limits != nil {
		t.Fatalf("state = %+v", st)
	}
}

// TestFetchKeepsOnly5hAnd7dWindows feeds every pool family plus monthly and
// expects only the *_5h/*_7d rows to survive.
func TestFetchKeepsOnly5hAnd7dWindows(t *testing.T) {
	body := `{
		"success": true,
		"data": {
			"level": "pro",
			"limits": [
				{"type": "TOKENS_LIMIT", "unit": 3, "number": 5, "percentage": 82},
				{"type": "TOKENS_LIMIT", "unit": 6, "number": 7, "percentage": 38},
				{"type": "CREDIT_LIMIT", "unit": 3, "number": 5, "usage": 12000, "currentValue": 1438, "remaining": 10561, "percentage": 11},
				{"type": "CREDIT_LIMIT", "unit": 6, "number": 1, "usage": 60000, "currentValue": 2254, "remaining": 57745, "percentage": 3},
				{"type": "TIME_LIMIT", "unit": 5, "number": 1, "usage": 100, "currentValue": 0, "remaining": 100},
				{"type": "TOKENS_LIMIT", "unit": 4, "number": 30, "percentage": 5}
			]
		}
	}`
	_, rw := newZaiServer(t, body, "", "", "")
	st := Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if st.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s), want ok", st.Status, st.Message)
	}
	want := map[string]bool{"tokens_5h": false, "tokens_7d": false, "credits_5h": false, "credits_7d": false}
	if len(st.Limits) != len(want) {
		t.Fatalf("limits = %+v, want exactly %d pools", st.Limits, len(want))
	}
	for _, l := range st.Limits {
		seen, ok := want[l.ID]
		if !ok || seen {
			t.Fatalf("unexpected or duplicate pool %q in %+v", l.ID, st.Limits)
		}
		want[l.ID] = true
		if l.WindowMinutes != meter.FiveHourMinutes && l.WindowMinutes != meter.WeeklyMinutes {
			t.Fatalf("pool %q window %d survived the filter", l.ID, l.WindowMinutes)
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("pool %q missing from %+v", id, st.Limits)
		}
	}
}

// TestFetchSingleWindow expects a lone 5h pool to pass through alone.
func TestFetchSingleWindow(t *testing.T) {
	body := `{
		"success": true,
		"data": {
			"limits": [
				{"type": "TOKENS_LIMIT", "unit": 3, "number": 5, "percentage": 42}
			]
		}
	}`
	_, rw := newZaiServer(t, body, "", "", "")
	st := Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if st.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s), want ok", st.Status, st.Message)
	}
	if len(st.Limits) != 1 || st.Limits[0].ID != "tokens_5h" || st.Limits[0].WindowMinutes != meter.FiveHourMinutes {
		t.Fatalf("limits = %+v, want single tokens_5h pool", st.Limits)
	}
}

func TestFetchBusiness401IsAuth(t *testing.T) {
	body := `{"code":401,"msg":"invalid key sk-test-key-1234","success":false}`
	_, rw := newZaiServer(t, body, "200", "", "")
	st := Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if st.Status != meter.StatusAuth {
		t.Fatalf("status = %q, want auth", st.Status)
	}
	if strings.Contains(st.Message, "sk-test-key-1234") {
		t.Fatalf("message leaks key: %q", st.Message)
	}
}

func TestFetchHTTP401IsAuth(t *testing.T) {
	_, rw := newZaiServer(t, `{"success":true}`, "401", "", "")
	st := Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if st.Status != meter.StatusAuth {
		t.Fatalf("status = %q, want auth", st.Status)
	}
	if strings.Contains(st.Message, "sk-test-key-1234") {
		t.Fatalf("message leaks key: %q", st.Message)
	}
}

func TestFetchMissingLimitsIsParse(t *testing.T) {
	_, rw := newZaiServer(t, `{"success":true,"data":{"level":"pro"}}`, "", "", "")
	st := Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if st.Status != meter.StatusParse {
		t.Fatalf("status = %q, want parse", st.Status)
	}
}

func TestFetchModelUsage(t *testing.T) {
	_, rw := newZaiServer(t,
		fixture(t, "success-tokens-and-tools.json"), "",
		fixture(t, "success-model-usage.json"), "")
	st := Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if st.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s)", st.Status, st.Message)
	}
	u := st.Usage
	if u == nil {
		t.Fatal("usage is nil")
	}
	if u.Granularity != "hourly" {
		t.Fatalf("granularity = %q", u.Granularity)
	}
	if u.TotalCalls != 6856 || u.TotalTokens != 688408686 {
		t.Fatalf("totals = %v/%v", u.TotalCalls, u.TotalTokens)
	}
	if len(u.Models) != 2 {
		t.Fatalf("models = %+v", u.Models)
	}
	if u.Models[0].Name != "GLM-5.3" || u.Models[0].Tokens != 826662212 {
		t.Fatalf("models[0] = %+v", u.Models[0])
	}
	if u.Models[1].Name != "GLM-5.3-Flash" || u.Models[1].Tokens != 58233867 {
		t.Fatalf("models[1] = %+v", u.Models[1])
	}
	if len(u.Features) != 3 {
		t.Fatalf("features = %+v", u.Features)
	}
	if u.Features[0].ID != "search-prime" || u.Features[2].ID != "zread" {
		t.Fatalf("features = %+v", u.Features)
	}
	wantStart := testNow.UTC().Add(-7 * 24 * time.Hour)
	wantEnd := testNow.UTC()
	if !u.Start.Equal(wantStart) || !u.End.Equal(wantEnd) {
		t.Fatalf("window = %v..%v", u.Start, u.End)
	}
}

func TestFetchModelUsageFailureIgnored(t *testing.T) {
	_, rw := newZaiServer(t,
		fixture(t, "success-tokens-and-tools.json"), "",
		`{"success":false}`, "500")
	st := Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if st.Status != meter.StatusOK {
		t.Fatalf("status = %q (%s)", st.Status, st.Message)
	}
	if st.Usage != nil {
		t.Fatalf("usage = %+v, want nil", st.Usage)
	}
	if len(st.Limits) != 2 {
		t.Fatalf("limits = %d", len(st.Limits))
	}
}

func TestFetchHeadersAndStamps(t *testing.T) {
	_, rw := newZaiServer(t, fixture(t, "success-tokens-and-tools.json"), "", "", "")
	Fetch(context.Background(), rw, "sk-test-key-1234", "intl", "raw", testNow)
	if len(rw.urls) != 2 || len(rw.auths) != 2 {
		t.Fatalf("requests = %v", rw.urls)
	}
	if rw.auths[0] != "sk-test-key-1234" {
		t.Fatalf("raw authorization = %q", rw.auths[0])
	}
	want := "https://api.z.ai/api/monitor/usage/model-usage" +
		"?startTime=2026-10-11%2B12%3A00%3A00" +
		"&endTime=2026-10-18%2B12%3A00%3A00"
	if rw.urls[1] != want {
		t.Fatalf("model-usage url = %q, want %q", rw.urls[1], want)
	}
}

func TestFetchBearerSchemeAndRegion(t *testing.T) {
	_, rw := newZaiServer(t, fixture(t, "success-credits.json"), "", "", "")
	Fetch(context.Background(), rw, "sk-cn-key-5678", "cn", "bearer", testNow)
	if len(rw.auths) == 0 || rw.auths[0] != "Bearer sk-cn-key-5678" {
		t.Fatalf("bearer authorization = %v", rw.auths)
	}
	if !strings.HasPrefix(rw.urls[0], "https://open.bigmodel.cn/") {
		t.Fatalf("cn base = %q", rw.urls[0])
	}
}

func TestEnvKey(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("Z_AI_API_KEY", "")
	if EnvKey() != "" {
		t.Fatal("EnvKey should be empty")
	}
	t.Setenv("Z_AI_API_KEY", "fallback-key")
	if EnvKey() != "fallback-key" {
		t.Fatalf("EnvKey = %q", EnvKey())
	}
	t.Setenv("ZAI_API_KEY", "primary-key")
	if EnvKey() != "primary-key" {
		t.Fatalf("EnvKey = %q", EnvKey())
	}
}
