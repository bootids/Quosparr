package runway

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/licoy/quosparr/internal/codexapi"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/export/swiftjson"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/sessions"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

// RemoteFetcher pulls the raw bytes of a remote status.json.
type RemoteFetcher interface {
	FetchStatus(ctx context.Context) ([]byte, error)
}

// RemoteOptions configures a RemoteService.
type RemoteOptions struct {
	Fetcher RemoteFetcher
	// Preferences drive the same rendering choices as the local Service.
	Preferences store.Preferences
	// SystemLanguages are the OS preferred languages, for Language "system".
	SystemLanguages []string
	// OnSnapshot receives every published snapshot, in order, from one
	// goroutine; intermediate snapshots may be skipped when the receiver is
	// slow (the latest always arrives).
	OnSnapshot func(Snapshot)
	// Now replaces the clock; the default is time.Now.
	Now func() time.Time
}

// RemoteService is the data side of the remote view mode: it polls a
// RemoteFetcher, decodes the exported status file and rebuilds the same
// Snapshot the UI renders locally. All methods are safe from any goroutine.
type RemoteService struct {
	fetcher  RemoteFetcher
	prefs    store.Preferences
	lc       l10n.Localizer
	nowFn    func() time.Time
	onSnap   func(Snapshot)
	interval time.Duration

	root   context.Context
	cancel context.CancelFunc
	lifeMu sync.Mutex
	closed bool
	wg     sync.WaitGroup

	// fetchMu serializes fetch cycles; the loop and Refresh share it.
	fetchMu sync.Mutex

	mu      sync.Mutex
	seq     uint64
	st      export.StatusSnapshot
	errText string

	pubMu  sync.Mutex
	latest Snapshot
	pubSig chan struct{}
}

// NewRemote builds a RemoteService. Nothing runs until Start (or Refresh).
func NewRemote(opts RemoteOptions) *RemoteService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	interval := time.Duration(opts.Preferences.RefreshIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Duration(store.DefaultRefreshIntervalSeconds) * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &RemoteService{
		fetcher:  opts.Fetcher,
		prefs:    opts.Preferences,
		lc:       l10n.ForPreference(string(opts.Preferences.Language), opts.SystemLanguages),
		nowFn:    opts.Now,
		onSnap:   opts.OnSnapshot,
		interval: interval,
		root:     ctx,
		cancel:   cancel,
		pubSig:   make(chan struct{}, 1),
	}
}

func (s *RemoteService) now() time.Time { return s.nowFn() }

// Start launches the publisher and the poll loop, and fetches at once. It
// returns immediately; Close stops everything.
func (s *RemoteService) Start(ctx context.Context) {
	s.wg.Add(2)
	go func() { defer s.wg.Done(); s.publishLoop() }()
	go func() { defer s.wg.Done(); s.fetchLoop(ctx) }()
}

// Close cancels all work, waits for it and stops publishing.
func (s *RemoteService) Close() {
	s.lifeMu.Lock()
	s.closed = true
	s.lifeMu.Unlock()
	s.cancel()
	s.wg.Wait()
}

// Snapshot returns the latest published snapshot.
func (s *RemoteService) Snapshot() Snapshot {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	return s.latest
}

// Refresh fetches at once; the returned channel closes when that cycle ends.
func (s *RemoteService) Refresh() <-chan struct{} {
	done := make(chan struct{})
	s.lifeMu.Lock()
	closed := s.closed
	s.lifeMu.Unlock()
	if closed {
		close(done)
		return done
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(done)
		s.fetchOnce(s.root)
	}()
	return done
}

// WidgetSnapshot renders the latest snapshot as the widget file payload, the
// same way the local Service builds it.
func (s *RemoteService) WidgetSnapshot(now time.Time) export.Snapshot {
	s.pubMu.Lock()
	snap := s.latest
	s.pubMu.Unlock()
	s.mu.Lock()
	p := s.prefs
	s.mu.Unlock()
	return makeWidgetSnapshot(snap, p, now)
}

func (s *RemoteService) fetchLoop(ctx context.Context) {
	s.fetchOnce(ctx)
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-s.root.Done():
			return
		case <-ctx.Done():
			return
		case <-t.C:
			s.fetchOnce(ctx)
		}
	}
}

// errRemoteDecode marks an undecodable status file. It never carries the
// remote content; only the category reaches the user.
var errRemoteDecode = errors.New("remote status file undecodable")

func (s *RemoteService) fetchOnce(ctx context.Context) {
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	s.setRefreshing(true)
	errText := ""
	data, err := s.fetcher.FetchStatus(ctx)
	if err == nil {
		var st export.StatusSnapshot
		if derr := json.Unmarshal(data, &st); derr != nil {
			err = errRemoteDecode
		} else {
			s.mu.Lock()
			s.st = st
			s.mu.Unlock()
		}
	}
	if err != nil {
		category := l10n.KeyRemoteSourceErrorFetch
		if err == errRemoteDecode {
			category = l10n.KeyRemoteSourceErrorDecode
		}
		errText = l10n.Format(s.lc.Text(l10n.KeyRemoteSourceUnavailable), s.lc.Text(category))
	}
	s.mu.Lock()
	s.errText = errText
	s.mu.Unlock()
	s.setRefreshing(false)
}

func (s *RemoteService) setRefreshing(on bool) {
	s.mu.Lock()
	st, errText := s.st, s.errText
	now := s.now()
	var snap Snapshot
	if errText != "" {
		snap = remoteErrorSnapshot(errText, s.prefs, s.lc, now)
	} else {
		snap = BuildRemoteSnapshot(st, s.prefs, s.lc, now)
	}
	snap.Refresh.All = on
	s.seq++
	snap.Seq = s.seq
	s.mu.Unlock()
	s.pubMu.Lock()
	s.latest = snap
	s.pubMu.Unlock()
	select {
	case s.pubSig <- struct{}{}:
	default:
	}
}

func (s *RemoteService) publishLoop() {
	for {
		select {
		case <-s.root.Done():
			return
		case <-s.pubSig:
			if s.onSnap != nil {
				s.onSnap(s.Snapshot())
			}
		}
	}
}

// remoteErrorSnapshot is the view of a failed fetch: every provider carries
// the user-facing error; no remote content is ever embedded.
func remoteErrorSnapshot(msg string, prefs store.Preferences, lc l10n.Localizer, now time.Time) Snapshot {
	snap := Snapshot{GeneratedAt: now, Language: lc.Language(), Provider: prefs.SelectedProvider}
	snap.Codex.StatusText = msg
	snap.Codex.LastError = msg
	snap.Grok = GrokState{Availability: GrokFailed, LastError: msg}
	snap.Claude = ClaudeState{Availability: ClaudeFailed, LastError: msg}
	in := StatusInputs{
		Prefs: prefs, Provider: prefs.SelectedProvider,
		CodexText: snap.Codex.StatusText, Grok: snap.Grok, Claude: snap.Claude, Now: now,
	}
	snap.StatusBar = BuildStatusBar(lc, in)
	if prefs.FloatingWindow.Enabled {
		snap.Floating = BuildFloating(lc, in)
	}
	return snap
}

// BuildRemoteSnapshot rebuilds the local Snapshot from an exported status
// file, so the remote view renders exactly like the local one.
func BuildRemoteSnapshot(st export.StatusSnapshot, prefs store.Preferences, lc l10n.Localizer, now time.Time) Snapshot {
	snap := Snapshot{GeneratedAt: now, Language: lc.Language(), Provider: prefs.SelectedProvider}
	var providers *export.StatusProviders
	if st.Providers != nil {
		providers = st.Providers
	}
	var codex *export.ProviderSnapshot
	if providers != nil && providers.Codex != nil {
		codex = providers.Codex
	}
	snap.Codex = remoteCodexView(st, codex, prefs, lc, now)
	snap.Grok = remoteGrokState(providers, prefs, lc, now)
	snap.Claude = remoteClaudeState(providers, prefs, lc, now)
	snap.ResetToday = remoteResetToday(providers, lc)
	in := StatusInputs{
		Prefs: prefs, Provider: prefs.SelectedProvider,
		CodexText: snap.Codex.StatusText, CodexMeters: snap.Codex.Meters,
		Grok: snap.Grok, Claude: snap.Claude, Now: now,
	}
	snap.StatusBar = BuildStatusBar(lc, in)
	if prefs.FloatingWindow.Enabled {
		snap.Floating = BuildFloating(lc, in)
	}
	return snap
}

func remoteCodexView(st export.StatusSnapshot, p *export.ProviderSnapshot, prefs store.Preferences, lc l10n.Localizer, now time.Time) CodexView {
	var v CodexView
	if q := importStatusQuota(st.Quota); q != nil {
		v.Quota = q
		v.Authenticated = true
		v.StatusText = MenuBarText(lc, *q, now)
		v.Meters = QuotaMeters(lc, *q, prefs.ShowsModelSpecificQuotaUsage)
		v.QuotaText, v.QuotaLines = quotaText(lc, prefs, *q)
	}
	if c := importStatusCost(st.Cost); c != nil {
		v.Cost, v.DisplayedCost = c, c
		v.DisplayedRange = prefs.APICostSummaryRange
		d := DisplayCost(lc, *c, prefs.APICostSummaryRange, "", now)
		v.CostText, v.CostSubtitle, v.CostLines = d.Text, d.Subtitle, d.Lines
	}
	if st.Sessions != nil {
		v.RecentSessions = st.Sessions.Items
		v.RecentSessionLines = recentSessionLines(lc, st.Sessions.Items)
	}
	if p != nil {
		if len(p.DailyTokens) > 0 {
			tokens := map[string]int64{}
			for _, d := range p.DailyTokens {
				tokens[d.Date] = int64(d.Tokens)
			}
			if p.TokenSource == export.TokensAllDevices {
				v.Heatmap.AllDevices = tokens
			} else {
				v.Heatmap.Local = tokens
			}
			v.Heatmap.CalculatedAt = now
		}
		if p.ResetCredits != nil {
			c := codexapi.ResetCreditsSnapshot{AvailableCount: p.ResetCredits.AvailableCount, UpdatedAt: derefTime(p.UpdatedAt)}
			sum := SummarizeCredits(c)
			v.ResetCredits = &c
			v.ResetCreditSummary = &sum
			v.ResetCreditsText, v.ResetCreditLines = creditsText(lc, nil, c, sum)
		}
	}
	return v
}

func remoteGrokState(providers *export.StatusProviders, prefs store.Preferences, lc l10n.Localizer, now time.Time) GrokState {
	if providers == nil || providers.Grok == nil {
		return GrokState{Availability: GrokCLIUnavailable}
	}
	w := *providers.Grok
	g := GrokState{Widget: &w, Availability: grokAvailability(w.Availability), Meters: widgetMeters(lc, w.Quota, prefs, now)}
	if w.Plan != nil {
		g.Plan = *w.Plan
	}
	g.UpdatedAt = derefTime(w.UpdatedAt)
	return g
}

func remoteClaudeState(providers *export.StatusProviders, prefs store.Preferences, lc l10n.Localizer, now time.Time) ClaudeState {
	if providers == nil || providers.Claude == nil {
		return ClaudeState{Availability: ClaudeNotLoggedIn}
	}
	w := *providers.Claude
	c := ClaudeState{Widget: &w, Availability: claudeAvailability(w.Availability), Meters: widgetMeters(lc, w.Quota, prefs, now)}
	if w.Plan != nil {
		c.Plan = *w.Plan
	}
	c.UpdatedAt = derefTime(w.UpdatedAt)
	return c
}

func remoteResetToday(providers *export.StatusProviders, lc l10n.Localizer) ResetTodayState {
	if providers == nil || providers.ResetToday == nil {
		return ResetTodayState{}
	}
	r := *providers.ResetToday
	v := ResetTodayState{Widget: &r, HasData: true}
	switch r.State {
	case export.ResetYes:
		v.Text = lc.Text(l10n.KeyRateLimitResetTodayYes)
	case export.ResetNo:
		v.Text = lc.Text(l10n.KeyRateLimitResetTodayNo)
	default:
		v.Text = lc.Text(l10n.KeyRateLimitResetTodayUnknown)
	}
	v.Lines = []DetailLine{{lc.Text(l10n.KeyRateLimitResetTodayVerdict), v.Text}}
	return v
}

func grokAvailability(a export.Availability) GrokAvailability {
	switch a {
	case export.Available:
		return GrokReady
	case export.NotLoggedIn:
		return GrokNotLoggedIn
	case export.CLIUnavailable:
		return GrokCLIUnavailable
	}
	return GrokFailed
}

func claudeAvailability(a export.Availability) ClaudeAvailability {
	switch a {
	case export.Available:
		return ClaudeReady
	case export.NotLoggedIn:
		return ClaudeNotLoggedIn
	}
	return ClaudeFailed
}

// widgetMeters rebuilds the quota bars of a provider block, dropping
// model-specific rows the preferences hide.
func widgetMeters(lc l10n.Localizer, quota []export.WidgetQuota, prefs store.Preferences, now time.Time) []Meter {
	var meters []Meter
	for _, q := range quota {
		if q.Source == export.QuotaModelSpecific && !prefs.ShowsModelSpecificQuotaUsage {
			continue
		}
		source := SourceStandard
		if q.Source == export.QuotaModelSpecific {
			source = SourceModelSpecific
		}
		m := Meter{
			Title:            q.Title,
			UsedPercent:      q.UsedPercent,
			RemainingPercent: q.RemainingPercent,
			Source:           source,
			Health:           HealthForUsedPercent(q.UsedPercent),
			MarkerPercents:   DefaultMarkers(),
		}
		if q.WindowMinutes != nil {
			m.WindowMinutes = *q.WindowMinutes
		}
		if q.ResetsAt != nil {
			m.ResetsAt = *q.ResetsAt
			m.ResetText = DurationLocalized(m.ResetsAt.Sub(now).Seconds(), l10n.English, false)
		}
		if q.ValueText != nil {
			m.ValueText = *q.ValueText
			m.Balance = true
		}
		meters = append(meters, m)
	}
	return meters
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// importStatusQuota reverses statusQuota: "5-hour" and "Weekly" recover their
// window lengths; the status file carries no other length.
func importStatusQuota(q *export.StatusQuota) *codexapi.QuotaSnapshot {
	if q == nil {
		return nil
	}
	out := &codexapi.QuotaSnapshot{UpdatedAt: q.UpdatedAt, CreditsBalance: q.CreditsBalance}
	if q.Plan != nil {
		out.Plan = *q.Plan
	}
	out.Primary = importStatusWindow(q.Primary)
	if q.Secondary != nil {
		w := importStatusWindow(*q.Secondary)
		out.Secondary = &w
	}
	for _, a := range q.AdditionalWindows {
		out.AdditionalWindows = append(out.AdditionalWindows, codexapi.NamedRateWindow{Name: a.Name, Window: importStatusWindow(a)})
	}
	return out
}

func importStatusWindow(w export.StatusWindow) codexapi.RateWindow {
	out := codexapi.RateWindow{UsedPercent: w.UsedPercent, ResetsAt: derefTime(w.ResetsAt)}
	switch w.Name {
	case "5-hour":
		out.WindowMinutes = FiveHourMinutes
	case "Weekly":
		out.WindowMinutes = WeeklyMinutes
	}
	return out
}

// importStatusCost reverses exportCost.
func importStatusCost(c *export.CostSummary) *usage.Summary {
	if c == nil {
		return nil
	}
	out := &usage.Summary{
		Source:         usage.Source(c.Source),
		Confidence:     usage.Confidence(c.Confidence),
		Window:         usage.Window{Start: c.Window.Start, End: c.Window.Start.Add(time.Duration(c.Window.Duration * float64(time.Second)))},
		EstimatedUSD:   parseSwiftDecimal(c.EstimatedUSD),
		Totals:         importTotals(c.Totals),
		RawCredits:     c.RawCredits,
		Warnings:       append([]string{}, c.Warnings...),
		PricingVersion: c.PricingVersion,
		CalculatedAt:   c.CalculatedAt,
	}
	for _, r := range c.DailyRows {
		out.DailyRows = append(out.DailyRows, usage.DailyRow{
			Date: r.Date, Totals: importTotals(r.Totals), EstimatedUSD: parseSwiftDecimal(r.EstimatedUSD),
			RawCredits: r.RawCredits, CreditsReported: r.CreditsReported, TotalsReported: r.TotalsReported,
		})
	}
	out.ModelRows = importBreakdown(c.ModelRows)
	out.ProjectRows = importBreakdown(c.ProjectRows)
	out.ClientRows = importBreakdown(c.ClientRows)
	return out
}

func importBreakdown(rows []export.CostBreakdownRow) []usage.BreakdownRow {
	out := make([]usage.BreakdownRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, usage.BreakdownRow{
			Name: r.Name, Totals: importTotals(r.Totals), EstimatedUSD: parseSwiftDecimal(r.EstimatedUSD), RawCredits: r.RawCredits,
		})
	}
	return out
}

// importTotals reverses sessionTotals.
func importTotals(t sessions.Totals) usage.Totals {
	return usage.Totals{
		TotalTokens: int64(t.TotalTokens), UncachedInputTokens: int64(t.UncachedInputTokens),
		CachedInputTokens: int64(t.CachedInputTokens), OutputTokens: int64(t.OutputTokens),
		Turns: int64(t.Turns), Threads: int64(t.Threads),
	}
}

func parseSwiftDecimal(d *swiftjson.Decimal) *decimal.Decimal {
	if d == nil {
		return nil
	}
	v, err := decimal.Parse(d.String())
	if err != nil {
		return nil
	}
	return &v
}
