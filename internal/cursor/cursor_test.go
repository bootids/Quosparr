package cursor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/sqliteruntime"
)

// testJWT builds a JWT-shaped token with the given subject and expiry.
func testJWT(sub string, exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := fmt.Sprintf(`{"sub":%q,"exp":%d}`, sub, exp)
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	return header + "." + payload + ".sig"
}

const testUser = "user|alice@example.com"

// writeStateDB creates a state.vscdb holding the access token.
func writeStateDB(t *testing.T, token string) string {
	t.Helper()
	if err := sqliteruntime.Load(); err != nil {
		t.Fatalf("load sqlite runtime: %v", err)
	}
	path := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sqlite.Open(context.Background(), path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Execute(context.Background(), `CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value BLOB)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Execute(context.Background(),
		`INSERT INTO ItemTable (key, value) VALUES ('cursorAuth/accessToken', ?)`, token); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	return path
}

func TestLoadCredential(t *testing.T) {
	path := writeStateDB(t, testJWT(testUser, 4102444800))
	cred, ok, err := LoadCredential(context.Background(), path)
	if err != nil || !ok {
		t.Fatalf("LoadCredential = (%v, %v, %v), want (cred, true, nil)", cred, ok, err)
	}
	if cred.Token == "" || cred.User != "alice@example.com" {
		t.Fatalf("credential = %+v, want user alice@example.com", cred)
	}
	if !cred.HasExpiry || cred.ExpiresAt.Unix() != 4102444800 {
		t.Fatalf("expiry = %v (%v)", cred.ExpiresAt, cred.HasExpiry)
	}
}

func TestLoadCredentialMissingDB(t *testing.T) {
	if err := sqliteruntime.Load(); err != nil {
		t.Fatalf("load sqlite runtime: %v", err)
	}
	_, ok, err := LoadCredential(context.Background(), filepath.Join(t.TempDir(), "absent.vscdb"))
	if ok || err != nil {
		t.Fatalf("missing db = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestLoadCredentialInvalidToken(t *testing.T) {
	path := writeStateDB(t, "not-a-jwt")
	_, ok, err := LoadCredential(context.Background(), path)
	if ok || !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("invalid token = (%v, %v), want (false, ErrInvalidToken)", ok, err)
	}
}

// recordingServer answers with body on the first request and records the
// Cookie header. No request leaves it unconsumed.
type recordingServer struct {
	srv    *httptest.Server
	cookie string
	calls  int
}

func newRecordingServer(status int, body string) *recordingServer {
	r := &recordingServer{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.calls++
		r.cookie = req.Header.Get("Cookie")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	usageSummaryURL = r.srv.URL + "/api/usage-summary"
	return r
}

func (r *recordingServer) client() *http.Client {
	return &http.Client{Transport: r.srv.Client().Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func TestFetchSendsEncodedCookie(t *testing.T) {
	rs := newRecordingServer(200, `{}`)
	defer rs.srv.Close()
	token := "tok/en+with=special~chars"
	cred := Credential{Token: token, User: "alice@example.com"}
	state := Fetch(context.Background(), rs.client(), cred, time.Now())
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %v (%s)", state.Status, state.Message)
	}
	want := "WorkosCursorSessionToken=" + meter.PercentEncode("alice@example.com::"+token)
	if rs.cookie != want {
		t.Fatalf("cookie = %q, want %q", rs.cookie, want)
	}
}

func fetchFixture(t *testing.T, name string) meter.State {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rs := newRecordingServer(200, string(body))
	defer rs.srv.Close()
	cred := Credential{Token: "secret-token", User: "alice@example.com"}
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	state := Fetch(context.Background(), rs.client(), cred, now)
	if state.Status != meter.StatusOK {
		t.Fatalf("status = %v (%s)", state.Status, state.Message)
	}
	if strings.Contains(state.Message, "secret-token") || strings.Contains(state.Message, "alice@example.com::secret-token") {
		t.Fatalf("message leaks credential: %q", state.Message)
	}
	return state
}

func limitByID(t *testing.T, s meter.State, id string) meter.Limit {
	t.Helper()
	for _, l := range s.Limits {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("no limit %q in %v", id, s.Limits)
	return meter.Limit{}
}

func TestFetchPersonalFixture(t *testing.T) {
	s := fetchFixture(t, "success-personal.json")
	if s.Plan != "pro" {
		t.Fatalf("plan = %q", s.Plan)
	}
	plan := limitByID(t, s, "plan")
	if plan.Used != 5 || plan.Remaining != 15 || plan.LimitValue != 20 {
		t.Fatalf("plan money = used %v remaining %v limit %v", plan.Used, plan.Remaining, plan.LimitValue)
	}
	if plan.Unit != "usd" || plan.WindowMinutes != meter.MonthlyMinutes || plan.Title != "Plan" {
		t.Fatalf("plan meta = %+v", plan)
	}
	if !plan.HasPercent || plan.Percent != 25 {
		t.Fatalf("plan percent = %v (%v)", plan.Percent, plan.HasPercent)
	}
	if !plan.HasReset || !plan.ResetsAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("plan reset = %v (%v)", plan.ResetsAt, plan.HasReset)
	}
	auto := limitByID(t, s, "plan_auto")
	if auto.Unit != "percent" || auto.Percent != 10 || !auto.HasPercent || meter.Has(auto.Used) {
		t.Fatalf("plan_auto = %+v", auto)
	}
	api := limitByID(t, s, "plan_api")
	if api.Percent != 15 || !api.HasPercent {
		t.Fatalf("plan_api = %+v", api)
	}
	onDemand := limitByID(t, s, "on_demand")
	if !onDemand.HasPercent || onDemand.Percent != 10 {
		t.Fatalf("on_demand percent = %v (%v)", onDemand.Percent, onDemand.HasPercent)
	}
}

func TestFetchTeamFixture(t *testing.T) {
	s := fetchFixture(t, "success-team.json")
	if s.Plan != "enterprise" {
		t.Fatalf("plan = %q", s.Plan)
	}
	overall := limitByID(t, s, "overall")
	if !overall.HasPercent || overall.Percent != 30 {
		t.Fatalf("overall percent = %v (%v)", overall.Percent, overall.HasPercent)
	}
	pooled := limitByID(t, s, "team_pooled")
	if pooled.LimitValue != 1000 {
		t.Fatalf("team_pooled limit = %v", pooled.LimitValue)
	}
}

func TestFetchUnauthorized(t *testing.T) {
	rs := newRecordingServer(401, `{}`)
	defer rs.srv.Close()
	cred := Credential{Token: "secret-token", User: "alice@example.com"}
	state := Fetch(context.Background(), rs.client(), cred, time.Now())
	if state.Status != meter.StatusAuth {
		t.Fatalf("status = %v", state.Status)
	}
	if strings.Contains(state.Message, "secret-token") {
		t.Fatalf("message leaks credential: %q", state.Message)
	}
}

func TestFetchExpiredTokenNoRequest(t *testing.T) {
	rs := newRecordingServer(200, `{}`)
	defer rs.srv.Close()
	cred := Credential{Token: "secret-token", User: "alice@example.com",
		ExpiresAt: time.Now().Add(-time.Hour), HasExpiry: true}
	state := Fetch(context.Background(), rs.client(), cred, time.Now())
	if state.Status != meter.StatusAuth {
		t.Fatalf("status = %v", state.Status)
	}
	if rs.calls != 0 {
		t.Fatalf("expired token made %d requests", rs.calls)
	}
	if strings.Contains(state.Message, "secret-token") {
		t.Fatalf("message leaks credential: %q", state.Message)
	}
}

func TestFetchMissingAndInvalidToken(t *testing.T) {
	if s := Fetch(context.Background(), nil, Credential{}, time.Now()); s.Status != meter.StatusNotConfigured {
		t.Fatalf("empty token status = %v", s.Status)
	}
	rs := newRecordingServer(200, `{}`)
	defer rs.srv.Close()
	if s := Fetch(context.Background(), rs.client(), Credential{Token: "t"}, time.Now()); s.Status != meter.StatusAuth {
		t.Fatalf("token without user status = %v", s.Status)
	}
	if rs.calls != 0 {
		t.Fatalf("invalid token made %d requests", rs.calls)
	}
}

func TestParseResetEpoch(t *testing.T) {
	raw, _ := json.Marshal(1793616000)
	ts, ok := parseReset(raw)
	if !ok || !ts.Equal(time.Unix(1793616000, 0).UTC()) {
		t.Fatalf("epoch reset = %v (%v)", ts, ok)
	}
}
