package app

import (
	"context"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/licoy/quosparr/internal/agy"
	"github.com/licoy/quosparr/internal/cursor"
	"github.com/licoy/quosparr/internal/kimi"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/netx"
	"github.com/licoy/quosparr/internal/platform"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/zai"
)

// runway.MeterProvider seam. The business state is fetched by a closure that
// re-detects the credential on every call; nothing is cached across
// refreshes. Locking follows the Grok bridge: Refresh is serialized by
// refreshMu, State only takes the short state lock, and onChange runs without
// it.
type meterModule struct {
	// fetch returns the provider's business state, re-reading credentials
	// and environment every call.
	fetch func(ctx context.Context) meter.State
	// detect reports whether a credential is currently present; it never
	// touches the network.
	detect func(ctx context.Context) bool
	// provider is the history key this module's pool samples are recorded
	// under.
	provider store.Provider
	// history persists pool samples between refreshes; nil disables
	// recording and rate analysis.
	history *meter.History
	// speedScan derives the provider's model-speed report from local
	// session stats; nil (cursor, agy) means the provider has none.
	speedScan func(ctx context.Context, now time.Time) (speed.Report, error)

	refreshMu sync.Mutex

	mu       sync.Mutex
	lc       l10n.Localizer
	raw      meter.State
	loaded   bool
	speed    *speed.Report
	speedErr string
	onChange func()
}

func newMeterModule(lc l10n.Localizer, fetch func(ctx context.Context) meter.State, detect func(ctx context.Context) bool) *meterModule {
	return &meterModule{fetch: fetch, detect: detect, lc: lc}
}

// State maps the cached business state to the runway view. It never calls
// back into the Service.
func (m *meterModule) State(now time.Time) runway.MeterState {
	m.mu.Lock()
	lc, raw, loaded := m.lc, m.raw, m.loaded
	speed, speedErr := m.speed, m.speedErr
	m.mu.Unlock()
	st := meterStateFromRaw(lc, raw, loaded, now)
	st.Speed, st.SpeedError = speed, speedErr
	if m.history != nil && st.Availability == runway.MeterReady {
		st.Rates = meterHistoryRates(currentLimitRates(raw.Limits, m.history.Rates(string(m.provider), now)))
	}
	return st
}

// Refresh refetches the quota. The credential is re-detected inside fetch,
// so environment changes (KIMI_SHARE_DIR, the zai store, ...) apply at once.
func (m *meterModule) Refresh(ctx context.Context) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	raw := m.fetch(ctx)
	m.mu.Lock()
	m.raw = raw
	m.loaded = true
	m.mu.Unlock()
	if m.history != nil && raw.Status == meter.StatusOK {
		_ = m.history.Record(string(m.provider), raw, time.Now())
	}
	// The speed scan is independent of the quota fetch, so it runs after
	// both success and failure.
	m.refreshSpeed(ctx)
	m.fire()
	return nil
}

// refreshSpeed runs the provider's speed scan (if any) and caches the result
// under the state lock. A scan error replaces any previous report — errors
// are transient and the next refresh retries; an empty report stores nil.
func (m *meterModule) refreshSpeed(ctx context.Context) {
	if m.speedScan == nil {
		return
	}
	rep, err := m.speedScan(ctx, time.Now())
	m.mu.Lock()
	switch {
	case err != nil:
		m.speed, m.speedErr = nil, err.Error()
	case rep.Empty():
		m.speed, m.speedErr = nil, ""
	default:
		r := rep
		m.speed, m.speedErr = &r, ""
	}
	m.mu.Unlock()
}

// RefreshLocal re-runs only the credential detection. A credential that
// disappeared downgrades the state to not-configured without a fetch; a
// credential that appeared keeps the previous state until the next Refresh.
func (m *meterModule) RefreshLocal(ctx context.Context) error {
	if m.detect == nil {
		return nil
	}
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if m.detect(ctx) {
		return nil
	}
	m.mu.Lock()
	downgrade := m.raw.Status != meter.StatusNotConfigured
	if downgrade {
		m.raw = meter.NotConfigured()
		m.loaded = true
	}
	m.mu.Unlock()
	if downgrade {
		m.fire()
	}
	return nil
}

// SetOnChange stores the callback; it is invoked after every state change
// without holding the state lock.
func (m *meterModule) SetOnChange(fn func()) {
	m.mu.Lock()
	m.onChange = fn
	m.mu.Unlock()
}

func (m *meterModule) fire() {
	m.mu.Lock()
	fn := m.onChange
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// meterStateFromRaw maps a business state to the runway view.
func meterStateFromRaw(lc l10n.Localizer, raw meter.State, loaded bool, now time.Time) runway.MeterState {
	switch {
	case !loaded:
		return runway.MeterState{Availability: runway.MeterLoading}
	case raw.Status == meter.StatusNotConfigured:
		return runway.MeterState{Availability: runway.MeterNotConfigured}
	case raw.Status != meter.StatusOK:
		st := runway.MeterState{Availability: runway.MeterError}
		if raw.Message != "" {
			st.Detail = []runway.DetailLine{{Value: raw.Message}}
		}
		return st
	}
	st := runway.MeterState{
		Availability: runway.MeterReady,
		Plan:         raw.Plan,
		Meters:       meterLimitMeters(lc, raw.Limits, now),
		FetchedAt:    raw.FetchedAt,
	}
	return st
}

// meterLimitMeters turns the normalized quota pools into panel meters.
// Percent pools carry a bar; amount pools carry their value as text.
func meterLimitMeters(lc l10n.Localizer, limits []meter.Limit, now time.Time) []runway.Meter {
	rows := make([]runway.Meter, 0, len(limits))
	for _, l := range limits {
		title := meterLimitTitle(lc, l)
		w := runway.MeterWindow{WindowMinutes: l.WindowMinutes, ResetsAt: l.ResetsAt}
		if l.HasPercent && !math.IsNaN(l.Percent) {
			w.UsedPercent = int(math.Round(l.Percent))
		}
		m := runway.NewMeter(title, w, now, nil, runway.SourceStandard)
		m.Group = l.Group
		m.GroupDescription = l.GroupDescription
		if remaining := 100 - l.Percent; l.HasPercent && remaining != math.Trunc(remaining) {
			m.RemainingCaption = meterAmount(remaining)
		}
		if text := meterValueText(l); text != "" {
			m.ValueText = text
		}
		rows = append(rows, m)
	}
	return rows
}

// meterLimitTitle keeps a pool's own identity when it has one and localizes
// bare window labels ("5h", "7d", "weekly", "monthly", "unknown", "<n>s/h/d")
// to the standard window name.
func meterLimitTitle(lc l10n.Localizer, l meter.Limit) string {
	if l.Title != "" && !bareWindowLabel(l.Title) {
		return l.Title
	}
	if l.WindowMinutes == 0 {
		if l.Title != "" {
			return l.Title
		}
		return lc.Text(l10n.KeyStatusUnknown)
	}
	return runway.WindowTitle(lc, l.WindowMinutes)
}

// bareWindowLabel reports whether a pool title is only a window name.
func bareWindowLabel(title string) bool {
	switch title {
	case "5h", "7d", "weekly", "monthly", "unknown":
		return true
	}
	digits := 0
	for _, r := range title {
		if r >= '0' && r <= '9' {
			digits++
			continue
		}
	}
	unit := strings.TrimLeft(title, "0123456789")
	return digits > 0 && (unit == "s" || unit == "h" || unit == "d" || unit == "mo")
}

// meterValueText is the "$used / $limit" caption of an amount pool; it is
// empty when the pool carries no numbers. Percent-only pools have no text.
func meterValueText(l meter.Limit) string {
	if math.IsNaN(l.Used) {
		return ""
	}
	if l.Unit == "usd" {
		if math.IsNaN(l.LimitValue) {
			return "$" + meterAmount(l.Used)
		}
		return "$" + meterAmount(l.Used) + " / $" + meterAmount(l.LimitValue)
	}
	if math.IsNaN(l.LimitValue) {
		return meterAmount(l.Used)
	}
	return meterAmount(l.Used) + " / " + meterAmount(l.LimitValue)
}

// meterAmount formats a quota number: integers without a decimal part,
// otherwise the shortest decimal rounded to two places (raw float math
// leaves artifacts like 91.00000000000009). Amounts are data, not UI copy.
func meterAmount(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// currentLimitRates keeps only the rate rows whose pool still exists in the
// current limits; history leftovers from pools that disappeared (for example
// after a window filter change) must not keep rendering.
func currentLimitRates(limits []meter.Limit, rs []meter.Rate) []meter.Rate {
	if len(rs) == 0 {
		return nil
	}
	ids := make(map[string]bool, len(limits))
	for _, l := range limits {
		ids[l.ID] = true
	}
	kept := make([]meter.Rate, 0, len(rs))
	for _, r := range rs {
		if ids[r.ID] {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// meterHistoryRates maps the derived pool rates to the runway mirror,
// field for field.
func meterHistoryRates(rs []meter.Rate) []runway.MeterRate {
	if len(rs) == 0 {
		return nil
	}
	rows := make([]runway.MeterRate, 0, len(rs))
	for _, r := range rs {
		rows = append(rows, runway.MeterRate{
			ID:         r.ID,
			Title:      r.Title,
			Unit:       r.Unit,
			IsPercent:  r.IsPercent,
			PerHour:    r.PerHour,
			HasRate:    r.HasRate,
			Samples:    r.Samples,
			From:       r.From,
			To:         r.To,
			ExhaustsAt: r.ExhaustsAt,
			HasExhaust: r.HasExhaust,
			ResetsAt:   r.ResetsAt,
			HasReset:   r.HasReset,
			AtReset:    r.AtReset,
			HasAtReset: r.HasAtReset,
		})
	}
	return rows
}

// newMeterModules builds the four quota-only provider modules and their
// Options.Meters map. Credentials and environment are re-read inside every
// fetch, so nothing is captured. The concrete modules are returned for the
// accounts page (configuration changes refresh them); the zai store and
// module serve the zai tab.
func newMeterModules(home string, prefs store.Preferences, langs []string) (map[store.Provider]runway.MeterProvider, map[store.Provider]*meterModule, *zai.Store, *meterModule) {
	lc := l10n.ForPreference(string(prefs.Language), langs)
	doer := func(ctx context.Context) meter.Doer {
		client, err := netx.ClientFor(ctx, netx.WithoutCookies)
		if err != nil {
			return nil
		}
		return meter.NoRedirectClient(client)
	}
	root := platform.RunwayDir(home)
	cursorMod := newMeterModule(lc,
		func(ctx context.Context) meter.State {
			token, ok, err := cursor.LoadConfig(root)
			if err != nil {
				return meter.Failed(meter.StatusParse, "cursor: read configuration failed")
			}
			if ok {
				cred, usable, terr := cursor.CredentialFromToken(token)
				if terr != nil || !usable {
					return meter.Failed(meter.StatusAuth, "cursor: configured token is invalid")
				}
				return cursor.Fetch(ctx, doer(ctx), cred, time.Now())
			}
			cred, ok, err := cursor.LoadCredential(ctx, cursor.StateDBPath(home))
			if err != nil {
				return meter.Failed(meter.StatusParse, "cursor: read credential store failed")
			}
			if !ok {
				return meter.NotConfigured()
			}
			return cursor.Fetch(ctx, doer(ctx), cred, time.Now())
		},
		func(ctx context.Context) bool {
			token, ok, err := cursor.LoadConfig(root)
			if err != nil {
				return false
			}
			if ok {
				_, usable, terr := cursor.CredentialFromToken(token)
				return terr == nil && usable
			}
			_, ok, err = cursor.LoadCredential(ctx, cursor.StateDBPath(home))
			return err == nil && ok
		})
	kimiMod := newMeterModule(lc,
		func(ctx context.Context) meter.State {
			key, err := kimiMeterKey(root)
			if err != nil {
				return meter.Failed(meter.StatusParse, "kimi: read configuration failed")
			}
			cred, ok, err := kimi.LoadCredential(key, kimi.CredentialsPath(os.Getenv("KIMI_SHARE_DIR"), home))
			if err != nil {
				return meter.Failed(meter.StatusParse, "kimi: read credentials failed")
			}
			if !ok {
				return meter.NotConfigured()
			}
			return kimi.Fetch(ctx, doer(ctx), cred, time.Now())
		},
		func(context.Context) bool {
			key, err := kimiMeterKey(root)
			if err != nil {
				return false
			}
			_, ok, err := kimi.LoadCredential(key, kimi.CredentialsPath(os.Getenv("KIMI_SHARE_DIR"), home))
			return err == nil && ok
		})
	kimiMod.speedScan = func(ctx context.Context, now time.Time) (speed.Report, error) {
		return meter.ScanOMPSpeed(ctx, filepath.Join(home, ".omp", "stats.db"), []string{"kimi-code"}, now)
	}
	zstore, err := zai.LoadStore(root)
	if err != nil {
		log.Println("zai accounts: load failed")
		zstore = nil
	}
	zaiCurrent := func() (key, region, scheme string, ok bool) {
		if zstore != nil {
			if id, err := zstore.Current(); err == nil && id != "" {
				if k, err := zstore.LoadKey(id); err == nil && k != "" {
					region, scheme = zai.RegionIntl, zai.SchemeRaw
					if rows, err := zstore.Rows(); err == nil {
						for _, r := range rows {
							if r.ID == id {
								region, scheme = r.Region, r.AuthScheme
								break
							}
						}
					}
					return k, region, scheme, true
				}
			}
		}
		if k := zai.EnvKey(); k != "" {
			return k, zai.RegionIntl, zai.SchemeRaw, true
		}
		return "", "", "", false
	}
	zaiMod := newMeterModule(lc,
		func(ctx context.Context) meter.State {
			key, region, scheme, ok := zaiCurrent()
			if !ok {
				return meter.NotConfigured()
			}
			return zai.Fetch(ctx, doer(ctx), key, region, scheme, time.Now())
		},
		func(context.Context) bool {
			_, _, _, ok := zaiCurrent()
			return ok
		})
	zaiMod.speedScan = func(ctx context.Context, now time.Time) (speed.Report, error) {
		return meter.ScanOMPSpeed(ctx, filepath.Join(home, ".omp", "stats.db"), []string{"zai"}, now)
	}
	agyMod := newMeterModule(lc,
		func(ctx context.Context) meter.State {
			d, found, err := agyMeterDiscovery(root, home)
			if err != nil {
				return meter.Failed(meter.StatusParse, "agy: read configuration failed")
			}
			if !found {
				return meter.NotConfigured()
			}
			return agy.Fetch(ctx, d.Bin, time.Now())
		},
		func(context.Context) bool {
			_, found, err := agyMeterDiscovery(root, home)
			return err == nil && found
		})
	mods := map[store.Provider]*meterModule{
		store.ProviderCursor: cursorMod,
		store.ProviderKimi:   kimiMod,
		store.ProviderZai:    zaiMod,
		store.ProviderAgy:    agyMod,
	}
	hist := meter.LoadHistory(filepath.Join(root, "meter-history.json"))
	meters := make(map[store.Provider]runway.MeterProvider, len(mods))
	for p, m := range mods {
		m.provider = p
		m.history = hist
		meters[p] = m
	}
	return meters, mods, zstore, zaiMod
}

// kimiMeterKey resolves the Kimi Code credential key: the explicitly
// configured API key wins over KIMI_CODE_API_KEY; "" when neither is set.
func kimiMeterKey(root string) (string, error) {
	key, ok, err := kimi.LoadConfig(root)
	if err != nil {
		return "", err
	}
	if ok {
		return key, nil
	}
	return os.Getenv("KIMI_CODE_API_KEY"), nil
}

// agyMeterDiscovery resolves the Antigravity CLI location: the explicit
// configuration's overrides win over auto-discovery.
func agyMeterDiscovery(root, home string) (agy.Discovery, bool, error) {
	cfg, ok, err := agy.LoadConfig(root)
	if err != nil {
		return agy.Discovery{}, false, err
	}
	if ok {
		d, found := agy.DetectIn(cfg.Bin, cfg.ProfileDir, home)
		return d, found, nil
	}
	d, found := agy.Detect("", home)
	return d, found, nil
}
