package runway

import (
	"slices"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

// maxFloatingMeters is how many quota windows the floating window shows for
// one provider.
const maxFloatingMeters = 2

// FloatingMeter is one quota window of the floating window.
type FloatingMeter struct {
	// Label is the short window name ("5h", "Weekly").
	Label            string
	RemainingPercent int
	Health           Health
	// Reset is the localized time left until the reset, "" without one.
	Reset string
	// ValueText is the balance amount shown instead of the percentage.
	ValueText string
}

// FloatingProvider is one provider of the floating window. Without meters,
// Status says why ("Log in", "Wait").
type FloatingProvider struct {
	Provider store.Provider
	Name     string
	Meters   []FloatingMeter
	Status   string
}

// FloatingView is the data side of the floating window.
type FloatingView struct {
	Providers []FloatingProvider
}

// BuildFloating computes the floating window content for the preferences.
func BuildFloating(lc l10n.Localizer, in StatusInputs) FloatingView {
	var v FloatingView
	for _, p := range in.Prefs.FloatingSelection() {
		item := FloatingProvider{Provider: p, Name: lc.Text(providerStatusKey(p))}
		meters := metersOf(in, p)
		if p == store.ProviderAgy {
			meters = agyFloatingMeters(meters)
		}
		for _, m := range floatingMeters(meters, preferredOf(p)) {
			fm := FloatingMeter{Label: meterTitle(lc, m), RemainingPercent: m.RemainingPercent, Health: m.Health, ValueText: m.ValueText}
			if m.HasReset() {
				fm.Reset = DurationLocalized(m.ResetsAt.Sub(in.Now).Seconds(), lc.Language(), false)
			}
			item.Meters = append(item.Meters, fm)
		}
		if len(item.Meters) == 0 {
			if b := in.Meters[p].BalanceText; b != "" {
				// A ready provider with only balances (deepseek) shows its
				// money instead of the unknown placeholder.
				item.Status = b
			} else {
				item.Status = floatingStatus(lc, in, p)
			}
		}
		v.Providers = append(v.Providers, item)
	}
	return v
}

// agyGeminiGroup is the Antigravity group the floating window follows: the
// CLI's native Gemini quota, exactly as the agy usage JSON names it.
const agyGeminiGroup = "Gemini Models"

// agyFloatingMeters keeps only the Gemini Models pools — the floating window
// shows the Gemini 5-hour and weekly windows, not the other-models group. A
// CLI output without that group keeps every pool so the window never goes
// blank.
func agyFloatingMeters(meters []Meter) []Meter {
	var gemini []Meter
	for _, m := range meters {
		if m.Group == agyGeminiGroup {
			gemini = append(gemini, m)
		}
	}
	if len(gemini) == 0 {
		return meters
	}
	return gemini
}

// floatingMeters picks up to two standard windows, the preferred lengths
// first; a provider with only model-specific windows shows its first one.
func floatingMeters(meters []Meter, preferred []int) []Meter {
	var standard []Meter
	for _, m := range meters {
		if m.Source == SourceStandard {
			standard = append(standard, m)
		}
	}
	if len(standard) == 0 {
		return meters[:min(1, len(meters))]
	}
	rank := func(m Meter) int {
		if i := slices.Index(preferred, m.WindowMinutes); i >= 0 {
			return i
		}
		return len(preferred)
	}
	slices.SortStableFunc(standard, func(a, b Meter) int { return rank(a) - rank(b) })
	return standard[:min(maxFloatingMeters, len(standard))]
}

// floatingStatus is the provider's status text when it has no reading. The
// Codex text may be a countdown, which says nothing without a meter.
func floatingStatus(lc l10n.Localizer, in StatusInputs, p store.Provider) string {
	if p == store.ProviderCodex && len(in.CodexMeters) == 0 && in.CodexText == "" {
		return lc.Text(l10n.KeyStatusWait)
	}
	return statusTextFor(lc, in, p)
}
