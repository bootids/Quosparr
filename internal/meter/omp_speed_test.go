package meter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/sqliteruntime"
)

// ompMessagesSchema mirrors the messages table of omp's stats.db for the
// columns the scan reads.
const ompMessagesSchema = `CREATE TABLE messages (
	id INTEGER PRIMARY KEY,
	session_file TEXT NOT NULL,
	entry_id TEXT NOT NULL,
	folder TEXT,
	model TEXT,
	provider TEXT,
	api TEXT,
	timestamp INTEGER,
	duration INTEGER,
	ttft INTEGER,
	stop_reason TEXT,
	error_message TEXT,
	input_tokens INTEGER,
	output_tokens INTEGER,
	cache_read_tokens INTEGER,
	cache_write_tokens INTEGER,
	total_tokens INTEGER,
	premium_requests INTEGER,
	agent_type TEXT,
	UNIQUE (session_file, entry_id)
)`

// ompFixtureRow is one messages row; a nil measurement column is SQL NULL.
type ompFixtureRow struct {
	file, model, provider string
	at                    time.Time
	duration, ttft, out   any
}

// writeOMPStatsDB builds a stats.db fixture holding rows and returns its
// path. The scan never touches the real ~/.omp.
func writeOMPStatsDB(t *testing.T, rows ...ompFixtureRow) string {
	t.Helper()
	if err := sqliteruntime.Load(); err != nil {
		t.Fatalf("load sqlite runtime: %v", err)
	}
	path := filepath.Join(t.TempDir(), "stats.db")
	db, err := sqlite.Open(context.Background(), path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.Execute(ctx, ompMessagesSchema); err != nil {
		t.Fatalf("create messages: %v", err)
	}
	for i, r := range rows {
		if _, err := db.Execute(ctx, `INSERT INTO messages
			(session_file, entry_id, model, provider, timestamp, duration, ttft, output_tokens)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r.file, fmt.Sprintf("entry-%d", i+1), r.model, r.provider,
			r.at.UnixMilli(), r.duration, r.ttft, r.out); err != nil {
			t.Fatalf("insert row %d: %v", i+1, err)
		}
	}
	return path
}

func TestScanOMPSpeed(t *testing.T) {
	now := time.UnixMilli(1900000000000)
	path := writeOMPStatsDB(t,
		// kimi session: full sample, ttft-only sample, and a row whose model
		// is empty.
		ompFixtureRow{file: "/home/u/.omp/sessions/kimi-a.jsonl", model: "kimi-k2", provider: "kimi-code",
			at: now.Add(-2 * time.Hour), duration: int64(2000), ttft: int64(800), out: int64(300)},
		ompFixtureRow{file: "/home/u/.omp/sessions/kimi-a.jsonl", model: "kimi-k2", provider: "kimi-code",
			at: now.Add(-90 * time.Minute), duration: nil, ttft: int64(900), out: int64(500)},
		ompFixtureRow{file: "/home/u/.omp/sessions/kimi-a.jsonl", model: "", provider: "kimi-code",
			at: now.Add(-80 * time.Minute), duration: int64(1500), ttft: int64(700), out: int64(40)},
		ompFixtureRow{file: "/home/u/.omp/sessions/kimi-a.jsonl", model: "kimi-k2", provider: "kimi-code",
			at: now.Add(-3 * time.Hour), duration: int64(1000), ttft: int64(500), out: int64(100)},
		// zai session: output-only sample and a row with no measurement.
		ompFixtureRow{file: "/home/u/.omp/sessions/zai-b.jsonl", model: "glm-4.7", provider: "zai",
			at: now.Add(-5 * time.Hour), duration: int64(4000), ttft: nil, out: int64(900)},
		ompFixtureRow{file: "/home/u/.omp/sessions/zai-b.jsonl", model: "glm-4.7", provider: "zai",
			at: now.Add(-4 * time.Hour), duration: nil, ttft: nil, out: int64(123)},
		// Another provider and a row outside the window: both must stay out.
		ompFixtureRow{file: "cur-c.jsonl", model: "cursor-1", provider: "cursor",
			at: now.Add(-time.Hour), duration: int64(800), ttft: int64(400), out: int64(60)},
		ompFixtureRow{file: "/home/u/.omp/sessions/kimi-a.jsonl", model: "kimi-k2", provider: "kimi-code",
			at: now.Add(-20 * 24 * time.Hour), duration: int64(1000), ttft: int64(500), out: int64(999)},
	)
	report, err := ScanOMPSpeed(context.Background(), path, []string{"kimi-code", "zai"}, now)
	if err != nil {
		t.Fatalf("ScanOMPSpeed: %v", err)
	}
	if report.Empty() {
		t.Fatalf("report is empty, want kimi and zai groups")
	}
	if len(report.Groups) != 2 {
		t.Fatalf("groups = %v, want 2 (kimi-k2, glm-4.7)", report.Groups)
	}
	kimi, glm := report.Groups[0], report.Groups[1]
	if kimi.Model != "kimi-k2" || glm.Model != "glm-4.7" {
		t.Fatalf("group order = %s, %s, want kimi-k2 first (most samples)", kimi.Model, glm.Model)
	}
	if kimi.Calls != 3 || kimi.TTFTP50 != 800*time.Millisecond || kimi.TTFTP90 != 900*time.Millisecond {
		t.Fatalf("kimi ttft = %d calls, p50 %v, p90 %v, want 3, 800ms, 900ms", kimi.Calls, kimi.TTFTP50, kimi.TTFTP90)
	}
	if kimi.OutputTokens != 400 || kimi.Decode != 3*time.Second || !kimi.HasTPS() {
		t.Fatalf("kimi output = %d tokens over %v, want 400 over 3s", kimi.OutputTokens, kimi.Decode)
	}
	if glm.Calls != 0 || glm.TTFTP50 != 0 || glm.OutputTokens != 900 || glm.Decode != 4*time.Second {
		t.Fatalf("glm stat = %+v, want 900 tokens over 4s and no ttft", glm)
	}
	for _, g := range report.Groups {
		if g.Model == "cursor-1" {
			t.Fatalf("group %s leaked in, want the provider filter to exclude cursor", g.Model)
		}
	}
	if report.Tiered || kimi.Tier != speed.TierUnknown {
		t.Fatalf("tiered = %v, kimi tier = %q, want no tiers for omp", report.Tiered, kimi.Tier)
	}
	if len(report.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want one per session_file", report.Sessions)
	}
	if report.Sessions[0].ID != "kimi-a.jsonl" || report.Sessions[1].ID != "zai-b.jsonl" {
		t.Fatalf("session ids = %s, %s, want file bases newest first", report.Sessions[0].ID, report.Sessions[1].ID)
	}
	if !report.Sessions[0].UpdatedAt.Equal(now.Add(-90 * time.Minute)) {
		t.Fatalf("newest session updated at %v, want its newest sample", report.Sessions[0].UpdatedAt)
	}
	if !report.From.Equal(now.Add(-5*time.Hour)) || !report.To.Equal(now.Add(-90*time.Minute)) {
		t.Fatalf("from/to = %v..%v, want %v..%v", report.From, report.To,
			now.Add(-5*time.Hour), now.Add(-90*time.Minute))
	}
}

func TestScanOMPSpeedSessionLimit(t *testing.T) {
	now := time.UnixMilli(1900000000000)
	rows := make([]ompFixtureRow, speed.SessionLimit+2)
	for i := range rows {
		rows[i] = ompFixtureRow{
			file: fmt.Sprintf("omp-%02d.jsonl", i), model: "kimi-k2", provider: "kimi-code",
			at:       now.Add(-time.Duration(i+1) * time.Hour),
			duration: int64(1000), ttft: int64(500), out: int64(10),
		}
	}
	report, err := ScanOMPSpeed(context.Background(), writeOMPStatsDB(t, rows...), []string{"kimi-code"}, now)
	if err != nil {
		t.Fatalf("ScanOMPSpeed: %v", err)
	}
	if len(report.Sessions) != speed.SessionLimit {
		t.Fatalf("sessions = %d, want the newest %d", len(report.Sessions), speed.SessionLimit)
	}
	for i, want := range []string{"omp-00.jsonl", "omp-09.jsonl"} {
		if got := report.Sessions[i*(speed.SessionLimit-1)].ID; got != want {
			t.Fatalf("session %d = %s, want %s", i*(speed.SessionLimit-1), got, want)
		}
	}
	if report.Groups[0].Calls != speed.SessionLimit {
		t.Fatalf("calls = %d, want %d (only the kept sessions count)", report.Groups[0].Calls, speed.SessionLimit)
	}
}

func TestScanOMPSpeedMissingDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.db")
	report, err := ScanOMPSpeed(context.Background(), path, []string{"kimi-code"}, time.UnixMilli(1900000000000))
	if err != nil {
		t.Fatalf("ScanOMPSpeed missing db: %v, want nil", err)
	}
	if !report.Empty() {
		t.Fatalf("report = %+v, want the zero report", report)
	}
}

func TestScanOMPSpeedNoProviders(t *testing.T) {
	path := writeOMPStatsDB(t, ompFixtureRow{file: "s.jsonl", model: "kimi-k2", provider: "kimi-code",
		at: time.Now().Add(-time.Minute), duration: int64(1000), ttft: int64(500), out: int64(10)})
	report, err := ScanOMPSpeed(context.Background(), path, nil, time.Now())
	if err != nil || !report.Empty() {
		t.Fatalf("ScanOMPSpeed without providers = (%+v, %v), want zero report, nil error", report, err)
	}
}

func TestScanOMPSpeedMalformedDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.db")
	if err := os.WriteFile(path, []byte("definitely not a sqlite database, no row content here"), 0o644); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	report, err := ScanOMPSpeed(context.Background(), path, []string{"kimi-code"}, time.UnixMilli(1900000000000))
	if err == nil {
		t.Fatalf("ScanOMPSpeed malformed db = (%+v, nil), want error", report)
	}
	if !strings.Contains(err.Error(), "stats.db") {
		t.Fatalf("error %q, want it to name the database path", err)
	}
}
