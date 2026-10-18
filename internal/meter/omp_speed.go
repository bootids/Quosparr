package meter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/speed"
)

// ompRowLimit caps how many message rows one scan reads, so a long history
// cannot make the read unbounded.
const ompRowLimit = 20000

// ScanOMPSpeed builds the model speed report of the given oh-my-pi providers
// (for example "kimi-code" and "zai") from omp's local statistics database
// at dbPath. The database is opened read-only because omp writes it while we
// scan. A missing database or an empty provider list means the source has no
// data: a zero report and no error, so the caller shows nothing. A database
// that cannot be read is an error naming the path, never row content.
func ScanOMPSpeed(ctx context.Context, dbPath string, providers []string, now time.Time) (speed.Report, error) {
	if len(providers) == 0 {
		return speed.Report{}, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return speed.Report{}, nil
		}
		return speed.Report{}, fmt.Errorf("omp stats %s: %w", dbPath, err)
	}
	if now.IsZero() {
		now = time.Now()
	}
	db, err := sqlite.Open(ctx, dbPath, sqlite.OpenOptions{ReadOnly: true})
	if err != nil {
		return speed.Report{}, fmt.Errorf("omp stats %s: %w", dbPath, err)
	}
	defer db.Close()
	sessions, err := ompSpeedSessions(ctx, db, providers, now)
	if err != nil {
		return speed.Report{}, fmt.Errorf("omp stats %s: %w", dbPath, err)
	}
	return speed.Build(sessions, now), nil
}

// ompSpeedSessions reads the providers' message rows inside speed.Window and
// groups them into sessions by session_file, keeping the newest
// speed.SessionLimit of them. Rows are read newest first so the row cap
// keeps the newest measurements; timestamps become local times like in the
// other scanners, and speed.Build re-checks the window itself.
func ompSpeedSessions(ctx context.Context, db *sqlite.DB, providers []string, now time.Time) ([]speed.Session, error) {
	var q strings.Builder
	q.WriteString(`SELECT session_file, model, timestamp, duration, ttft, output_tokens
		FROM messages WHERE provider IN (`)
	args := make([]any, 0, len(providers)+2)
	for i, p := range providers {
		if i > 0 {
			q.WriteByte(',')
		}
		q.WriteByte('?')
		args = append(args, p)
	}
	q.WriteString(") AND timestamp >= ? ORDER BY timestamp DESC LIMIT ?")
	args = append(args, now.Add(-speed.Window).UnixMilli(), int64(ompRowLimit))
	rows, err := db.Query(ctx, q.String(), args...)
	if err != nil {
		return nil, err
	}
	byFile := map[string]*speed.Session{}
	var files []string
	for _, row := range rows.Values {
		sample, file, ok := ompSample(row)
		if !ok {
			continue
		}
		session := byFile[file]
		if session == nil {
			// The base of a bare name is the name itself, so a short value
			// stays raw.
			session = &speed.Session{ID: filepath.Base(file)}
			byFile[file] = session
			files = append(files, file)
		}
		session.Samples = append(session.Samples, sample)
		if sample.At.After(session.UpdatedAt) {
			session.UpdatedAt = sample.At
		}
	}
	sessions := make([]speed.Session, 0, len(files))
	for _, file := range files {
		sessions = append(sessions, *byFile[file])
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if !sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
			return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
		}
		return sessions[i].ID < sessions[j].ID
	})
	if len(sessions) > speed.SessionLimit {
		sessions = sessions[:speed.SessionLimit]
	}
	return sessions, nil
}

// ompSample turns one messages row into a speed.Sample named by its session
// file. A row without a model, without a timestamp, or with neither a
// positive duration nor a positive ttft carries no measurement, as does a
// corrupt short row: all are dropped. The output part exists only when the
// duration does; omp logs ttft per call and tokens per turn, so samples may
// carry either part alone.
func ompSample(row []any) (speed.Sample, string, bool) {
	if len(row) < 6 {
		return speed.Sample{}, "", false
	}
	file, _ := row[0].(string)
	model, _ := row[1].(string)
	ts, hasTS := ompInt(row[2])
	duration, _ := ompInt(row[3])
	ttft, _ := ompInt(row[4])
	outputTokens, _ := ompInt(row[5])
	if model == "" || !hasTS || (duration <= 0 && ttft <= 0) {
		return speed.Sample{}, "", false
	}
	sample := speed.Sample{At: time.UnixMilli(ts), Model: model}
	if ttft > 0 {
		sample.HasTTFT, sample.TTFT = true, time.Duration(ttft)*time.Millisecond
	}
	if duration > 0 {
		sample.Decode = time.Duration(duration) * time.Millisecond
		sample.OutputTokens = outputTokens
	}
	return sample, file, true
}

// ompInt reads a SQLite numeric column; a NULL or non-numeric value reads as
// absent.
func ompInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	}
	return 0, false
}
