package app

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

// quotaOnlyProvider reports whether p is one of the quota-only providers
// (cursor, kimi, zai, agy): they render one quota section and have no local
// scan modules, so the panel's local-scan logic must not treat them as
// Grok or Claude.
func quotaOnlyProvider(p store.Provider) bool {
	switch p {
	case store.ProviderCursor, store.ProviderKimi, store.ProviderZai, store.ProviderAgy:
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
			panelMeterReady(c, l, ms, rs)
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

func panelMeterReady(c *ui.Context, l l10n.Localizer, ms runway.MeterState, rs resetSwitch) {
	if len(ms.Meters) == 0 && len(ms.Detail) == 0 && ms.Plan == "" {
		sectionEmpty(c, l, false)
		return
	}
	ui.Column(c).Gap(12).Children(func() {
		if ms.Plan != "" {
			ui.Text(c, ms.Plan).FontSize(fsCaption).FontWeight(600)
		}
		now := time.Now()
		for i, m := range ms.Meters {
			grokMeterRow(c, l, m, i == 0, now, rs)
		}
		panelMeterDetail(c, ms.Detail)
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
