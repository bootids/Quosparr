package runway

import (
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/export"
)

// widgetBlock is a ready-made widget provider block for the fake states.
func widgetBlock(p export.Provider, rows ...export.WidgetQuota) *export.ProviderSnapshot {
	return &export.ProviderSnapshot{
		Provider: p, Availability: export.Available, TokenSource: export.TokensThisMac, Quota: rows,
	}
}

// statusSnapshotLocked must carry the Grok, Claude and reset-today widget
// blocks in the status file, without the widget's model-specific quota
// filtering (each row keeps its source; readers filter).
func TestStatusSnapshotLockedExportsProviders(t *testing.T) {
	stdRow := export.WidgetQuota{Title: "5 hours", Source: export.QuotaStandard, UsedPercent: 40, RemainingPercent: 60}
	modelRow := export.WidgetQuota{Title: "opus", Source: export.QuotaModelSpecific, UsedPercent: 5, RemainingPercent: 95}
	reset := &export.ResetTodaySnapshot{State: export.ResetYes, FetchedAt: time.Now()}
	f := newFake(t)
	s, _ := newService(t, f, func(o *Options) {
		o.Grok = &fakeGrok{state: GrokState{Availability: GrokReady,
			Widget: widgetBlock(export.ProviderGrok, stdRow, modelRow)}}
		o.Claude = &fakeClaude{state: ClaudeState{Availability: ClaudeReady,
			Widget: widgetBlock(export.ProviderClaude, stdRow)}}
		o.ResetToday = &fakeResetToday{state: ResetTodayState{Widget: reset}}
	})
	s.mu.Lock()
	snap := s.statusSnapshotLocked(time.Now())
	s.mu.Unlock()
	p := snap.Providers
	if p == nil {
		t.Fatal("providers missing")
	}
	if p.Grok == nil || p.Grok.Provider != export.ProviderGrok {
		t.Fatalf("grok = %+v", p.Grok)
	}
	if len(p.Grok.Quota) != 2 || p.Grok.Quota[1].Source != export.QuotaModelSpecific {
		t.Fatalf("grok quota rows filtered: %+v", p.Grok.Quota)
	}
	if p.Claude == nil || p.Claude.Provider != export.ProviderClaude || len(p.Claude.Quota) != 1 {
		t.Fatalf("claude = %+v", p.Claude)
	}
	if p.ResetToday == nil || p.ResetToday.State != export.ResetYes {
		t.Fatalf("resetToday = %+v", p.ResetToday)
	}
	if p.Codex == nil || p.Codex.Provider != export.ProviderCodex {
		t.Fatalf("codex = %+v", p.Codex)
	}
}

// An empty state still writes the providers block: Codex carries its
// not-logged-in availability so a remote reader can tell it from an absent
// provider, while the other blocks stay omitted.
func TestStatusProvidersCarryCodexAvailability(t *testing.T) {
	p := statusExportProviders(Snapshot{})
	if p == nil {
		t.Fatal("providers missing")
	}
	if p.Codex == nil || p.Codex.Availability != export.NotLoggedIn {
		t.Fatalf("codex = %+v", p.Codex)
	}
	if p.Grok != nil || p.Claude != nil || p.ResetToday != nil {
		t.Fatalf("empty state wrote grok/claude/resetToday: %+v", p)
	}
}
