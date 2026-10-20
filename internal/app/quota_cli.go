package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/licoy/quosparr/internal/export"
)

// ReadQuota runs before any desktop, preferences, account or network setup.
func ReadQuota(out, stderr io.Writer, home string, args []string, now time.Time) int {
	fs := flag.NewFlagSet("quosparr --quota", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("status-file", export.DefaultStatusPath(home), "")
	maxAge := fs.Int("max-age", 900, "")
	if err := fs.Parse(args); err != nil || *maxAge <= 0 || int64(*maxAge) > int64((1<<63-1)/time.Second) {
		fmt.Fprintln(stderr, "quota: use --quota [--status-file PATH] [--max-age SECONDS] [provider ...]; max-age must be positive")
		return 2
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stderr, "quota: status export missing; enable Export status JSON in Quosparr and wait for a refresh")
		} else {
			fmt.Fprintln(stderr, "quota: cannot read status export")
		}
		return 1
	}
	// Decode only the contract we need. Never emit the status file's sessions,
	// account labels or other panel data, even in error diagnostics.
	var st struct {
		QuotaReport *export.QuotaReport `json:"quotaReport"`
	}
	if json.Unmarshal(data, &st) != nil || st.QuotaReport == nil ||
		st.QuotaReport.SchemaVersion != export.QuotaSchemaVersion ||
		st.QuotaReport.GeneratedAt.IsZero() || st.QuotaReport.Providers == nil {
		fmt.Fprintln(stderr, "quota: invalid or unsupported quota report; update Quosparr and refresh its status export")
		return 1
	}
	r := st.QuotaReport.At(now, time.Duration(*maxAge)*time.Second)
	if names := fs.Args(); len(names) > 0 {
		selected := make(map[string]export.QuotaProvider, len(names))
		for _, name := range names {
			p, ok := r.Providers[name]
			if !ok {
				fmt.Fprintln(stderr, "quota: unknown provider (supported: codex grok claude cursor kimi zai agy deepseek minimax pixellab elevenlabs)")
				return 2
			}
			selected[name] = p
		}
		r.Providers = selected
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		fmt.Fprintln(stderr, "quota: cannot encode report")
		return 1
	}
	return 0
}
