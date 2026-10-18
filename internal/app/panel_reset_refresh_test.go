package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/codex"
	"github.com/licoy/quosparr/internal/resettoday"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

func TestPanelOpenRefreshesResetStatusEveryPresentation(t *testing.T) {
	var sections []runway.Section
	a, view := newPanel(t, nil, panelFixture(), PanelActions{RefreshSection: func(section runway.Section) { sections = append(sections, section) }})
	for open := 1; open <= 2; open++ {
		a.panelOpened() // The lifecycle invoked by OpenPanel, without a native window.
		if len(sections) != open || sections[open-1] != runway.SectionResetToday {
			t.Fatalf("open %d: reset status refresh requests = %v, want one per presentation", open, sections)
		}
		view.Frame()
		view.Frame()
		if len(sections) != open {
			t.Fatal("repaint started another request")
		}
		a.panelClosed()
		view = ui.NewTester(a.panelView, panelWidth, 860)
	}
}

func TestPanelPresentationIsReportedToTheService(t *testing.T) {
	var shown []bool
	a, _ := newPanel(t, nil, panelFixture(), PanelActions{PanelShown: func(open bool) { shown = append(shown, open) }})
	a.panelOpened()
	a.panelClosed()
	if len(shown) != 2 || !shown[0] || shown[1] {
		t.Fatalf("panel presentation = %v, want open then closed", shown)
	}
}

func TestPanelOpenSkipsHiddenResetStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*store.Preferences)
	}{
		{"hidden", func(p *store.Preferences) { p.ShowsRateLimitResetToday = false }},
		{"Grok", func(p *store.Preferences) { p.SelectedProvider = store.ProviderGrok }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			a, _ := newPanel(t, tc.mutate, panelFixture(), PanelActions{RefreshSection: func(runway.Section) { calls++ }})
			a.panelOpened()
			if calls != 0 {
				t.Fatal("hidden reset module refreshed on open")
			}
		})
	}
	a, _ := newPanel(t, nil, panelFixture(), PanelActions{})
	a.panelOpened() // No installed runtime: no panic or fabricated result.
}

// Drive the production UI queue, service and feed refresher together, while
// keeping network/account IO synthetic. The timer interval must not gate opens.
func TestPanelOpenLoadsLatestResetThroughLiveQueue(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "cached result", true: "previous request failed"}[failed], func(t *testing.T) {
			a, _ := newPanel(t, nil, panelFixture(), PanelActions{})
			prefs := a.settings.Prefs()
			reset := newResetBridge(a.settings, prefs, nil)
			var calls atomic.Int32
			started, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			now := time.Now()
			old := resettoday.NewStateSnapshot(resettoday.StateNo, now)
			latest := resettoday.NewStateSnapshot(resettoday.StateYes, now)
			reset.ref.Fetch = func(ctx context.Context, _ time.Time, _ *time.Location) (*resettoday.Snapshot, error) {
				switch calls.Add(1) {
				case 1:
					if failed {
						return nil, errors.New("synthetic feed failure")
					}
					return old, nil
				case 2:
					close(started)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-release:
					}
				}
				return latest, nil
			}
			reset.ref.Refresh(context.Background(), true)
			// Opening also loads the Codex local data; no one is signed in.
			svc := runway.New(runway.Options{Preferences: prefs, ResetToday: reset, Sources: runway.Sources{
				LoadValidAuth: func(context.Context, bool, *codex.Auth) (codex.Auth, error) {
					return codex.Auth{}, &runway.AuthLoadError{}
				},
			}})
			defer svc.Close()
			rt := &liveApp{svc: svc, reset: reset}
			defer rt.closePreferences()
			a.SetPanelActions(rt.panelActions(a.settings))
			a.panelOpened()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("opening did not fetch within the timer interval")
			}
			if !svc.Snapshot().Refresh.Has(runway.SectionResetToday) {
				t.Fatal("pending refresh has no loading state")
			}
			if !failed && svc.Snapshot().ResetToday.Detail != old {
				t.Fatal("refresh cleared the last successful result")
			}
			a.panelClosed()
			a.panelOpened()
			rt.preferenceWork.idle.Wait()
			if calls.Load() != 2 {
				t.Fatal("reopening while pending duplicated the request")
			}
			close(release)
			waitPanelResetIdle(t, rt)
			if svc.Snapshot().ResetToday.Detail != latest {
				t.Fatal("new reset result did not reach the published panel snapshot")
			}
			a.panelClosed()
			a.panelOpened()
			waitPanelResetIdle(t, rt)
			if calls.Load() != 3 {
				t.Fatal("next presentation was suppressed by the recent fetch interval")
			}
		})
	}
}

func waitPanelResetIdle(t *testing.T, rt *liveApp) {
	t.Helper()
	rt.preferenceWork.idle.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.localService().WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPanelOpenResetRefreshSurvivesQueuedFullRefresh(t *testing.T) {
	a, _ := newPanel(t, nil, panelFixture(), PanelActions{})
	prefs := a.settings.Prefs()
	reset := newResetBridge(a.settings, prefs, nil)
	var calls atomic.Int32
	reset.ref.Fetch = func(context.Context, time.Time, *time.Location) (*resettoday.Snapshot, error) {
		calls.Add(1)
		return resettoday.NewStateSnapshot(resettoday.StateNo, time.Now()), nil
	}
	svc := runway.New(runway.Options{Preferences: prefs, ResetToday: reset, Sources: runway.Sources{
		LoadValidAuth: func(context.Context, bool, *codex.Auth) (codex.Auth, error) {
			return codex.Auth{}, errors.New("synthetic signed-out account")
		},
	}})
	defer svc.Close()
	rt := &liveApp{svc: svc, reset: reset}
	defer rt.closePreferences()
	actions := rt.panelActions(a.settings)
	a.SetPanelActions(actions)
	// Hold the real UI queue before consumption, as a burst of UI actions does.
	rt.preferenceWork.running = true
	rt.preferenceWork.idle.Add(1)
	actions.Refresh()
	a.panelOpened()
	go rt.drainPreferences()
	waitPanelResetIdle(t, rt)
	if calls.Load() != 1 {
		t.Fatalf("full refresh swallowed the panel-open reset fetch: calls=%d", calls.Load())
	}
}
