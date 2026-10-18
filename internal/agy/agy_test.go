package agy

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

// fixture holds the committed success payload copied from
// ../aimeter/research/fixtures/agy/usage-success.json.
var fixture = func() string {
	b, err := os.ReadFile(filepath.Join("testdata", "usage-success.json"))
	if err != nil {
		panic(err)
	}
	return string(b)
}()

// writeFake creates a fake agy executable in dir that prints body and exits
// with code. On windows a .cmd batch file types the body from a side file.
func writeFake(t *testing.T, dir, body string, exit int) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "body.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	bodyPath := filepath.Join(dir, "body.json")
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "agy.cmd")
		script := "@echo off\r\ntype \"" + bodyPath + "\"\r\nexit " + itoa(exit) + "\r\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path := filepath.Join(dir, "agy")
	script := "#!/bin/sh\ncat \"" + bodyPath + "\"\nexit " + itoa(exit) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeSleepyFake creates a fake agy that stalls longer than any test
// deadline.
func writeSleepyFake(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "agy.cmd")
		if err := os.WriteFile(path, []byte("@echo off\r\nping -n 10 127.0.0.1 >nul\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path := filepath.Join(dir, "agy")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// fakeHome builds a home with the profile dir and (optionally) a binary
// installed at .local/bin/agy.
func fakeHome(t *testing.T, installBin bool) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "antigravity-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	if installBin {
		if err := os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		name := "agy"
		if runtime.GOOS == "windows" {
			name = "agy.exe"
		}
		if err := os.WriteFile(filepath.Join(home, ".local", "bin", name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestDetectOverrideAndPath(t *testing.T) {
	dir := t.TempDir()
	bin := writeFake(t, dir, fixture, 0)
	home := fakeHome(t, false)

	// Explicit override used as-is.
	d, ok := Detect(bin, home)
	if !ok || d.Bin != bin {
		t.Fatalf("override: ok=%v bin=%q", ok, d.Bin)
	}

	// PATH lookup.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	d, ok = Detect("", home)
	if !ok {
		t.Fatalf("path lookup failed: %+v", d)
	}
	if want := filepath.Join(home, ".gemini", "antigravity-cli"); d.ProfileDir != want {
		t.Fatalf("profile dir = %q, want %q", d.ProfileDir, want)
	}
}

func TestDetectHomeFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no agy on PATH
	home := fakeHome(t, true)
	d, ok := Detect("", home)
	if !ok {
		t.Fatal("Detect failed for home fallback")
	}
	name := "agy"
	if runtime.GOOS == "windows" {
		name = "agy.exe"
	}
	if want := filepath.Join(home, ".local", "bin", name); d.Bin != want {
		t.Fatalf("bin = %q, want %q", d.Bin, want)
	}
}

func TestDetectMissingProfileDir(t *testing.T) {
	dir := t.TempDir()
	bin := writeFake(t, dir, fixture, 0)
	if _, ok := Detect(bin, t.TempDir()); ok {
		t.Fatal("Detect ok without profile dir")
	}
}

func TestFetchSuccess(t *testing.T) {
	dir := t.TempDir()
	bin := writeFake(t, dir, fixture, 0)
	st := Fetch(context.Background(), bin, time.Unix(0, 0).UTC())
	if st.Status != meter.StatusOK {
		t.Fatalf("status = %q, message %q", st.Status, st.Message)
	}
	if len(st.Limits) != 4 {
		t.Fatalf("limits = %d, want 4", len(st.Limits))
	}
	byID := map[string]meter.Limit{}
	for _, l := range st.Limits {
		byID[l.ID] = l
	}
	for _, id := range []string{"gemini-weekly", "gemini-5h", "3p-weekly", "3p-5h"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing limit %q", id)
		}
	}
	w := byID["gemini-weekly"]
	if w.WindowMinutes != meter.WeeklyMinutes {
		t.Errorf("weekly WindowMinutes = %d, want %d", w.WindowMinutes, meter.WeeklyMinutes)
	}
	if w.Unit != "percent" || w.Title != "Weekly Limit Remaining" {
		t.Errorf("unit/title = %q/%q", w.Unit, w.Title)
	}
	if w.Group != "Gemini Models" || w.GroupDescription != "Synthetic Gemini group" {
		t.Errorf("group = %q/%q", w.Group, w.GroupDescription)
	}
	// (1 - 0.9925113320350647) * 100
	if !w.HasPercent || math.Abs(w.Percent-0.7488667964935298) > 1e-9 {
		t.Errorf("weekly percent = %v has=%v", w.Percent, w.HasPercent)
	}
	if meter.Has(w.Used) || meter.Has(w.LimitValue) || meter.Has(w.Remaining) {
		t.Errorf("numeric fields set: %+v", w)
	}
	if !w.HasReset || w.ResetsAt != time.Date(2026, 8, 21, 22, 8, 38, 0, time.UTC) {
		t.Errorf("weekly reset = %v has=%v", w.ResetsAt, w.HasReset)
	}
	if h := byID["gemini-5h"]; h.WindowMinutes != meter.FiveHourMinutes || h.Title != "Five Hour Limit Remaining" {
		t.Errorf("5h window/title = %d/%q", h.WindowMinutes, h.Title)
	}
	if p := byID["3p-5h"]; !p.HasPercent || p.Percent != 0 {
		t.Errorf("3p-5h percent = %v has=%v", p.Percent, p.HasPercent)
	}
	// Full pools drop the reset: the official panel shows "Quota available"
	// instead of a countdown.
	for _, id := range []string{"3p-weekly", "3p-5h"} {
		if p := byID[id]; p.HasPercent && p.Percent <= 0 && p.HasReset {
			t.Errorf("%s full pool kept its reset", id)
		}
		if p := byID[id]; p.Group != "Claude and GPT models" {
			t.Errorf("%s group = %q", id, p.Group)
		}
	}
}

// A non-zero exit with a valid shape is still success, mirroring the aimeter
// integration that runs the fake agy with FAKE_EXIT=7.
func TestFetchExit7WithValidShapeStillOK(t *testing.T) {
	dir := t.TempDir()
	bin := writeFake(t, dir, fixture, 7)
	st := Fetch(context.Background(), bin, time.Now())
	if st.Status != meter.StatusOK || len(st.Limits) != 4 {
		t.Fatalf("status = %q, limits = %d, message %q", st.Status, len(st.Limits), st.Message)
	}
}

func TestFetchGarbage(t *testing.T) {
	garbage := "NOT JSON \x01\x02\nline2\n" + strings.Repeat("Z", 5000)
	dir := t.TempDir()
	bin := writeFake(t, dir, garbage, 3)
	st := Fetch(context.Background(), bin, time.Now())
	if st.Status != meter.StatusSubprocess {
		t.Fatalf("status = %q", st.Status)
	}
	if strings.ContainsAny(st.Message, "\n\r\t") {
		t.Errorf("message contains newline/control: %q", st.Message)
	}
	if !strings.Contains(st.Message, "agy exit 3") {
		t.Errorf("message missing exit code: %q", st.Message)
	}
	// Two scrubbed excerpts capped at 200 runes each plus framing.
	if n := len([]rune(st.Message)); n > 2*201+64 {
		t.Errorf("message too long: %d runes", n)
	}
	if strings.Contains(st.Message, strings.Repeat("Z", 201)) {
		t.Errorf("raw garbage leaked past the cap")
	}
}

func TestFetchMissingBin(t *testing.T) {
	st := Fetch(context.Background(), filepath.Join(t.TempDir(), "no-such-agy"), time.Now())
	if st.Status != meter.StatusSubprocess {
		t.Fatalf("status = %q, message %q", st.Status, st.Message)
	}
}

func TestFetchTimeout(t *testing.T) {
	dir := t.TempDir()
	bin := writeSleepyFake(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	st := Fetch(ctx, bin, time.Now())
	if st.Status != meter.StatusTimeout {
		t.Fatalf("status = %q, message %q", st.Status, st.Message)
	}
	if st.Message != "agy usage timed out" {
		t.Errorf("message = %q", st.Message)
	}
}

func TestNormalizeFallbackIDAndWindows(t *testing.T) {
	body := `{"command":{"data":{"groups":[
		{"name":"Claude and GPT models!","buckets":[
			{"window":"monthly","reset_time":1785000000},
			{"window":"7d","remaining_fraction":"oops"}
		]}]}}}`
	var doc usageDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}
	var groups []group
	if err := json.Unmarshal(doc.Command.Data.GroupsRaw, &groups); err != nil {
		t.Fatal(err)
	}
	ls := limits(groups)
	if len(ls) != 2 {
		t.Fatalf("limits = %d", len(ls))
	}
	if ls[0].ID != "claude-and-gpt-models:monthly" || ls[0].WindowMinutes != 0 {
		t.Errorf("fallback id = %q window = %d", ls[0].ID, ls[0].WindowMinutes)
	}
	if !ls[0].HasReset || ls[0].ResetsAt.Location() != time.UTC {
		t.Errorf("epoch reset not UTC: %v", ls[0].ResetsAt)
	}
	if ls[1].WindowMinutes != meter.WeeklyMinutes {
		t.Errorf("7d window = %d", ls[1].WindowMinutes)
	}
	if ls[1].HasPercent {
		t.Errorf("non-numeric remaining_fraction produced a percent")
	}
}

func TestScrubCapsAndControls(t *testing.T) {
	in := "a\nb\x00c\td" + strings.Repeat("é", 300)
	out := scrub(in)
	if strings.ContainsAny(out, "\n\x00\t") {
		t.Errorf("control chars survived: %q", out)
	}
	if n := len([]rune(out)); n != 201 || !strings.HasSuffix(out, "…") {
		t.Errorf("scrub length = %d", n)
	}
}
