package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/store"
)

// quotaOnlyProvider reports whether p is one of the quota-only providers
// (cursor, kimi, zai, deepseek, agy, minimax, pixellab, elevenlabs): they
// render one quota section and have no local scan modules, so the panel's
// local-scan logic must not treat them as Grok or Claude.
func quotaOnlyProvider(p store.Provider) bool {
	switch p {
	case store.ProviderCursor, store.ProviderKimi, store.ProviderZai, store.ProviderDeepSeek,
		store.ProviderAgy, store.ProviderMiniMax, store.ProviderPixelLab, store.ProviderElevenLabs:
		return true
	}
	return false
}

// panelMeterQuota is the quota section of a quota-only provider: the plan,
// the meter rows and the usage detail, or the empty state (not configured,
// loading, error). In remote mode the state arrives from the remote path the
// same way, so the renderer is shared.
func panelMeterQuota(c *ui.Context, l l10n.Localizer, snap runway.Snapshot, provider store.Provider, acts PanelActions, first *bool, st *panelState, rs resetSwitch) {
	ms := snap.Meters[provider]
	refreshing := sectionRefreshing(snap, runway.SectionQuota)
	spec := sectionSpec{title: l.Text(providerKey(provider)), refreshing: refreshing, refresh: sectionRefresh(acts, runway.SectionQuota)}
	sectionBlock(c, first, func() {
		drawSectionHead(c, l, st, spec)
		switch ms.Availability {
		case runway.MeterReady:
			panelMeterReady(c, l, ms, provider, rs)
		case runway.MeterError:
			if msg := meterErrorText(ms); msg != "" {
				calloutDim(c, msg)
			} else {
				sectionEmptyKey(c, l, refreshing, l10n.KeyMeterNotConfigured)
			}
		case runway.MeterLoading:
			sectionEmpty(c, l, true)
		default:
			sectionEmptyKey(c, l, refreshing, l10n.KeyMeterNotConfigured)
		}
	})
}

// meterErrorText is the failure message of an error state; the business
// packages already redacted it.
func meterErrorText(ms runway.MeterState) string {
	for _, d := range ms.Detail {
		if d.Value != "" {
			return d.Value
		}
	}
	return ""
}

func panelMeterReady(c *ui.Context, l l10n.Localizer, ms runway.MeterState, provider store.Provider, rs resetSwitch) {
	if len(ms.Meters) == 0 && len(ms.Detail) == 0 && ms.Plan == "" {
		sectionEmpty(c, l, false)
		return
	}
	ui.Column(c).Gap(12).Children(func() {
		if ms.Plan != "" {
			ui.Text(c, ms.Plan).FontSize(fsCaption).FontWeight(600)
		}
		now := time.Now()
		// Kimi, Z.ai, Antigravity and MiniMax show only headline pools, so
		// every row is a primary quota row with its reset line; the other
		// quota-only providers keep Grok's one-primary-plus-slim-rows layout.
		primaryRows := provider == store.ProviderKimi || provider == store.ProviderZai || provider == store.ProviderAgy || provider == store.ProviderMiniMax
		lastGroup := ""
		for i, m := range ms.Meters {
			if m.Group != "" && m.Group != lastGroup {
				lastGroup = m.Group
				ui.Text(c, m.Group).FontSize(fsCaption).FontWeight(600)
				if m.GroupDescription != "" {
					dimText(c, m.GroupDescription)
				}
			}
			grokMeterRow(c, l, m, primaryRows || i == 0, now, rs)
		}
		panelMeterDetail(c, ms.Detail)
		panelMeterRates(c, l, ms, now)
		panelMeterSpeed(c, l, ms)
	})
}

// panelMeterSpeed renders the compact token-speed block of the quota-only
// providers that measure it (kimi, zai): one terse row per model group plus
// the source note, or just the sanitized scan error. Providers without speed
// data (Speed nil, no error) render nothing.
func panelMeterSpeed(c *ui.Context, l l10n.Localizer, ms runway.MeterState) {
	if ms.Speed != nil && !ms.Speed.Empty() {
		ui.Text(c, tx(l, xSpeedTitle)).FontSize(fsCaption).FontWeight(600)
		for _, g := range ms.Speed.Groups[:min(speedCardRows, len(ms.Speed.Groups))] {
			meterSpeedRow(c, l, g)
		}
		dimText(c, tx(l, xMeterSpeedSource))
	}
	speedError(c, ms.SpeedError)
}

// meterSpeedRow is one model at one effort of the compact speed block: name
// and effort, the TTFT when measured, the output speed and the call count.
func meterSpeedRow(c *ui.Context, l l10n.Localizer, s speed.Stat) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		title := speedModelName(l, s.Model)
		if s.Effort != "" {
			title += " " + s.Effort
		}
		ui.Text(c, title).FontSize(fsCaption).FontWeight(600).Grow(1).Basis(0).SingleLine()
		if s.TTFTP50 > 0 {
			ui.Text(c, speedTTFT(s.TTFTP50, s.Calls)).FontSize(fsCaption).FontFeatures("tnum")
		}
		ui.Text(c, speedRate(s, true)).FontSize(fsCaption).FontFeatures("tnum")
		ui.Text(c, fmt.Sprintf("%d", s.Calls)).FontSize(fsCaption).FontFeatures("tnum")
	})
}

// panelMeterDetail renders the usage summary rows below the meters.
func panelMeterDetail(c *ui.Context, lines []runway.DetailLine) {
	for _, d := range lines {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if d.Title != "" {
				ui.Text(c, d.Title).FontSize(fsCaption).FontWeight(600)
			}
			ui.Text(c, d.Value).FontSize(fsCaption).FontFeatures("tnum")
		})
	}
}

// panelMeterRates renders the rate-analysis block below the pools and usage
// detail: one terse row per pool with a usable consumption rate, or a muted
// hint while samples accumulate. Rates arrive the same way for local and
// remote states, so the renderer is shared.
func panelMeterRates(c *ui.Context, l l10n.Localizer, ms runway.MeterState, now time.Time) {
	usable := 0
	for _, r := range ms.Rates {
		if rateRowUsable(r) {
			usable++
		}
	}
	if usable == 0 {
		if len(ms.Meters) > 0 {
			dimText(c, tx(l, xMeterRateEmpty))
		}
		return
	}
	ui.Text(c, tx(l, xMeterRateAnalysis)).FontSize(fsCaption).FontWeight(600)
	for _, r := range ms.Rates {
		if !rateRowUsable(r) {
			continue
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if r.Title != "" {
				ui.Text(c, r.Title).FontSize(fsCaption).FontWeight(600)
			}
			ui.Text(c, meterRateText(l, r, now)).FontSize(fsCaption).FontFeatures("tnum")
		})
	}
}

// rateRowUsable reports whether a rate row carries a rate worth rendering:
// rates under 0.005/h would only print "≈ 0..." noise.
func rateRowUsable(r runway.MeterRate) bool {
	return r.HasRate && r.PerHour >= 0.005
}

// meterRateText composes one pool's rate row: the per-hour consumption and,
// when available, the exhaust and at-reset projections. Rates and durations
// are data; the projection labels are localized.
func meterRateText(l l10n.Localizer, r runway.MeterRate, now time.Time) string {
	parts := []string{"≈ " + meterRateAmount(r.PerHour, r.IsPercent, r.Unit) + "/h"}
	if r.HasExhaust {
		parts = append(parts, tx(l, xMeterRateExhausts, meterRateShortDuration(r.ExhaustsAt.Sub(now))))
	}
	if r.HasAtReset {
		parts = append(parts, tx(l, xMeterRateAtReset, meterRateAmount(r.AtReset, r.IsPercent, r.Unit)))
	}
	return strings.Join(parts, " · ")
}

// meterRateAmount formats a rate or projected value like its pool: "$x" for
// usd, "x%" for percent pools, "x <unit>" otherwise.
func meterRateAmount(v float64, isPercent bool, unit string) string {
	switch {
	case isPercent:
		return meterAmount(v) + "%"
	case unit == "usd":
		return "$" + meterAmount(v)
	case unit != "" && unit != "unknown":
		return meterAmount(v) + " " + unit
	}
	return meterAmount(v)
}

// meterRateShortDuration renders a projection horizon as compact data
// ("3h20m"), rounded to the nearest minute; anything under a minute counts
// as "0m".
func meterRateShortDuration(d time.Duration) string {
	if d < time.Minute {
		return "0m"
	}
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}
