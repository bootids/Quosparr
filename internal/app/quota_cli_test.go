package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/export"
)

func TestReadQuotaMissingIsReadOnly(t *testing.T) {
	home := t.TempDir()
	var out, stderr bytes.Buffer
	if code := ReadQuota(&out, &stderr, home, nil, time.Now()); code != 1 || out.Len() != 0 ||
		!strings.Contains(stderr.String(), "enable Export status JSON") {
		t.Fatalf("code/output: %d, %q, %q", code, out.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".quosparr")); !os.IsNotExist(err) {
		t.Fatalf("query created state: %v", err)
	}
}

func TestReadQuotaFiltersAndRechecksAge(t *testing.T) {
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "status.json")
	st := export.StatusSnapshot{GeneratedAt: now, QuotaReport: &export.QuotaReport{
		SchemaVersion: 1, GeneratedAt: now, Providers: map[string]export.QuotaProvider{
			"codex": {Status: "ok", FetchedAt: &now}, "kimi": {Status: "not_configured"},
		},
	}}
	if err := (export.StatusExporter{Path: path}).Save(st); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"--status-file", path, "codex"}, 0},
		{[]string{"--status-file", path, "unsupported"}, 2},
		{[]string{"--max-age", "0"}, 2},
		{[]string{"--max-age", "9223372036854775807"}, 2},
		{[]string{"--refresh"}, 2},
	} {
		var out, stderr bytes.Buffer
		code := ReadQuota(&out, &stderr, t.TempDir(), tc.args, now.Add(16*time.Minute))
		if code != tc.code {
			t.Fatalf("%v: code %d, stderr %q", tc.args, code, stderr.String())
		}
		if code != 0 {
			if out.Len() != 0 {
				t.Fatalf("error has stdout: %q", out.String())
			}
			continue
		}
		var r export.QuotaReport
		if err := json.Unmarshal(out.Bytes(), &r); err != nil || len(r.Providers) != 1 ||
			r.Providers["codex"].Status != "stale" || *r.Providers["codex"].AgeSeconds != 960 {
			t.Fatalf("query = %s, %v", out.String(), err)
		}
	}
}

func TestReadQuotaRejectsOldOrMalformedExportWithoutEcho(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	for _, content := range []string{`{"quota":{}}`, `{"quotaReport":{"schema_version":99}}`, `SECRET malformed body`} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if code := ReadQuota(&out, &stderr, t.TempDir(), []string{"--status-file", path}, time.Now()); code != 1 || out.Len() != 0 || strings.Contains(stderr.String(), "SECRET") {
			t.Fatalf("code %d: %q / %q", code, out.String(), stderr.String())
		}
	}
}
