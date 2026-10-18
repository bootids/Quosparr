package app

import (
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/grok"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
)

const grokInstallGuideURL = "https://docs.x.ai/build/overview"

// panelGrokQuota is GrokDashboardView: the included quota rows, the billing
// details behind the info button, or the empty state of the CLI.
func panelGrokQuota(c *ui.Context, l l10n.Localizer, snap runway.Snapshot, acts PanelActions, first *bool, st *panelState, rs resetSwitch) {
	g := snap.Grok
	refreshing := sectionRefreshing(snap, runway.SectionQuota)
	spec := sectionSpec{title: l.Text(l10n.KeyGrokIncludedQuota), refreshing: refreshing, refresh: sectionRefresh(acts, runway.SectionQuota)}
	if len(g.Meters) > 0 {
		spec.info = &sectionInfo{id: "grok-billing", help: l.Text(l10n.KeyGrokBillingPeriod), body: grokBillingBody(c, l, st, g)}
	}
	sectionBlock(c, first, func() {
		drawSectionHead(c, l, st, spec)
		if len(g.Meters) == 0 {
			if refreshing && g.Availability == runway.GrokLoading {
				sectionEmpty(c, l, true)
			} else {
				grokEmptyState(c, l, g)
			}
			return
		}
		ui.Column(c).Gap(12).Children(func() {
			now := time.Now()
			for i, m := range g.Meters {
				grokMeterRow(c, l, m, i == 0, now, rs)
			}
		})
	})
}

// grokMeterRow is GrokQuotaMeterRow: the overall row is a quota row, the
// per-product rows are slim used-share bars.
func grokMeterRow(c *ui.Context, l l10n.Localizer, m runway.Meter, primary bool, now time.Time, rs resetSwitch) {
	if primary {
		t := c.Theme()
		ui.Column(c).Gap(6).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				percent := l10n.Format("%d%% %s", m.RemainingPercent, l.Text(l10n.KeyLeft))
				if m.RemainingCaption != "" {
					percent = l10n.Format("%s%% %s", m.RemainingCaption, l.Text(l10n.KeyLeft))
				}
				ui.Text(c, m.Title).FontSize(fsCallout).FontWeight(600).Grow(1).Basis(0)
				ui.Text(c, percent).FontSize(fsCallout).FontWeight(600).FontFeatures("tnum").TextColor(healthInk(t, m.Health)).SingleLine()
			})
			quotaBar(c, m)
			if m.HasReset() {
				ui.Row(c).Children(func() {
					ui.Spacer(c).Grow(1)
					resetLine(c, l, "reset-grok", m.ResetsAt, now, rs)
				})
			} else if m.UsedPercent == 0 && m.RemainingPercent == 100 {
				ui.Row(c).Children(func() {
					ui.Spacer(c).Grow(1)
					dimText(c, tx(l, xMeterQuotaAvailable))
				})
			}
		})
		return
	}
	t := c.Theme()
	ui.Column(c).Gap(6).Padding(0, 0, 0, 8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.Title).FontSize(fsCaption).FontWeight(600).TextColor(inkSecondary(t)).Grow(1).Basis(0)
			ui.Text(c, l10n.Format("%d%% %s", m.UsedPercent, l.Text(l10n.KeyUsed))).FontSize(fsCaption).FontWeight(600).FontFeatures("tnum").TextColor(healthInk(t, m.Health)).SingleLine()
		})
		usedBar(c, m)
	})
}

// grokEmptyState explains why there is no quota: the status in a sunken card.
func grokEmptyState(c *ui.Context, l l10n.Localizer, g runway.GrokState) {
	t := c.Theme()
	title, detail, icon, tint := grokEmptyCopy(l, g)
	cardColumn(c, surfaceSunkenKind, radiusCard).Gap(7).Padding(12).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(13, 13).TextColor(tint(t))
			ui.Text(c, title).FontSize(fsCallout).FontWeight(600).TextColor(tint(t))
		})
		if detail != "" {
			dimText(c, detail)
		}
		if g.Availability == runway.GrokCLIUnavailable {
			if linkButton(c, l.Text(l10n.KeyGrokCLIOpenInstallGuide)).Clicked() {
				c.OpenURL(grokInstallGuideURL)
			}
		}
	})
}

// linkButton is a caption-size link in the brand color.
func linkButton(c *ui.Context, title string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(title).Cursor(ui.CursorPointer).AlignItems(ui.Center)
	ink := brandInk(t, t.Accent)
	if b.Pressed() {
		ink = brandInk(t, t.AccentPressed)
	} else if b.Hovered() {
		ink = brandInk(t, t.AccentHover)
	}
	return b.Children(func() {
		underlineIf(ui.Text(c, title).FontSize(fsCaption).TextColor(ink).SingleLine(), b.Hovered())
	})
}

func grokEmptyCopy(l l10n.Localizer, g runway.GrokState) (title, detail string, icon *ui.SVG, tint func(*ui.Theme) ui.Color) {
	secondary := func(t *ui.Theme) ui.Color { return inkSecondary(t) }
	warn := func(t *ui.Theme) ui.Color { return sys(t, sysOrange) }
	switch g.Availability {
	case runway.GrokLoading:
		return l.Text(l10n.KeyGrokRefreshing), "", icRefresh, secondary
	case runway.GrokReady:
		return l.Text(l10n.KeyGrokNoQuotaData), "", icUsers, secondary
	case runway.GrokNotLoggedIn:
		return l.Text(l10n.KeyGrokNotLoggedIn), "", icUsers, secondary
	case runway.GrokReauthRequired:
		return l.Text(l10n.KeyGrokReauthenticationRequired), "", icKey, warn
	case runway.GrokCLIUnavailable:
		return l.Text(l10n.KeyGrokCLIUnavailable), l.Text(l10n.KeyGrokCLIInstallHint), icTerminal, secondary
	case runway.GrokCLITooOld:
		return l.Text(l10n.KeyGrokCLITooOld), "", icTerminal, secondary
	case runway.GrokBillingParseFailed:
		return l.Text(l10n.KeyGrokBillingParseFailed), "", icAlert, warn
	}
	return l.Text(l10n.KeyGrokRefreshFailed), g.LastError, icAlert, warn
}

// grokBillingBody is the billing details popover (GrokBillingDetailsSheet).
func grokBillingBody(c *ui.Context, l l10n.Localizer, st *panelState, g runway.GrokState) func() {
	t := c.Theme()
	lines := grokBillingLines(l, g)
	return func() {
		ui.Text(c, l.Text(l10n.KeyGrokBillingPeriod)).FontSize(fsTitle3).FontWeight(600)
		cardColumn(c, surfaceSunkenKind, radiusCard).Children(func() {
			for i, line := range lines {
				tableRowRule(c, i == 0)
				ui.Row(c).Gap(10).Padding(7, 10).Children(func() {
					ui.Text(c, line.Title).FontSize(fsCallout).TextColor(inkSecondary(t)).Grow(1).Basis(0)
					ui.Text(c, line.Value).FontSize(fsCallout).FontFeatures("tnum").TextAlign(ui.End)
				})
			}
		})
		ui.Row(c).Children(func() {
			ui.Spacer(c).Grow(1)
			if PillButton(c, nil, l.Text(l10n.KeyOk), false).Clicked() {
				st.info = ""
			}
		})
	}
}

// grokBillingLines are the rows of GrokQuotaPresentation.lines.
func grokBillingLines(l l10n.Localizer, g runway.GrokState) []runway.DetailLine {
	snap, _ := g.Detail.(*grok.BillingSnapshot)
	if snap == nil {
		return nil
	}
	plan := l.Text(l10n.KeyUnknown)
	if snap.Plan != nil && *snap.Plan != "" {
		plan = grok.TierResolve(*snap.Plan).DisplayName()
	}
	title := l.Text(l10n.KeyGrokIncludedQuota)
	if snap.Period != nil {
		title = periodTitle(l, snap.Period.Kind)
	}
	used := max(0, min(100, int(snap.IncludedUsagePercent+0.5)))
	lines := []runway.DetailLine{
		{Title: l.Text(l10n.KeyGrokPlan), Value: plan},
		{Title: title, Value: l10n.Format("%d%% %s", used, l.Text(l10n.KeyUsed))},
	}
	add := func(key l10n.Key, value string) {
		lines = append(lines, runway.DetailLine{Title: l.Text(key), Value: value})
	}
	if snap.IncludedLimitCents != nil && *snap.IncludedLimitCents > 0 {
		spent := int64(0)
		if snap.IncludedUsedCents != nil {
			spent = max(0, *snap.IncludedUsedCents)
		}
		add(l10n.KeyGrokIncludedUSD, grokMoney(spent)+" / "+grokMoney(*snap.IncludedLimitCents))
	}
	if snap.Period != nil && snap.Period.ResetsAt != nil {
		add(l10n.KeyGrokResetAt, snap.Period.ResetsAt.Local().Format("2006-01-02 15:04"))
	}
	if snap.PrepaidBalanceCents != nil {
		add(l10n.KeyGrokPrepaidBalance, grokMoney(*snap.PrepaidBalanceCents))
	}
	if text := grokOnDemandText(snap); text != "" {
		add(l10n.KeyGrokOnDemandUsage, text)
	}
	if snap.IsUnifiedBillingUser != nil && *snap.IsUnifiedBillingUser {
		add(l10n.KeyGrokUnifiedBilling, l.Text(l10n.KeyAvailable))
	}
	add(l10n.KeyGrokDataSource, l.Text(l10n.KeyGrokSourceCLI))
	if !g.UpdatedAt.IsZero() {
		add(l10n.KeyGrokUpdatedAt, g.UpdatedAt.Local().Format("2006-01-02 15:04"))
	}
	return lines
}

func grokOnDemandText(s *grok.BillingSnapshot) string {
	if s.OnDemandEnabled == nil || !*s.OnDemandEnabled || s.OnDemandUsedCents == nil {
		return ""
	}
	if s.OnDemandLimitCents != nil {
		return grokMoney(*s.OnDemandUsedCents) + " / " + grokMoney(*s.OnDemandLimitCents)
	}
	return grokMoney(*s.OnDemandUsedCents)
}

// grokMoney is "$12.34" from cents, with the sign in front.
func grokMoney(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	frac := strconv.FormatInt(cents%100, 10)
	if len(frac) < 2 {
		frac = "0" + frac
	}
	return sign + "$" + strconv.FormatInt(cents/100, 10) + "." + frac
}
