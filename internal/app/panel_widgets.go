package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
)

// toolButton is a borderless icon button with the neutral hover wash of the
// Swift header and section controls. A disabled one reports no clicks.
func toolButton(c *ui.Context, icon *ui.SVG, label string, w, h, size float32, disabled bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Disabled(disabled).Size(w, h).Radius(radiusControl).AlignItems(ui.Center).Justify(ui.Center)
	hovered := b.Hovered() && !disabled
	if hovered {
		b.Background(hoverNeutral(t)).Cursor(ui.CursorPointer)
	}
	col := inkSecondary(t)
	switch {
	case disabled:
		col = col.Alpha(0.5)
	case hovered:
		col = t.Text
	}
	return b.Children(func() { ui.Icon(c, icon).Size(size, size).TextColor(col) })
}

// sectionSpec describes the typographic header of a main-panel section.
type sectionSpec struct {
	title      string
	caption    string
	refreshing bool
	refresh    func()
	info       *sectionInfo
	// trailing draws extra controls before the info and refresh buttons.
	trailing func()
	// peak marks the header of a provider whose peak pricing window is
	// active right now: the title gains weight and takes the danger color.
	peak bool
}

// sectionInfo is the (i) button of a header and the popover it opens.
type sectionInfo struct {
	id, help string
	body     func()
	// icon is the button glyph; nil is the exclamation circle of the Swift
	// section headers (the token usage header uses the info circle).
	icon *ui.SVG
}

// drawSectionHead is RefreshableSectionHeader: title, caption, info, refresh.
func drawSectionHead(c *ui.Context, l l10n.Localizer, st *panelState, h sectionSpec) {
	t := c.Theme()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		title := ui.Text(c, h.title).FontSize(fsHeadline).FontWeight(600).Grow(1).Basis(0)
		if h.peak {
			title.FontWeight(700).TextColor(t.Danger)
		}
		if h.caption != "" {
			ui.Text(c, h.caption).FontSize(fsCaption).TextColor(inkSecondary(t)).SingleLine()
		}
		if h.trailing != nil {
			h.trailing()
		}
		if h.info != nil {
			infoButton(c, st, *h.info)
		}
		refreshButton(c, l, h.refreshing, h.refresh)
	})
}

// refreshButton is the 24pt refresh control; it shows a spinner while busy.
func refreshButton(c *ui.Context, l l10n.Localizer, busy bool, refresh func()) {
	if busy {
		ui.Row(c).Size(24, 24).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Spinner(c).Size(14, 14)
		})
		return
	}
	if toolButton(c, icRefresh, l.Text(l10n.KeyRefresh), 24, 24, 13, refresh == nil).Clicked() && refresh != nil {
		refresh()
	}
}

// infoButton is a header button whose popover explains the section.
func infoButton(c *ui.Context, st *panelState, in sectionInfo) {
	open := st.info == in.id
	icon := in.icon
	if icon == nil {
		icon = icWarnCircle
	}
	anchor := toolButton(c, icon, in.help, 24, 24, 13, false)
	if anchor.Clicked() {
		st.info, open = in.id, true
	}
	panelPopover(c, anchor, &open, func() {
		ui.Column(c).Gap(14).Width(280).Padding(4).Children(in.body)
	})
	if !open && st.info == in.id {
		st.info = ""
	}
}

// panelPopover is ui.Popover over a dimmed panel. The theme's popover shares
// the panel's background (clear under native material), so it blends into
// the content behind it; this one has an opaque raised surface instead. The
// scrim passes presses through, so a click outside still closes the popover.
func panelPopover(c *ui.Context, anchor ui.Element, open *bool, fn func()) {
	if !*open {
		return
	}
	t := c.Theme()
	scrim := ui.RGBA(0, 0, 0, 0.18)
	if t.Dark {
		scrim = ui.RGBA(0, 0, 0, 0.45)
	}
	ui.Overlay(c, func() {
		ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).PassThrough().Role(ui.RoleNone).Background(scrim)
	})
	ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).Radius(t.Radius+2).Background(pal(t).Inset).Border(1, pal(t).Line).
			Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.3))
		fn()
	})
}

// popoverBody is the title / text / OK layout of the Swift info popovers.
func popoverBody(c *ui.Context, l l10n.Localizer, st *panelState, title string, paragraphs ...string) func() {
	t := c.Theme()
	return func() {
		ui.Text(c, title).FontSize(fsTitle3).FontWeight(600)
		for _, p := range paragraphs {
			ui.Text(c, p).FontSize(fsCallout).TextColor(inkSecondary(t))
		}
		ui.Row(c).Children(func() {
			ui.Spacer(c).Grow(1)
			if PillButton(c, nil, l.Text(l10n.KeyOk), false).Clicked() {
				st.info = ""
			}
		})
	}
}

// disclosureRow is SidePanelDisclosureRow: a raised row with an optional
// leading icon, a title and a chevron. It returns the button for Clicked.
func disclosureRow(c *ui.Context, icon *ui.SVG, title string, chevron bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(title).Padding(8, 10).Gap(8).Radius(radiusRow).AlignItems(ui.Center).Justify(ui.Start).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(hoverNeutral(t))
	} else {
		b.Background(surfaceRaised(t))
	}
	if t.Dark {
		b.Border(1, hairlineColor(t))
	}
	return b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(13, 13).TextColor(inkSecondary(t))
		}
		ui.Text(c, title).FontSize(fsCallout).FontWeight(500).Grow(1).Basis(0).SingleLine()
		if chevron {
			col := inkSecondary(t)
			if b.Hovered() {
				col = t.Text
			}
			ui.Icon(c, icChevRight).Size(12, 12).TextColor(col)
		}
	})
}

// statCard is RunwayStatCard: a dot and a caption over a mono-digit value.
func statCard(c *ui.Context, title, value string, dot ui.Color) {
	t := c.Theme()
	cardColumn(c, surfaceRaisedKind, radiusCard).Grow(1).Basis(0).Gap(4).Padding(10).Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(dot)
			ui.Text(c, title).FontSize(fsCaption).TextColor(inkSecondary(t)).SingleLine()
		})
		ui.Text(c, value).FontSize(fsBody).FontWeight(600).FontFeatures("tnum").SingleLine()
	})
}

// statGrid lays stat cards out two per row.
func statGrid(c *ui.Context, cards ...func()) {
	ui.Column(c).Gap(8).Children(func() {
		for i := 0; i < len(cards); i += 2 {
			ui.Row(c).Gap(8).Children(func() {
				cards[i]()
				if i+1 < len(cards) {
					cards[i+1]()
				} else {
					ui.Box(c).Grow(1).Basis(0)
				}
			})
		}
	})
}

// tableContainer is RunwayTableContainer: a sunken card clipping a header
// band over the rows.
func tableContainer(c *ui.Context, header, rows func()) {
	t := c.Theme()
	cardColumn(c, surfaceSunkenKind, radiusCard).Clip().Children(func() {
		ui.Row(c).Background(surfaceTableHead(t)).Children(header)
		rows()
	})
}

// tableRowRule is the inset separator above a table row (not the first).
func tableRowRule(c *ui.Context, first bool) {
	if first {
		return
	}
	ui.Row(c).Padding(0, 10).Children(func() { hairlineFaint(c).Grow(1) })
}

// pageSummaryRow is RunwayPageSummaryRow: title and meta, figure trailing.
func pageSummaryRow(c *ui.Context, title, meta, figure string) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Gap(3).Grow(1).Basis(0).Children(func() {
			ui.Text(c, title).FontSize(fsHeadline).FontWeight(600)
			if meta != "" {
				ui.Text(c, meta).FontSize(fsCaption).TextColor(inkTertiary(t)).MaxLines(2)
			}
		})
		ui.Text(c, figure).FontSize(fsTitle3).FontWeight(600).FontFeatures("tnum").SingleLine()
	})
}

// dimText is a caption-size secondary text line.
func dimText(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(fsCaption).TextColor(inkSecondary(c.Theme()))
}

// calloutDim is secondary callout text, as "Not loaded".
func calloutDim(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(fsCallout).TextColor(inkSecondary(c.Theme()))
}

// underlineIf underlines e when on; Element.Underline takes no flag.
func underlineIf(e ui.Element, on bool) ui.Element {
	if on {
		return e.Underline()
	}
	return e
}
