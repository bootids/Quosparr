// Package agy reads the usage quota of the Antigravity CLI by running its
// read-only `agy -p /usage --output-format json` command in a subprocess.
//
// The package is pure business logic: no UI, no localization, no writes and
// no global state. Discovery only checks that the binary and the profile
// directory exist; it never reads file contents. Success is judged by the
// JSON shape of stdout (command.data.groups must be an array), which wins
// over the process exit code — the reference integration suite in
// ../aimeter/tests/integration.rs (line 668) accepts a fake agy exiting 7
// while printing the valid usage fixture.
package agy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

// Discovery is the located Antigravity CLI: the executable to run and the
// profile directory whose presence gates configuration.
type Discovery struct {
	Bin        string
	ProfileDir string
}

// Detect finds the Antigravity CLI binary and profile directory around
// home, defaulting the profile directory to <home>/.gemini/antigravity-cli.
// It is DetectIn without a profile directory override.
func Detect(binOverride, home string) (Discovery, bool) {
	return DetectIn(binOverride, "", home)
}

// DetectIn finds the Antigravity CLI binary and profile directory around
// home. binOverride is used as-is when it points to an existing executable
// file; otherwise the binary is looked up on PATH and finally at
// <home>/.local/bin/agy. A non-empty profileDirOverride replaces the
// default profile directory <home>/.gemini/antigravity-cli; either way the
// directory must exist. ok is false when either side is missing. DetectIn
// never reads file contents.
func DetectIn(binOverride, profileDirOverride, home string) (Discovery, bool) {
	bin := ""
	if binOverride != "" && isExecutable(binOverride) {
		bin = binOverride
	}
	if bin == "" {
		if p, err := exec.LookPath("agy"); err == nil {
			bin = p
		}
	}
	if bin == "" {
		name := "agy"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		p := filepath.Join(home, ".local", "bin", name)
		if isExecutable(p) {
			bin = p
		}
	}
	profile := profileDirOverride
	if profile == "" {
		profile = filepath.Join(home, ".gemini", "antigravity-cli")
	}
	if fi, err := os.Stat(profile); err != nil || !fi.IsDir() {
		return Discovery{}, false
	}
	if bin == "" {
		return Discovery{}, false
	}
	return Discovery{Bin: bin, ProfileDir: profile}, true
}

// isExecutable reports whether path is an existing regular file that the
// owner may execute; on windows existence is enough.
func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return fi.Mode().Perm()&0o100 != 0
}

// maxOutput caps each captured stream.
const maxOutput = 1 << 20

// Fetch runs the read-only usage command and normalizes its JSON output.
// Failures are reported as meter.Failed with a scrubbed message that never
// contains newlines, control characters or more than 200 runes of output.
func Fetch(ctx context.Context, bin string, now time.Time) meter.State {
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return meter.Failed(meter.StatusSubprocess, "agy: open devnull: "+scrub(err.Error()))
	}
	defer devnull.Close()

	cmd := exec.CommandContext(ctx, bin, "-p", "/usage", "--output-format", "json")
	cmd.Stdin = devnull
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return meter.Failed(meter.StatusSubprocess, scrub(err.Error()))
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return meter.Failed(meter.StatusSubprocess, scrub(err.Error()))
	}
	if err := cmd.Start(); err != nil {
		// Only the caller-given bin path can leak here, which is allowed.
		return meter.Failed(meter.StatusSubprocess, scrub(err.Error()))
	}
	stdout, _ := io.ReadAll(io.LimitReader(stdoutPipe, maxOutput))
	stderr, _ := io.ReadAll(io.LimitReader(stderrPipe, maxOutput))
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return meter.Failed(meter.StatusTimeout, "agy usage timed out")
		}
		return meter.Failed(meter.StatusSubprocess, scrub(err.Error()))
	}

	var doc usageDoc
	shapeOK := false
	if json.Unmarshal(stdout, &doc) == nil && len(doc.Command.Data.GroupsRaw) > 0 {
		var groups []group
		if json.Unmarshal(doc.Command.Data.GroupsRaw, &groups) == nil {
			shapeOK = true
			doc.groups = groups
		}
	}
	if !shapeOK {
		exit := exitCode(waitErr)
		return meter.Failed(meter.StatusSubprocess,
			fmt.Sprintf("agy exit %d; stdout: %s; stderr: %s", exit, scrub(string(stdout)), scrub(string(stderr))))
	}
	return meter.State{Status: meter.StatusOK, Limits: limits(doc.groups), FetchedAt: now}
}

// exitCode extracts the process exit code; 0 when waitErr is not an
// ExitError (failed start, signal) or is nil.
func exitCode(waitErr error) int {
	var ee *exec.ExitError
	if waitErr != nil && errors.As(waitErr, &ee) {
		return ee.ExitCode()
	}
	return 0
}

// usageDoc mirrors only the fields the shape check and normalization need;
// response, usage and descriptions are ignored.
type usageDoc struct {
	Command struct {
		Data struct {
			GroupsRaw json.RawMessage `json:"groups"`
		} `json:"data"`
	} `json:"command"`

	groups []group // decoded when GroupsRaw is a JSON array
}

type group struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Buckets     []bucket `json:"buckets"`
}

type bucket struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Window            string          `json:"window"`
	RemainingFraction json.RawMessage `json:"remaining_fraction"`
	ResetTime         json.RawMessage `json:"reset_time"`
}

// limits normalizes every bucket of every group into one meter.Limit row.
func limits(groups []group) []meter.Limit {
	var out []meter.Limit
	for _, g := range groups {
		for _, b := range g.Buckets {
			window := b.Window
			if window == "" {
				window = "unknown"
			}
			id := b.ID
			if id == "" {
				id = slug(g.Name) + ":" + window
			}
			l := meter.Limit{
				ID:            id,
				Title:         b.Name,
				Unit:          "percent",
				WindowMinutes: windowMinutes(window),
				Used:          meter.NaN(),
				LimitValue:    meter.NaN(),
				Remaining:     meter.NaN(),
				Percent:       meter.NaN(),
				Group:         g.Name,
			}
			if l.Title == "" {
				l.Title = id
			}
			if g.Description != "" {
				l.GroupDescription = g.Description
			}
			if f, ok := parseNumber(b.RemainingFraction); ok {
				l.Percent = (1 - f) * 100
				l.HasPercent = true
			}
			if t, ok := parseTime(b.ResetTime); ok {
				l.ResetsAt = t.UTC()
				l.HasReset = true
			}
			if l.HasPercent && l.Percent <= 0 {
				// A full pool shows no countdown in the official panel.
				l.HasReset = false
				l.ResetsAt = time.Time{}
			}
			out = append(out, l)
		}
	}
	return out
}

// windowMinutes maps a raw window label to shared window lengths; 0 means no
// duration.
func windowMinutes(window string) int {
	switch window {
	case "5h":
		return meter.FiveHourMinutes
	case "weekly", "7d":
		return meter.WeeklyMinutes
	default:
		return 0
	}
}

// slug lowercases s and joins its ASCII alphanumeric runs with hyphens.
func slug(s string) string {
	var b strings.Builder
	skip := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			skip = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
			skip = false
		default:
			if !skip {
				b.WriteByte('-')
				skip = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// parseNumber reads a JSON number, rejecting strings and other shapes.
func parseNumber(raw json.RawMessage) (float64, bool) {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

// parseTime reads an RFC3339 string or an epoch seconds/milliseconds number.
func parseTime(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, true
		}
		return time.Time{}, false
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		if f, err := n.Float64(); err == nil {
			sec := f
			if f >= 1e12 {
				sec = f / 1e3
			}
			return time.Unix(int64(sec), 0), true
		}
	}
	return time.Time{}, false
}

// maxScrub caps a scrubbed excerpt.
const maxScrub = 200

// scrub replaces control characters (including newlines) with spaces and
// caps the result at 200 runes plus a trailing ellipsis.
func scrub(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		b.WriteRune(r)
		n++
		if n >= maxScrub {
			return b.String() + "…"
		}
	}
	return b.String()
}
