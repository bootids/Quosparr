package runway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/export/swiftjson"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/sessions"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

type fakeRemoteFetcher struct {
	mu   sync.Mutex
	data []byte
	err  error
}

func (f *fakeRemoteFetcher) FetchStatus(ctx context.Context) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data, f.err
}

func remoteIntPtr(v int) *int { return &v }

func remoteStringPtr(v string) *string { return &v }

func remoteDecimalPtr(s string) *swiftjson.Decimal {
	d := swiftjson.MustDecimal(s)
	return &d
}

func remoteTestNow() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC) }

func remoteResetTime() time.Time { return remoteTestNow().Add(90 * time.Minute) }

func remoteFullStatus() export.StatusSnapshot {
	now, reset := remoteTestNow(), remoteResetTime()
	plan := "plus"
	balance := 1.25
	secondary := export.Window{UsedPercent: 30, ResetsAt: &reset}
	quota := export.NewStatusQuota(export.QuotaInput{
		Plan: &plan, Primary: export.Window{UsedPercent: 42, ResetsAt: &reset},
		Secondary:      &secondary,
		Additional:     []export.NamedWindow{{Name: "gpt-5-codex", Window: export.Window{UsedPercent: 7, ResetsAt: &reset}}},
		CreditsBalance: &balance, UpdatedAt: now,
	})
	cost := export.CostSummary{
		Source: "localSessions", Confidence: "priced",
		Window:       export.Interval{Start: now.Add(-time.Hour), Duration: 3600},
		EstimatedUSD: remoteDecimalPtr("1.5"),
		Totals:       sessions.Totals{TotalTokens: 100, UncachedInputTokens: 60, CachedInputTokens: 20, OutputTokens: 20, Turns: 4, Threads: 2},
		DailyRows: []export.CostDailyRow{{
			Date: "2026-10-18", Totals: sessions.Totals{TotalTokens: 100, Turns: 4},
			EstimatedUSD: remoteDecimalPtr("1.5"), RawCredits: 2,
		}},
		Warnings: []string{}, PricingVersion: "test", CalculatedAt: now,
	}
	codex := export.NewProviderSnapshot(export.ProviderSnapshot{
		Provider: export.ProviderCodex, Availability: export.Available, Plan: &plan,
		Quota: []export.WidgetQuota{
			export.NewWidgetQuota("5-hour", remoteIntPtr(300), export.QuotaStandard, 42, 58, &reset),
			export.NewWidgetQuota("Weekly", remoteIntPtr(10080), export.QuotaStandard, 30, 70, &reset),
			export.NewWidgetQuota("gpt-5-codex", remoteIntPtr(300), export.QuotaModelSpecific, 7, 93, &reset),
		},
		TokenSource:  export.TokensThisMac,
		DailyTokens:  []export.DailyTokens{export.NewDailyTokens("2026-10-17", 1200)},
		ResetCredits: &export.ResetCredits{AvailableCount: 2, ExpiringCount: 1},
	})
	grok := export.NewProviderSnapshot(export.ProviderSnapshot{
		Provider: export.ProviderGrok, Availability: export.Available, Plan: remoteStringPtr("supergrok"),
		Quota: []export.WidgetQuota{
			export.NewWidgetQuota("Included", remoteIntPtr(43200), export.QuotaStandard, 30, 70, &reset),
			export.NewWidgetQuota("grok-4", remoteIntPtr(300), export.QuotaModelSpecific, 10, 90, nil),
		},
	})
	claude := export.NewProviderSnapshot(export.ProviderSnapshot{
		Provider: export.ProviderClaude, Availability: export.Available,
		Quota: []export.WidgetQuota{export.NewWidgetQuota("5h", remoteIntPtr(300), export.QuotaStandard, 15, 85, &reset)},
	})
	return export.StatusSnapshot{
		GeneratedAt: now, Quota: &quota, Cost: &cost,
		Sessions: &sessions.Summary{Items: []sessions.Item{{
			ID: "s1", Title: "Fix bug", ProjectName: "Quosparr", UpdatedAt: now,
			Totals: sessions.Totals{TotalTokens: 100, Turns: 4},
		}}},
		Providers: &export.StatusProviders{
			Codex: &codex, Grok: &grok, Claude: &claude,
			ResetToday: &export.ResetTodaySnapshot{State: export.ResetYes, FetchedAt: now},
		},
	}
}

func remoteTestPrefs() store.Preferences {
	p := store.DefaultPreferences()
	p.FloatingWindow.Enabled = true
	return p
}

func TestBuildRemoteSnapshotFull(t *testing.T) {
	prefs := remoteTestPrefs()
	prefs.ShowsModelSpecificQuotaUsage = true
	snap := BuildRemoteSnapshot(remoteFullStatus(), prefs, l10n.New(l10n.English), remoteTestNow())

	q := snap.Codex.Quota
	if q == nil {
		t.Fatal("codex quota missing")
	}
	if q.Plan != "plus" || q.Primary.UsedPercent != 42 || q.Primary.WindowMinutes != FiveHourMinutes {
		t.Fatalf("primary = %+v", q.Primary)
	}
	if q.Secondary == nil || q.Secondary.UsedPercent != 30 || q.Secondary.WindowMinutes != WeeklyMinutes {
		t.Fatalf("secondary = %+v", q.Secondary)
	}
	if len(q.AdditionalWindows) != 1 || q.AdditionalWindows[0].Name != "gpt-5-codex" {
		t.Fatalf("additional = %+v", q.AdditionalWindows)
	}
	if q.CreditsBalance == nil || *q.CreditsBalance != 1.25 {
		t.Fatalf("credits balance = %+v", q.CreditsBalance)
	}
	if len(snap.Codex.Meters) != 3 {
		t.Fatalf("codex meters = %d", len(snap.Codex.Meters))
	}
	if snap.Codex.StatusText == "" || snap.Codex.QuotaText == "" || len(snap.Codex.QuotaLines) == 0 {
		t.Fatalf("codex text = %q lines %d", snap.Codex.StatusText, len(snap.Codex.QuotaLines))
	}
	c := snap.Codex.Cost
	if c == nil || c.EstimatedUSD == nil || !c.EstimatedUSD.Equal(decimal.MustParse("1.5")) || len(c.DailyRows) != 1 {
		t.Fatalf("cost = %+v", c)
	}
	if snap.Codex.DisplayedCost == nil || snap.Codex.CostText == "" || len(snap.Codex.CostLines) == 0 {
		t.Fatalf("displayed cost = %+v text %q", snap.Codex.DisplayedCost, snap.Codex.CostText)
	}
	if len(snap.Codex.RecentSessions) != 1 || len(snap.Codex.RecentSessionLines) != 1 {
		t.Fatalf("recent sessions = %+v", snap.Codex.RecentSessions)
	}
	if snap.Codex.Heatmap.Local["2026-10-17"] != 1200 {
		t.Fatalf("heatmap = %+v", snap.Codex.Heatmap)
	}
	if snap.Codex.ResetCredits == nil || snap.Codex.ResetCredits.AvailableCount != 2 {
		t.Fatalf("reset credits = %+v", snap.Codex.ResetCredits)
	}
	if snap.Codex.ResetCreditsText == "" || len(snap.Codex.ResetCreditLines) == 0 {
		t.Fatalf("reset credits text = %q", snap.Codex.ResetCreditsText)
	}
	if snap.Grok.Availability != GrokReady || len(snap.Grok.Meters) != 2 || snap.Grok.Plan != "supergrok" {
		t.Fatalf("grok = %+v", snap.Grok)
	}
	if snap.Claude.Availability != ClaudeReady || len(snap.Claude.Meters) != 1 {
		t.Fatalf("claude = %+v", snap.Claude)
	}
	if !snap.ResetToday.HasData || snap.ResetToday.Widget == nil || snap.ResetToday.Widget.State != export.ResetYes {
		t.Fatalf("reset today = %+v", snap.ResetToday)
	}
	if snap.ResetToday.Text == "" || len(snap.ResetToday.Lines) == 0 {
		t.Fatalf("reset today text = %q", snap.ResetToday.Text)
	}
	if snap.StatusBar.Text == "" {
		t.Fatal("status bar text empty")
	}
	if len(snap.Floating.Providers) == 0 {
		t.Fatal("floating empty")
	}
}

func TestBuildRemoteSnapshotEmpty(t *testing.T) {
	snap := BuildRemoteSnapshot(export.StatusSnapshot{}, remoteTestPrefs(), l10n.New(l10n.English), remoteTestNow())
	if snap.Codex.Quota != nil || snap.Codex.Cost != nil || snap.Codex.ResetCredits != nil {
		t.Fatalf("codex = %+v", snap.Codex)
	}
	if snap.Grok.Availability != GrokCLIUnavailable || snap.Claude.Availability != ClaudeNotLoggedIn {
		t.Fatalf("providers = %+v %+v", snap.Grok, snap.Claude)
	}
	if snap.ResetToday.HasData {
		t.Fatal("reset today should have no data")
	}
}

func TestBuildRemoteSnapshotFiltersModelSpecific(t *testing.T) {
	st := remoteFullStatus()
	for _, show := range []bool{false, true} {
		prefs := remoteTestPrefs()
		prefs.ShowsModelSpecificQuotaUsage = show
		snap := BuildRemoteSnapshot(st, prefs, l10n.New(l10n.English), remoteTestNow())
		want := 2
		if show {
			want = 3
		}
		if got := len(snap.Codex.Meters); got != want {
			t.Errorf("show=%v codex meters = %d, want %d", show, got, want)
		}
		wantGrok := 1
		if show {
			wantGrok = 2
		}
		if got := len(snap.Grok.Meters); got != wantGrok {
			t.Errorf("show=%v grok meters = %d, want %d", show, got, wantGrok)
		}
		for _, m := range snap.Grok.Meters {
			if !show && m.Source == SourceModelSpecific {
				t.Errorf("show=%v model-specific meter leaked: %+v", show, m)
			}
		}
	}
}

func waitForRemote(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestRemoteServiceFirstFrameAndClose(t *testing.T) {
	data, err := json.Marshal(remoteFullStatus())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemoteFetcher{data: data}
	s := NewRemote(RemoteOptions{
		Fetcher: f, Preferences: remoteTestPrefs(),
		OnSnapshot: func(Snapshot) {},
	})
	s.Start(context.Background())
	defer s.Close()
	waitForRemote(t, 2*time.Second, func() bool { return s.Snapshot().Codex.Quota != nil })
	snap := s.Snapshot()
	if snap.GeneratedAt.IsZero() || snap.Seq == 0 {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestRemoteServiceFetchError(t *testing.T) {
	f := &fakeRemoteFetcher{err: errors.New("ssh failed: secret stdout leak")}
	s := NewRemote(RemoteOptions{Fetcher: f, Preferences: remoteTestPrefs()})
	s.Start(context.Background())
	defer s.Close()
	waitForRemote(t, 2*time.Second, func() bool { return s.Snapshot().Codex.LastError != "" })
	snap := s.Snapshot()
	for name, msg := range map[string]string{
		"codex": snap.Codex.LastError, "grok": snap.Grok.LastError, "claude": snap.Claude.LastError,
	} {
		if msg == "" {
			t.Fatalf("%s last error empty", name)
		}
		if strings.Contains(msg, "secret") || strings.Contains(msg, "stdout") {
			t.Fatalf("%s error leaks remote content: %q", name, msg)
		}
	}
	if snap.StatusBar.Tooltip == "" {
		t.Fatal("tooltip empty")
	}
}

func TestRemoteServiceDecodeError(t *testing.T) {
	f := &fakeRemoteFetcher{data: []byte(`{"quota":"MARKER LEAK"}`)}
	s := NewRemote(RemoteOptions{Fetcher: f, Preferences: remoteTestPrefs()})
	s.Start(context.Background())
	defer s.Close()
	waitForRemote(t, 2*time.Second, func() bool { return s.Snapshot().Codex.LastError != "" })
	if msg := s.Snapshot().Codex.LastError; strings.Contains(msg, "MARKER") {
		t.Fatalf("decode error leaks remote content: %q", msg)
	}
}

func TestRemoteServiceRefresh(t *testing.T) {
	data, err := json.Marshal(remoteFullStatus())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemoteFetcher{data: data}
	s := NewRemote(RemoteOptions{Fetcher: f, Preferences: remoteTestPrefs()})
	done := s.Refresh()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not finish")
	}
	if s.Snapshot().Codex.Quota == nil {
		t.Fatal("no data after refresh")
	}
	s.Close()
	select {
	case <-s.Refresh():
	default:
		t.Fatal("refresh after close should return a closed channel")
	}
}

func TestRemoteSourceKeysTranslateEveryLanguage(t *testing.T) {
	keys := []l10n.Key{l10n.KeyRemoteSourceUnavailable, l10n.KeyRemoteSourceErrorFetch, l10n.KeyRemoteSourceErrorDecode}
	for _, lang := range l10n.Languages() {
		for _, k := range keys {
			if s, ok := l10n.Lookup(lang, k); !ok || strings.TrimSpace(s) == "" {
				t.Fatalf("%v missing translation for %v", lang, k)
			}
		}
	}
}
