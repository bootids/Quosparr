package app

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
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
	// label returns the account label shown beside the plan (zai).
	label func() string

	refreshMu sync.Mutex

	mu       sync.Mutex
	lc       l10n.Localizer
	raw      meter.State
	loaded   bool
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
	m.mu.Unlock()
	st := meterStateFromRaw(lc, raw, loaded, now)
	if m.label != nil && st.Availability != runway.MeterNotConfigured {
		if label := m.label(); label != "" {
			st.Detail = append([]runway.DetailLine{{Value: label}}, st.Detail...)
		}
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
	m.fire()
	return nil
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
	if raw.Usage != nil {
		st.Detail = append(st.Detail, meterUsageLines(raw.Usage)...)
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
// otherwise the shortest exact decimal. Amounts are data, not UI copy.
func meterAmount(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// meterUsageLines renders a fixed-lookback usage summary (zai model usage)
// as plain detail rows; the numbers are data, the labels neutral.
func meterUsageLines(u *meter.Usage) []runway.DetailLine {
	lines := make([]runway.DetailLine, 0, len(u.Models)+len(u.Features)+2)
	for _, m := range u.Models {
		lines = append(lines, runway.DetailLine{Value: fmt.Sprintf("%s: %s", m.Name, meterAmount(m.Tokens))})
	}
	if u.TotalCalls > 0 {
		lines = append(lines, runway.DetailLine{Value: "calls: " + meterAmount(u.TotalCalls)})
	}
	if u.TotalTokens > 0 {
		lines = append(lines, runway.DetailLine{Value: "tokens: " + meterAmount(u.TotalTokens)})
	}
	for _, f := range u.Features {
		lines = append(lines, runway.DetailLine{Value: fmt.Sprintf("%s: %s", f.ID, meterAmount(f.Used))})
	}
	return lines
}

// newMeterModules builds the four quota-only provider modules and their
// Options.Meters map. Credentials and environment are re-read inside every
// fetch, so nothing is captured. The zai store and module are returned for
// the accounts page.
func newMeterModules(home string, prefs store.Preferences, langs []string) (map[store.Provider]runway.MeterProvider, *zai.Store, *meterModule) {
	lc := l10n.ForPreference(string(prefs.Language), langs)
	doer := func(ctx context.Context) meter.Doer {
		client, err := netx.ClientFor(ctx, netx.WithoutCookies)
		if err != nil {
			return nil
		}
		return meter.NoRedirectClient(client)
	}
	cursorMod := newMeterModule(lc,
		func(ctx context.Context) meter.State {
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
			_, ok, err := cursor.LoadCredential(ctx, cursor.StateDBPath(home))
			return err == nil && ok
		})
	kimiMod := newMeterModule(lc,
		func(ctx context.Context) meter.State {
			cred, ok, err := kimi.LoadCredential(os.Getenv("KIMI_CODE_API_KEY"), kimi.CredentialsPath(os.Getenv("KIMI_SHARE_DIR"), home))
			if err != nil {
				return meter.Failed(meter.StatusParse, "kimi: read credentials failed")
			}
			if !ok {
				return meter.NotConfigured()
			}
			return kimi.Fetch(ctx, doer(ctx), cred, time.Now())
		},
		func(context.Context) bool {
			_, ok, err := kimi.LoadCredential(os.Getenv("KIMI_CODE_API_KEY"), kimi.CredentialsPath(os.Getenv("KIMI_SHARE_DIR"), home))
			return err == nil && ok
		})
	zstore, err := zai.LoadStore(platform.RunwayDir(home))
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
	zaiMod.label = func() string {
		if zstore == nil {
			return ""
		}
		id, err := zstore.Current()
		if err != nil || id == "" {
			return ""
		}
		rows, err := zstore.Rows()
		if err != nil {
			return ""
		}
		for _, r := range rows {
			if r.ID == id {
				if r.Label != "" {
					return r.Label
				}
				return r.KeyHint
			}
		}
		return ""
	}
	agyMod := newMeterModule(lc,
		func(ctx context.Context) meter.State {
			d, ok := agy.Detect("", home)
			if !ok {
				return meter.NotConfigured()
			}
			return agy.Fetch(ctx, d.Bin, time.Now())
		},
		func(context.Context) bool {
			_, ok := agy.Detect("", home)
			return ok
		})
	meters := map[store.Provider]runway.MeterProvider{
		store.ProviderCursor: cursorMod,
		store.ProviderKimi:   kimiMod,
		store.ProviderZai:    zaiMod,
		store.ProviderAgy:    agyMod,
	}
	return meters, zstore, zaiMod
}
