package runway

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/licoy/quosparr/internal/alerts"
	"github.com/licoy/quosparr/internal/codex"
	"github.com/licoy/quosparr/internal/codexapi"
	"github.com/licoy/quosparr/internal/estimate"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/export/swiftjson"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/sessions"
	"github.com/licoy/quosparr/internal/speed"
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
	// OnEvent receives deduplicated alerts, as the local Service's events.
	OnEvent func(Event)
	// AlertsPath is the alert dedupe store; alerts are not delivered without it.
	AlertsPath string
	// Now replaces the clock; the default is time.Now.
	Now func() time.Time
}

// RemoteService is the data side of the remote view mode: it polls a
// RemoteFetcher, decodes the exported status file and rebuilds the same
// Snapshot the UI renders locally. All methods are safe from any goroutine.
type RemoteService struct {
	fetcher RemoteFetcher
	nowFn   func() time.Time
	onSnap  func(Snapshot)

	// prefs, lc and interval are mutable through SetPreferences; mu guards
	// them, so every reader copies them under the lock.
	prefs      store.Preferences
	sysLangs   []string
	lc         l10n.Localizer
	interval   time.Duration
	wake       chan struct{}
	onEvent    func(Event)
	alertsPath string

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
	rawSt   export.StatusSnapshot
	haveRaw bool
	good    Snapshot
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
	ctx, cancel := context.WithCancel(context.Background())
	return &RemoteService{
		fetcher:    opts.Fetcher,
		prefs:      opts.Preferences,
		sysLangs:   opts.SystemLanguages,
		lc:         l10n.ForPreference(string(opts.Preferences.Language), opts.SystemLanguages),
		nowFn:      opts.Now,
		onSnap:     opts.OnSnapshot,
		onEvent:    opts.OnEvent,
		alertsPath: opts.AlertsPath,
		interval:   remoteInterval(opts.Preferences),
		root:       ctx,
		cancel:     cancel,
		pubSig:     make(chan struct{}, 1),
		wake:       make(chan struct{}, 1),
	}
}

func remoteInterval(p store.Preferences) time.Duration {
	interval := time.Duration(p.RefreshIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Duration(store.DefaultRefreshIntervalSeconds) * time.Second
	}
	return interval
}

// SetPreferences updates the language, the refresh interval and the rendering
// preferences; the next fetch cycle picks them up. The fetcher's host
// configuration keeps needing a restart, as before.
func (s *RemoteService) SetPreferences(p store.Preferences) {
	s.mu.Lock()
	s.prefs = p
	s.lc = l10n.ForPreference(string(p.Language), s.sysLangs)
	s.interval = remoteInterval(p)
	s.mu.Unlock()
	// Wake the poll loop so a shorter interval (or a re-render under a new
	// language) applies at once instead of after the previous sleep.
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *RemoteService) currentInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interval
}

// RawStatus returns the status file of the last successful decode; the zero
// value before the first one.
func (s *RemoteService) RawStatus() export.StatusSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rawSt
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
	for {
		// Re-read the interval every cycle so SetPreferences takes effect
		// without a restart.
		t := time.NewTimer(s.currentInterval())
		select {
		case <-s.root.Done():
			t.Stop()
			return
		case <-ctx.Done():
			t.Stop()
			return
		case <-s.wake:
			// Preferences changed: drop the old sleep and refetch at once.
			t.Stop()
		case <-t.C:
		}
		s.fetchOnce(ctx)
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
	decoded := false
	data, err := s.fetcher.FetchStatus(ctx)
	if err == nil {
		var st export.StatusSnapshot
		if derr := json.Unmarshal(data, &st); derr != nil {
			err = errRemoteDecode
		} else {
			s.mu.Lock()
			s.st, s.rawSt, s.haveRaw = st, st, true
			s.mu.Unlock()
			decoded = true
		}
	}
	if err != nil {
		s.mu.Lock()
		lc := s.lc
		s.mu.Unlock()
		category := l10n.KeyRemoteSourceErrorFetch
		if err == errRemoteDecode {
			category = l10n.KeyRemoteSourceErrorDecode
		}
		errText = l10n.Format(lc.Text(l10n.KeyRemoteSourceUnavailable), lc.Text(category))
	}
	s.mu.Lock()
	s.errText = errText
	s.mu.Unlock()
	s.setRefreshing(false)
	if decoded {
		s.deliverRemoteAlerts()
	}
}

func (s *RemoteService) setRefreshing(on bool) {
	s.mu.Lock()
	st, errText := s.st, s.errText
	prefs, lc := s.prefs, s.lc
	now := s.now()
	var snap Snapshot
	if errText != "" {
		if s.haveRaw {
			// Keep the last good data; only the error line changes.
			snap = s.good
			snap.GeneratedAt = now
			snap.Codex.LastError = errText
			snap.Grok.LastError = errText
			snap.Claude.LastError = errText
		} else {
			snap = remoteErrorSnapshot(errText, prefs, lc, now)
		}
	} else {
		snap = BuildRemoteSnapshot(st, prefs, lc, now)
		s.good = snap
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

// deliverRemoteAlerts builds the alert candidates of the last decoded status
// with the current preferences and hands the deduplicated new ones to OnEvent,
// like the local Service's deliver.
func (s *RemoteService) deliverRemoteAlerts() {
	if s.alertsPath == "" {
		return
	}
	s.mu.Lock()
	st, prefs := s.st, s.prefs
	s.mu.Unlock()
	candidates := remoteAlertCandidates(st, prefs)
	if len(candidates) == 0 {
		return
	}
	unseen, err := (&alerts.Store{Path: s.alertsPath}).Unseen(candidates)
	if err != nil {
		s.mu.Lock()
		s.errText = err.Error()
		s.mu.Unlock()
		s.setRefreshing(false)
		return
	}
	if len(unseen) > 0 && s.onEvent != nil {
		s.onEvent(Event{Kind: EventAlerts, Alerts: unseen})
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
	var panel *export.StatusPanel
	if st.Panel != nil {
		panel = st.Panel
	}
	if panel != nil && panel.Codex != nil {
		snap.Accounts = remoteAccountsView(panel.Codex)
	}
	snap.Codex = remoteCodexView(st, codex, panelCodexBlock(panel), prefs, lc, now)
	var grokPB, claudePB *export.ProviderPanelBlock
	if panel != nil {
		grokPB, claudePB = panel.Grok, panel.Claude
	}
	snap.Grok = remoteGrokState(providers, grokPB, prefs, lc, now)
	snap.Claude = remoteClaudeState(providers, claudePB, prefs, lc, now)
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

func panelCodexBlock(panel *export.StatusPanel) *export.CodexPanelBlock {
	if panel == nil {
		return nil
	}
	return panel.Codex
}

func remoteCodexView(st export.StatusSnapshot, p *export.ProviderSnapshot, pb *export.CodexPanelBlock, prefs store.Preferences, lc l10n.Localizer, now time.Time) CodexView {
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
	if p != nil && pb == nil {
		if len(p.DailyTokens) > 0 {
			tokens := dailyTokensMap(p.DailyTokens)
			if p.TokenSource == export.TokensAllDevices {
				v.Heatmap.AllDevices = tokens
			} else {
				v.Heatmap.Local = tokens
			}
			v.Heatmap.CalculatedAt = now
		}
	}
	if p != nil && p.ResetCredits != nil && (pb == nil || pb.ResetCredits == nil) {
		c := codexapi.ResetCreditsSnapshot{AvailableCount: p.ResetCredits.AvailableCount, UpdatedAt: derefTime(p.UpdatedAt)}
		sum := SummarizeCredits(c)
		v.ResetCredits = &c
		v.ResetCreditSummary = &sum
		v.ResetCreditsText, v.ResetCreditLines = creditsText(lc, nil, c, sum)
	}
	if pb != nil {
		v.APIKeyMode, v.APIBaseURL, v.APIBalance = pb.APIKeyMode, pb.APIBaseURL, pb.APIBalance
		if len(pb.AllDevicesTokens) > 0 {
			v.Heatmap.AllDevices = dailyTokensMap(pb.AllDevicesTokens)
		}
		if len(pb.LocalTokens) > 0 {
			v.Heatmap.Local = dailyTokensMap(pb.LocalTokens)
		}
		if len(pb.AllDevicesTokens) > 0 || len(pb.LocalTokens) > 0 {
			v.Heatmap.CalculatedAt = now
			if pb.TokensAsOf != nil {
				v.Heatmap.OfficialStatsAsOf = estimate.UTCDay(*pb.TokensAsOf)
			}
			v.Heatmap.OfficialGeneratedAt = derefTime(pb.TokensGenerated)
		}
		if pb.Estimate != nil {
			v.Estimate = importPanelEstimate(pb.Estimate)
			v.EstimateError = pb.Estimate.Error
		}
		if c := importPanelResetCredits(pb.ResetCredits); c != nil {
			sum := SummarizeCredits(*c)
			v.ResetCredits, v.ResetCreditSummary = c, &sum
			v.ResetCreditsText, v.ResetCreditLines = creditsText(lc, nil, *c, sum)
			v.CreditDetails = CreditDetails(lc, *c)
		}
		v.Speed = importPanelSpeed(pb.Speed)
	}
	return v
}

func remoteGrokState(providers *export.StatusProviders, pb *export.ProviderPanelBlock, prefs store.Preferences, lc l10n.Localizer, now time.Time) GrokState {
	if providers == nil || providers.Grok == nil {
		return GrokState{Availability: GrokCLIUnavailable}
	}
	w := *providers.Grok
	g := GrokState{Widget: &w, Availability: grokAvailability(w.Availability), Meters: widgetMeters(lc, w.Quota, prefs, now)}
	if w.Plan != nil {
		g.Plan = *w.Plan
	}
	g.UpdatedAt = derefTime(w.UpdatedAt)
	if pb == nil {
		return g
	}
	g.Cost = importStatusCost(pb.Cost)
	if len(pb.LocalTokens) > 0 {
		g.Heatmap.Local = dailyTokensMap(pb.LocalTokens)
		g.Heatmap.CalculatedAt = now
	}
	g.RecentSessions = pb.RecentSessions
	g.Speed = importPanelSpeed(pb.Speed)
	if c := importPanelResetCredits(pb.ResetCredits); c != nil {
		sum := SummarizeCredits(*c)
		g.ResetCredits, g.ResetCreditSummary = c, &sum
	}
	g.Billing = pb.Billing
	g.Accounts = remoteProviderAccounts(pb.Accounts, pb.ActiveAccountID)
	for _, q := range pb.AccountQuotas {
		if g.AccountQuotas == nil {
			g.AccountQuotas = map[string]GrokAccountQuota{}
		}
		g.AccountQuotas[q.ID] = GrokAccountQuota{
			Plan: q.Plan, Title: remoteQuotaTitle(lc, q.Plan, q.PeriodKind), UsedPercent: q.UsedPercent,
			ResetsAt: derefTime(q.ResetsAt), IncludedUSD: q.IncludedUSD, UpdatedAt: q.UpdatedAt,
		}
	}
	return g
}

func remoteClaudeState(providers *export.StatusProviders, pb *export.ProviderPanelBlock, prefs store.Preferences, lc l10n.Localizer, now time.Time) ClaudeState {
	if providers == nil || providers.Claude == nil {
		return ClaudeState{Availability: ClaudeNotLoggedIn}
	}
	w := *providers.Claude
	c := ClaudeState{Widget: &w, Availability: claudeAvailability(w.Availability), Meters: widgetMeters(lc, w.Quota, prefs, now)}
	if w.Plan != nil {
		c.Plan = *w.Plan
	}
	c.UpdatedAt = derefTime(w.UpdatedAt)
	if pb == nil {
		return c
	}
	c.Cost = importStatusCost(pb.Cost)
	if len(pb.LocalTokens) > 0 {
		c.Heatmap.Local = dailyTokensMap(pb.LocalTokens)
		c.Heatmap.CalculatedAt = now
	}
	c.RecentSessions = pb.RecentSessions
	c.Speed = importPanelSpeed(pb.Speed)
	if r := importPanelResetCredits(pb.ResetCredits); r != nil {
		sum := SummarizeCredits(*r)
		c.ResetCredits, c.ResetCreditSummary = r, &sum
	}
	c.ExtraUsage = pb.ExtraUsage
	if pb.API != nil {
		c.API = &ClaudeAPIView{BaseURL: pb.API.BaseURL, Quota: pb.API.Quota, ErrorCode: pb.API.ErrorCode}
	}
	c.Accounts = remoteProviderAccounts(pb.Accounts, pb.ActiveAccountID)
	for _, q := range pb.AccountQuotas {
		if c.AccountQuotas == nil {
			c.AccountQuotas = map[string]ClaudeAccountQuota{}
		}
		c.AccountQuotas[q.ID] = ClaudeAccountQuota{
			Plan: q.Plan, Title: remoteQuotaTitle(lc, q.Plan, q.PeriodKind), UsedPercent: q.UsedPercent,
			ResetsAt: derefTime(q.ResetsAt), ExtraUsage: q.ExtraUsage, Balance: q.Balance, UpdatedAt: q.UpdatedAt,
		}
	}
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

func dailyTokensMap(rows []export.DailyTokens) map[string]int64 {
	tokens := map[string]int64{}
	for _, d := range rows {
		tokens[d.Date] = int64(d.Tokens)
	}
	return tokens
}

// remoteAccountsView rebuilds the Codex managed-account list from the panel
// block; the email never travels in the file, so it stays empty.
func remoteAccountsView(b *export.CodexPanelBlock) AccountsView {
	v := remoteProviderAccounts(b.Accounts, b.ActiveAccountID)
	if b.Display != nil {
		v.Display = codex.AccountDisplay{Authenticated: b.Display.Authenticated, DisplayName: b.Display.DisplayName}
	}
	return v
}

func remoteProviderAccounts(accounts []export.PanelAccount, activeID string) AccountsView {
	v := AccountsView{ActiveID: activeID}
	for _, a := range accounts {
		v.Sidebar = append(v.Sidebar, importPanelAccount(a))
	}
	slices.SortStableFunc(v.Sidebar, func(x, y codex.ManagedAccount) int {
		return cmp.Compare(x.SortIndex, y.SortIndex)
	})
	return v
}

func importPanelAccount(a export.PanelAccount) codex.ManagedAccount {
	return codex.ManagedAccount{
		ID: a.ID, SortIndex: a.SortIndex, AuthMode: codex.AuthMode(a.AuthMode),
		Username: a.Username, UserID: a.UserID, SubjectID: a.SubjectID, AccountID: a.AccountID,
		WorkspaceName: a.WorkspaceName, DisplayName: a.DisplayName, Alias: a.Alias, Note: a.Note,
		PlanType: a.PlanType, SubscriptionExpiresAt: a.SubscriptionExpiresAt, CreatedAt: a.CreatedAt,
		LastUsedAt: a.LastUsedAt, LastQuotaAt: a.LastQuotaAt, RequiresReauth: a.RequiresReauth,
		APIBaseURL: a.APIBaseURL, APIModel: a.APIModel, APIQuota: a.APIQuota,
		CachedQuota: importPanelAccountQuota(a.Quota),
	}
}

func importPanelAccountQuota(q *export.PanelAccountQuotaLine) *codex.CachedAccountQuota {
	if q == nil {
		return nil
	}
	out := &codex.CachedAccountQuota{
		Plan: q.Plan, PrimaryUsedPercent: q.PrimaryUsedPercent, PrimaryResetsAt: q.PrimaryResetsAt,
		PrimaryWindowMinutes: q.PrimaryWindowMinutes, SecondaryUsedPercent: q.SecondaryUsedPercent,
		SecondaryResetsAt: q.SecondaryResetsAt, SecondaryWindowMinutes: q.SecondaryWindowMinutes,
		UpdatedAt: q.UpdatedAt,
	}
	for _, m := range q.AdditionalMeters {
		out.AdditionalMeters = append(out.AdditionalMeters, codex.CachedQuotaMeterRow{
			ID: m.ID, WindowMinutes: m.WindowMinutes, Name: m.Name, UsedPercent: m.UsedPercent,
		})
	}
	return out
}

func importPanelEstimate(e *export.PanelEstimate) *estimate.Snapshot {
	out := &estimate.Snapshot{
		WindowMode:       estimate.ParseWindowMode(e.WindowMode),
		CycleStartDate:   e.CycleStartDate,
		UsedPercent:      e.UsedPercent,
		UsedCredits:      e.UsedCredits,
		UsedUSD:          e.UsedUSD,
		DayCount:         e.DayCount,
		EstimatedCredits: e.EstimatedCredits,
		EstimatedUSD:     e.EstimatedUSD,
		CanExtrapolate:   e.CanExtrapolate,
		HasWeeklyWindow:  e.HasWeeklyWindow,
		PreviousEstimate: e.PreviousEstimate,
		ChangePercent:    e.ChangePercent,
		History:          e.History,
		CalculatedAt:     e.CalculatedAt,
	}
	if e.ChangeKind != "" {
		k := estimate.ChangeKind(e.ChangeKind)
		out.ChangeKind = &k
	}
	if e.UnavailableReason != "" {
		u := estimate.UnavailableReason(e.UnavailableReason)
		out.UnavailableReason = &u
	}
	for _, r := range e.CurrentRows {
		out.CurrentRows = append(out.CurrentRows, estimate.Row{
			Date: r.Date, Credits: r.Credits, Tokens: r.Tokens, Turns: r.Turns, USD: r.USD,
			CreditsReported: r.CreditsReported, TotalsReported: r.TotalsReported,
		})
	}
	return out
}

func importPanelResetCredits(r *export.PanelResetCredits) *codexapi.ResetCreditsSnapshot {
	if r == nil {
		return nil
	}
	out := &codexapi.ResetCreditsSnapshot{AvailableCount: r.AvailableCount, UpdatedAt: r.UpdatedAt}
	for _, c := range r.Credits {
		out.Credits = append(out.Credits, codexapi.ResetCredit{
			ID: c.ID, Status: c.Status, CreatedAt: c.CreatedAt, ExpiresAt: c.ExpiresAt,
			RemainingSeconds: c.RemainingSeconds,
		})
	}
	return out
}

func importPanelSpeed(r *export.PanelSpeedReport) *speed.Report {
	if r == nil {
		return nil
	}
	out := &speed.Report{NoTTFT: r.NoTTFT, Tiered: r.Tiered, From: r.From, To: r.To, CalculatedAt: r.CalculatedAt}
	out.Groups = importPanelSpeedStats(r.Groups)
	for _, s := range r.Sessions {
		out.Sessions = append(out.Sessions, speed.SessionStat{
			ID: s.ID, Title: s.Title, ProjectName: s.ProjectName, UpdatedAt: s.UpdatedAt,
			Groups: importPanelSpeedStats(s.Groups),
		})
	}
	return out
}

func importPanelSpeedStats(rows []export.PanelSpeedStat) []speed.Stat {
	out := make([]speed.Stat, 0, len(rows))
	for _, g := range rows {
		out = append(out, speed.Stat{
			Model: g.Model, Effort: g.Effort, Tier: speed.Tier(g.Tier), Calls: g.Calls,
			TTFTP50: time.Duration(g.TTFTP50NS), TTFTP90: time.Duration(g.TTFTP90NS),
			OutputTokens: g.OutputTokens, Decode: time.Duration(g.DecodeNS),
		})
	}
	return out
}

// remoteQuotaTitle rebuilds the localized period title of an account quota
// line from its period kind; the plan name is the fallback.
func remoteQuotaTitle(lc l10n.Localizer, plan, periodKind string) string {
	switch periodKind {
	case "weekly":
		return lc.Text(l10n.KeyGrokBillingPeriodWeekly)
	case "monthly":
		return lc.Text(l10n.KeyGrokBillingPeriodMonthly)
	}
	return plan
}
