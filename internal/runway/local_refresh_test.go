package runway

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/codexapi"
	"github.com/licoy/quosparr/internal/sessions"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
)

// localCounts is how often the Codex local data was read: estimate rows,
// official stats, local scans (cost and token chart each) and the session
// report.
func localCounts(f *fake, reports *atomic.Int32) [4]int {
	return [4]int{f.count("daily"), f.count("profile"), f.count("scan"), int(reports.Load())}
}

func localService(t *testing.T) (*Service, *fake, *fakeGrok, *fakeClaude, *atomic.Int32) {
	f := newFake(t)
	g, c := &fakeGrok{}, &fakeClaude{}
	reports := &atomic.Int32{}
	s, _ := newService(t, f, func(o *Options) {
		o.Grok, o.Claude = g, c
		o.Preferences.ShowsQuotaEstimateSummary = true
		o.Preferences.ShowsCostSummary = true
		o.Preferences.ShowsTokenUsageHeatmap = true
		o.Preferences.ShowsSessionRepairSummary = true
		o.Sources.DryRunSessions = func(context.Context) (sessions.Report, error) {
			reports.Add(1)
			return sessions.Report{}, nil
		}
	})
	return s, f, g, c, reports
}

func TestClosedPanelRefreshesQuotaOnly(t *testing.T) {
	s, f, g, c, reports := localService(t)
	refreshAll(t, s)
	s.Tick(f.clk.Advance(time.Duration(s.Preferences().RefreshIntervalSeconds) * time.Second))
	settle(t, s)
	if f.count("quota") != 2 || g.count() != 2 || len(c.calls()) != 2 {
		t.Fatalf("quota refreshes: codex %d, grok %d, claude %d", f.count("quota"), g.count(), len(c.calls()))
	}
	if got := localCounts(f, reports); got != [4]int{} || g.localCount() != 0 || c.localCount() != 0 {
		t.Fatalf("local data read with the panel closed: codex %v, grok %d, claude %d", got, g.localCount(), c.localCount())
	}
}

func TestOpeningThePanelLoadsOnlyItsProviderLocalData(t *testing.T) {
	s, f, g, c, reports := localService(t)
	refreshAll(t, s)
	quota := f.count("quota")
	s.SetPanelOpen(true)
	settle(t, s)
	if got := localCounts(f, reports); got != [4]int{1, 1, 2, 1} {
		t.Fatalf("codex local data after opening = %v", got)
	}
	if g.localCount() != 0 || c.localCount() != 0 {
		t.Fatalf("other providers scanned: grok %d, claude %d", g.localCount(), c.localCount())
	}
	// Reopening within the interval keeps the fresh data.
	s.SetPanelOpen(false)
	s.SetPanelOpen(true)
	settle(t, s)
	if got := localCounts(f, reports); got != [4]int{1, 1, 2, 1} {
		t.Fatalf("reopening rescanned: %v", got)
	}
	p := s.Preferences()
	p.SelectedProvider = store.ProviderGrok
	s.SetPreferences(p)
	settle(t, s)
	if g.localCount() != 1 || c.localCount() != 0 {
		t.Fatalf("switch scans: grok %d, claude %d", g.localCount(), c.localCount())
	}
	if f.count("quota") != quota || g.count() != 1 || len(c.calls()) != 1 {
		t.Fatal("opening or switching fetched a quota")
	}
}

func TestClosedPanelExportStillScansLocalData(t *testing.T) {
	s, f, g, c, reports := localService(t)
	p := s.Preferences()
	p.ExportsStatusJSON = true
	s.SetPreferences(p)
	refreshAll(t, s)
	settle(t, s)
	// The status export mirrors the whole panel, so the local data of all
	// three providers loads even with the panel closed: the estimate rows,
	// the official stats, the local scans and both provider scans.
	if got := localCounts(f, reports); got[0] == 0 || got[1] == 0 || got[2] == 0 {
		t.Fatalf("codex local data missing with the export on: %v", got)
	}
	if g.localCount() == 0 || c.localCount() == 0 {
		t.Fatalf("provider scans missing with the export on: grok %d, claude %d", g.localCount(), c.localCount())
	}
}

func TestSwitchingWithThePanelClosedLoadsNothing(t *testing.T) {
	s, _, g, c, _ := localService(t)
	p := s.Preferences()
	p.SelectedProvider = store.ProviderClaude
	s.SetPreferences(p)
	settle(t, s)
	if g.localCount() != 0 || c.localCount() != 0 || g.count() != 0 || len(c.calls()) != 0 {
		t.Fatalf("closed-panel switch refreshed: grok %d/%d, claude %d/%d", g.count(), g.localCount(), len(c.calls()), c.localCount())
	}
}

func TestScheduledRefreshCoversTheOpenPanelProvider(t *testing.T) {
	s, f, g, c, reports := localService(t)
	p := s.Preferences()
	p.SelectedProvider = store.ProviderClaude
	s.SetPreferences(p)
	showPanel(s)
	s.Tick(f.clk.Advance(time.Duration(p.RefreshIntervalSeconds) * time.Second))
	settle(t, s)
	if c.localCount() != 1 || g.localCount() != 0 || localCounts(f, reports) != [4]int{} {
		t.Fatalf("scheduled local scans: claude %d, grok %d, codex %v", c.localCount(), g.localCount(), localCounts(f, reports))
	}
	if f.count("quota") != 1 || g.count() != 1 || len(c.calls()) != 1 {
		t.Fatal("the schedule did not refresh every quota")
	}
}

func TestPanelOpenedDuringAFullRefreshLoadsAfterIt(t *testing.T) {
	f := newFake(t)
	gate := newGate()
	var reports atomic.Int32
	s, _ := newService(t, f, func(o *Options) {
		o.Preferences.ShowsSessionRepairSummary = true
		o.Sources.DryRunSessions = func(context.Context) (sessions.Report, error) {
			reports.Add(1)
			return sessions.Report{}, nil
		}
		quota := o.Sources.FetchQuota
		o.Sources.FetchQuota = func(ctx context.Context, a codexapi.Auth) (codexapi.QuotaSnapshot, error) {
			gate.waitCtx(ctx)
			return quota(ctx, a)
		}
	})
	done := s.Refresh(usage.RefreshIfChanged)
	gate.waitStarted(t)
	s.SetPanelOpen(true)
	if reports.Load() != 0 {
		t.Fatal("the closed-panel refresh read the session report")
	}
	gate.open()
	wait(t, done)
	settle(t, s)
	if reports.Load() != 1 {
		t.Fatalf("session report after the refresh = %d", reports.Load())
	}
}
