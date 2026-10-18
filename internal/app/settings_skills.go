package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/skills"
	"github.com/licoy/quosparr/internal/store"
)

// skillFileRows is how many files a skill page lists before "show all".
const skillFileRows = 12

// skillsPage is the settings skills tab: a tab per CLI and the trash.
func (u *settingsUI) skillsPage(c *ui.Context) {
	s := u.skillsState()
	acts := u.skillActions()
	u.syncSkills(acts)
	ui.Column(c).Gap(14).Children(func() {
		if s.tab != skillsTrashTab && s.open != "" {
			if sum, sk, ok := s.find(s.open); ok {
				u.skillPage(c, sum, sk, acts)
				return
			}
			if s.result != nil && !s.task.Running {
				s.closeItem() // gone after a rescan
			}
		}
		u.skillsTabs(c, acts)
		if s.tab == skillsTrashTab {
			u.skillsTrash(c, acts)
			return
		}
		u.skillsList(c, acts)
	})
}

// skillsPageKey keeps one scroll position per tab and skill page.
func (u *settingsUI) skillsPageKey() string {
	if u.skills == nil {
		return ""
	}
	return fmt.Sprintf("-%d-%s", u.skills.tab, u.skills.open)
}

func (u *settingsUI) skillsTabs(c *ui.Context, acts SkillActions) {
	s := u.skillsState()
	l, t := u.L(), c.Theme()
	ps := skillsCLIProviders()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		seg := ui.SegmentedBase(c.Key("skills-tabs"), &s.tab, len(ps)+1)
		seg.Track.Padding(3).Gap(2).Radius(7).Background(t.Text.Alpha(0.06)).Children(func() {
			for i := 0; i <= len(ps); i++ {
				label, count := tx(l, xSkillsTrash), ""
				if i < len(ps) {
					label = l.Text(providerKey(ps[i]))
					if sum, ok := s.result[ps[i]]; ok {
						count = fmt.Sprint(len(sum.Skills))
					}
				} else if n := len(s.trash.list); n > 0 {
					count = fmt.Sprint(n)
				}
				sg := seg.Segment(i).Padding(5, 10).Gap(6).Radius(5).Label(label).AlignItems(ui.Center)
				col := t.TextMuted
				if i == s.tab {
					col = t.Text
					sg.Background(pal(t).Inset).Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.12))
				}
				sg.Children(func() {
					if i < len(ps) {
						ProviderIcon(c, ps[i], 15)
					} else {
						ui.Icon(c, icTrash).Size(14, 14).TextColor(col)
					}
					ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(col).SingleLine()
					if count != "" {
						ui.Text(c, count).FontSize(11).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine()
					}
				})
			}
		})
		if seg.Track.Changed() {
			s.closeItem()
		}
		ui.Spacer(c).Grow(1)
		busy := s.task.Running || s.work.Running || s.trash.task.Running
		if busy {
			ui.Spinner(c).Size(14, 14)
		}
		if IconButton(c, icRefresh, l.Text(l10n.KeyRefresh), busy || acts.Query == nil).Clicked() {
			u.startSkillsScan(acts)
			u.loadSkillTrash(acts)
		}
	})
}

// ---- Provider list ----

func (u *settingsUI) skillsList(c *ui.Context, acts SkillActions) {
	s := u.skillsState()
	l, t := u.L(), c.Theme()
	sum, ok := s.result[s.provider()]
	switch {
	case !ok && s.task.Err != nil:
		ui.Text(c, tx(l, xSkillsScanFailed)).FontSize(12).TextColor(t.Danger)
		return
	case !ok:
		skillsLoading(c)
		return
	}
	f := &s.filter[s.tab]
	u.skillsOverview(c, sum, f)
	u.skillsToolbar(c, sum, f)
	u.skillsNotice(c)
	list := filterSkills(sum, *f)
	switch {
	case len(sum.Skills) == 0:
		skillsEmptyBox(c, tx(l, xSkillsEmpty))
	case len(list) == 0:
		skillsEmptyBox(c, tx(l, xSkillsNoMatch))
	case f.sort == skillSortCLI:
		u.skillGroups(c, sum, list, acts)
	default:
		Panel(c).Children(func() {
			for i, sk := range list {
				settingsRule(c, i)
				u.skillListRow(c, sum, sk, acts)
			}
		})
	}
	u.skillsIssues(c, sum, acts)
	u.skillsRoots(c, sum, acts)
}

func skillsLoading(c *ui.Context) {
	ui.Row(c).Height(80).Justify(ui.Center).AlignItems(ui.Center).Children(func() { ui.Spinner(c).Size(16, 16) })
}

func skillsEmptyBox(c *ui.Context, text string) {
	Panel(c).Padding(18, 12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, text).FontSize(12).TextColor(c.Theme().TextMuted)
	})
}

func settingsRule(c *ui.Context, i int) {
	if i > 0 {
		ui.Box(c).Height(1).Background(pal(c.Theme()).Line)
	}
}

// skillsOverview is the row of state counts, which doubles as the state
// filter, and a line of the other totals.
func (u *settingsUI) skillsOverview(c *ui.Context, sum skills.Summary, f *skillFilter) {
	l, t := u.L(), c.Theme()
	counts := skillStateCounts(sum, *f)
	states := []skills.State{"", skills.StateOn, skills.StateOff, skills.StateOverridden, skills.StateNotLoaded}
	ui.Row(c).Gap(8).Children(func() {
		for _, st := range states {
			selected := f.state == st
			name := skillStateName(l, st)
			b := ui.ButtonBase(c.Key("skills-state-"+string(st))).Label(name).Role(ui.RoleTab).Selected(selected).
				Grow(1).Basis(0).Padding(8, 10).Gap(2).Radius(10).BorderWidth(1).Cursor(ui.CursorPointer)
			col := skillStateColor(t, st)
			switch {
			case selected:
				b.Background(col.Alpha(0.12)).BorderColor(col.Alpha(0.55))
			case b.Hovered():
				b.Background(pal(t).Hover).BorderColor(pal(t).Line)
			default:
				b.Background(pal(t).Card).BorderColor(pal(t).Line)
			}
			b.Children(func() {
				ui.Column(c).Gap(1).Grow(1).Basis(0).Children(func() {
					ui.Text(c, fmt.Sprint(counts[st])).FontSize(18).FontWeight(650).FontFeatures("tnum").TextColor(col).SingleLine()
					ui.Text(c, name).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
				})
			})
			if b.Clicked() {
				f.state = st
			}
		}
	})
	var shared, links, github int
	for _, sk := range sum.Skills {
		if len(sk.SharedWith) > 0 {
			shared++
		}
		if sk.LinkPath != "" {
			links++
		}
		if sk.Origin.GitHub != "" {
			github++
		}
	}
	parts := []string{
		tx(l, xSkillsFlagShared) + " " + fmt.Sprint(shared),
		tx(l, xSkillsFlagLink) + " " + fmt.Sprint(links),
		tx(l, xSkillsFlagGitHub) + " " + fmt.Sprint(github),
	}
	if n := len(sum.Issues); n > 0 {
		parts = append(parts, tx(l, xSkillsIssueCount, n))
	}
	if !sum.ScannedAt.IsZero() {
		parts = append(parts, tx(l, xSkillsScannedAt, sum.ScannedAt.Local().Format("15:04:05")))
	}
	ui.Text(c, strings.Join(parts, " · ")).FontSize(11).FontFeatures("tnum").TextColor(t.TextMuted).Padding(0, 4)
}

func skillStateName(l l10n.Localizer, st skills.State) string {
	switch st {
	case skills.StateOn:
		return tx(l, xSkillsStateOn)
	case skills.StateOff:
		return tx(l, xSkillsStateOff)
	case skills.StateOverridden:
		return tx(l, xSkillsStateOverridden)
	case skills.StateNotLoaded:
		return tx(l, xSkillsStateNotLoaded)
	}
	return tx(l, xSkillsAll)
}

func skillStateColor(t *ui.Theme, st skills.State) ui.Color {
	switch st {
	case skills.StateOn:
		return t.Success
	case skills.StateOff:
		return t.Warning
	case skills.StateOverridden, skills.StateNotLoaded:
		return t.TextMuted
	}
	return t.Accent
}

// skillsToolbar is the search, location, filter and order controls, and
// the chips of the active narrowing.
func (u *settingsUI) skillsToolbar(c *ui.Context, sum skills.Summary, f *skillFilter) {
	l, t := u.L(), c.Theme()
	key := fmt.Sprint(u.skillsState().tab)
	sources := skillSources(l, sum)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.TextInput(c.Key("skills-search-"+key), &f.search).Placeholder(tx(l, xSkillsSearchMore)).Label(tx(l, xSkillsSearch)).Grow(1).Basis(0)
		keys := []string{""}
		for _, src := range sources {
			keys = append(keys, src.key)
		}
		if v, ok := choose(c, "skills-source-"+key, tx(l, xSkillsSource), f.source, keys, func(k string) string {
			if k == "" {
				return tx(l, xSkillsSourceAll)
			}
			for _, src := range sources {
				if src.key == k {
					return fmt.Sprintf("%s (%d)", src.title, src.count)
				}
			}
			return k
		}, false); ok {
			f.source = v
		}
		label := tx(l, xSkillsFilter)
		if n := skillFlagCount(f.flags); n > 0 {
			label = fmt.Sprintf("%s (%d)", label, n)
		}
		PillButton(c, icFilterSliders, label, false).Menu(func(m *ui.Menu) {
			for _, flag := range skillFlags {
				if m.Item(skillFlagName(l, flag)).Checked(f.flags&flag != 0).Chosen() {
					f.flags ^= flag
				}
			}
			m.Separator()
			if m.Item(tx(l, xSkillsClearFilters)).Disabled(!f.narrowed()).Chosen() {
				f.source, f.flags = "", 0
			}
		})
		if v, ok := choose(c, "skills-sort-"+key, tx(l, xSkillsSort), f.sort, skillSorts, func(v skillSort) string { return skillSortLabel(l, v) }, false); ok {
			f.sort = v
		}
	})
	if !f.narrowed() && f.state == "" && strings.TrimSpace(f.search) == "" {
		return
	}
	shown := len(filterSkills(sum, *f))
	ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
		ui.Text(c, tx(l, xSkillsShowing, shown, len(sum.Skills))).FontSize(11).FontFeatures("tnum").TextColor(t.TextMuted)
		if f.source != "" {
			for _, src := range sources {
				if src.key == f.source && skillChip(c, "chip-src", src.title) {
					f.source = ""
				}
			}
		}
		for _, flag := range skillFlags {
			if f.flags&flag != 0 && skillChip(c, fmt.Sprint("chip-", int(flag)), skillFlagName(l, flag)) {
				f.flags &^= flag
			}
		}
		if f.narrowed() || f.state != "" || f.search != "" {
			b := ui.ButtonBase(c.Key("chip-clear")).Label(tx(l, xSkillsClearFilters)).Padding(2, 6).Radius(5).Cursor(ui.CursorPointer)
			b.Children(func() { ui.Text(c, tx(l, xSkillsClearFilters)).FontSize(11).TextColor(t.Accent) })
			if b.Clicked() {
				f.source, f.flags, f.search, f.state = "", 0, "", ""
			}
		}
	})
}

// skillChip is an active filter with a close mark; it reports a click.
func skillChip(c *ui.Context, key, text string) bool {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Label(text).Padding(2, 6, 2, 8).Gap(4).Radius(10).AlignItems(ui.Center).
		Background(t.Accent.Alpha(0.12)).Cursor(ui.CursorPointer)
	b.Children(func() {
		ui.Text(c, text).FontSize(11).TextColor(t.Accent).SingleLine()
		ui.Icon(c, icClose).Size(10, 10).TextColor(t.Accent)
	})
	return b.Clicked()
}

func skillFlagCount(f skillFlag) int {
	n := 0
	for _, flag := range skillFlags {
		if f&flag != 0 {
			n++
		}
	}
	return n
}

func skillFlagName(l l10n.Localizer, f skillFlag) string {
	return tx(l, map[skillFlag]textKey{
		skillFlagShared:     xSkillsFlagShared,
		skillFlagLink:       xSkillsFlagLink,
		skillFlagGitHub:     xSkillsFlagGitHub,
		skillFlagIssues:     xSkillsFlagIssues,
		skillFlagDeletable:  xSkillsFlagDeletable,
		skillFlagSwitchable: xSkillsFlagSwitchable,
	}[f])
}

func skillSortLabel(l l10n.Localizer, v skillSort) string {
	switch v {
	case skillSortName:
		return tx(l, xSkillsSortName)
	case skillSortModified:
		return tx(l, xSkillsSortModified)
	}
	return tx(l, xSkillsSortCLI)
}

func (u *settingsUI) skillsNotice(c *ui.Context) {
	s := u.skillsState()
	if s.notice == "" {
		return
	}
	t := c.Theme()
	col, icon := t.Success, icCheckCircle
	if s.noticeErr {
		col, icon = t.Danger, icWarnCircle
	}
	ui.Row(c).Gap(8).Padding(8, 10).Radius(8).Background(col.Alpha(0.10)).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).Size(13, 13).TextColor(col)
		ui.Text(c, s.notice).FontSize(12).TextColor(col).Grow(1).Basis(0)
		b := ui.ButtonBase(c.Key("skills-notice-close")).Label(u.text(l10n.KeyOk)).Size(18, 18).Radius(4).AlignItems(ui.Center).Justify(ui.Center)
		b.Children(func() { ui.Icon(c, icClose).Size(10, 10).TextColor(col) })
		if b.Clicked() {
			s.notice, s.noticeErr = "", false
		}
	})
}

// skillGroups lists the skills by location, in the CLI's order.
func (u *settingsUI) skillGroups(c *ui.Context, sum skills.Summary, list []skills.Skill, acts SkillActions) {
	l, t := u.L(), c.Theme()
	var groups []skillGroup
	at := map[string]int{}
	for _, sk := range list {
		key, title := skillGroupOf(l, sk)
		i, ok := at[key]
		if !ok {
			i = len(groups)
			at[key] = i
			groups = append(groups, skillGroup{key: key, title: title})
		}
		groups[i].items = append(groups[i].items, sk)
	}
	for _, g := range groups {
		first := g.items[0]
		ui.Column(c).Gap(6).Children(func() {
			ui.Row(c).Gap(6).Padding(0, 4).AlignItems(ui.Center).Children(func() {
				Tile(c, skillScopeIcon(first.Scope), skillScopeTint(first.Scope), 16, false)
				ui.Text(c, g.title).FontSize(12).FontWeight(600).SingleLine().Ellipsis("…").Shrink(1)
				if first.Scope != skills.ScopeUser {
					ui.Text(c, first.RootLabel).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Shrink(1)
				}
				ui.Text(c, fmt.Sprint(len(g.items))).FontSize(11).FontFeatures("tnum").TextColor(t.TextMuted)
				ui.Spacer(c).Grow(1)
				if acts.OpenFolder != nil && IconButton(c, icFolder, tx(l, xSkillsOpenFolder), false).Clicked() {
					u.openSkillFolder(acts, first.Root)
				}
			})
			Panel(c).Children(func() {
				for i, sk := range g.items {
					settingsRule(c, i)
					u.skillListRow(c, sum, sk, acts)
				}
			})
		})
	}
}

func (u *settingsUI) openSkillFolder(acts SkillActions, dir string) {
	if acts.OpenFolder == nil {
		return
	}
	if err := acts.OpenFolder(dir); err != nil {
		s := u.skillsState()
		s.notice, s.noticeErr = tx(u.L(), xSkillsOpenFailed, err.Error()), true
	}
}

// skillListRow is one skill: its tile, name and badges, description and
// a line of facts, with its switch beside the button to its page.
func (u *settingsUI) skillListRow(c *ui.Context, sum skills.Summary, sk skills.Skill, acts SkillActions) {
	s := u.skillsState()
	l, t := u.L(), c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		b := ui.ButtonBase(c.Key("skill-row-"+sk.ID)).Label(skillTitle(sk)).Grow(1).Basis(0).Padding(9, 4).Gap(10).
			Radius(6).AlignItems(ui.Start).Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(pal(t).Hover)
		}
		b.Children(func() {
			skillTile(c, sk, 30)
			ui.Column(c).Gap(3).Grow(1).Basis(0).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					name := ui.Text(c, skillTitle(sk)).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…").Shrink(1)
					if sk.State != skills.StateOn {
						name.TextColor(t.TextMuted)
					}
					if sk.DisplayName != "" && sk.DisplayName != sk.InvokeName {
						ui.Text(c, sk.InvokeName).FontSize(11).Font("monospace").TextColor(t.TextMuted).SingleLine().Ellipsis("…").Shrink(2)
					}
					skillBadges(c, l, sum, sk)
				})
				if sub := skillSubtitle(sk); sub != "" {
					ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted).MaxLines(2).Ellipsis("…")
				}
				ui.Text(c, skillRowFacts(l, sum, sk)).FontSize(11).TextColor(t.TextMuted.Alpha(0.8)).SingleLine().Ellipsis("…")
			})
			ui.Icon(c, icChevRight).Size(12, 12).TextColor(t.TextMuted).Margin(9, 0, 0, 0)
		})
		if b.Clicked() {
			s.closeItem()
			s.open = sk.ID
		}
		u.skillRowSwitch(c, sk, acts)
	})
}

// skillRowFacts is the small line under a row's description.
func skillRowFacts(l l10n.Localizer, sum skills.Summary, sk skills.Skill) string {
	parts := []string{skillPath(sum, sk.Dir)}
	if !sk.ModTime.IsZero() {
		parts = append(parts, tx(l, xSkillsModified)+" "+sk.ModTime.Local().Format("2006-01-02"))
	}
	if len(sk.SharedWith) > 0 {
		parts = append(parts, tx(l, xSkillsSharedWith)+" "+skillProviders(l, sk.SharedWith))
	}
	if sk.Plugin != "" {
		parts = append(parts, tx(l, xSkillsPlugin)+" "+sk.Plugin)
	}
	return strings.Join(parts, " · ")
}

// skillRowSwitch turns a skill on or off from the list. The switch shows
// the new value until the scan after the write.
func (u *settingsUI) skillRowSwitch(c *ui.Context, sk skills.Skill, acts SkillActions) {
	s := u.skillsState()
	l := u.L()
	if !skillSwitchable(sk) {
		ui.Box(c).Width(38).Shrink(0)
		return
	}
	on := sk.State != skills.StateOff
	if v, ok := s.pending[sk.ID]; ok {
		on = v
	}
	set := acts.SetEnabled
	if !ui.Switch(c.Key("skill-switch-"+sk.ID), &on).Label(tx(l, xSkillsEnabled)+" "+skillTitle(sk)).Disabled(set == nil || s.work.Running).Changed() || set == nil {
		return
	}
	u.toggleSkill(acts, sk, on)
}

func (u *settingsUI) toggleSkill(acts SkillActions, sk skills.Skill, on bool) {
	s := u.skillsState()
	if s.pending == nil {
		s.pending = map[string]bool{}
	}
	s.pending[sk.ID] = on
	hint := skillToggleHint(u.L(), sk.Provider)
	u.skillWork(acts, func() (string, error) { return hint, acts.SetEnabled(sk, on) }, xSkillsToggleFailed, func(err error) {
		if err != nil {
			delete(s.pending, sk.ID)
		}
	})
}

// skillTile is the square icon of a skill, tinted by who owns it.
func skillTile(c *ui.Context, sk skills.Skill, size float32) {
	ui.Row(c).Shrink(0).Children(func() {
		Tile(c, skillScopeIcon(sk.Scope), skillScopeTint(sk.Scope), size, false)
	})
}

func skillScopeIcon(scope skills.Scope) *ui.SVG {
	switch scope {
	case skills.ScopePlugin:
		return icPlug
	case skills.ScopeSystem, skills.ScopeBundled:
		return icToolbox
	case skills.ScopeAdmin:
		return icShield
	case skills.ScopeSynced:
		return icCloudDown
	}
	return icSkill
}

func skillScopeTint(scope skills.Scope) Tint {
	switch scope {
	case skills.ScopePlugin:
		return TintTeal
	case skills.ScopeSystem:
		return TintGray
	case skills.ScopeBundled:
		return TintBlue
	case skills.ScopeAdmin:
		return TintOrange
	case skills.ScopeSynced:
		return TintIndigo
	}
	return TintPurple
}

func skillScopeName(l l10n.Localizer, scope skills.Scope) string {
	if scope == skills.ScopeUser {
		return tx(l, xSkillsScopeUser)
	}
	return skillScopeTitle(l, scope)
}

// skillBadges are the small tags after a skill's name.
func skillBadges(c *ui.Context, l l10n.Localizer, sum skills.Summary, sk skills.Skill) {
	t := c.Theme()
	if sk.State != skills.StateOn {
		settingsBadge(c, nil, skillStateName(l, sk.State), skillStateColor(t, sk.State))
	}
	if sk.Scope != skills.ScopeUser {
		settingsBadge(c, nil, skillScopeTitle(l, sk.Scope), skillScopeTint(sk.Scope).C(t))
	}
	if sk.LinkPath != "" {
		settingsBadge(c, icLink, tx(l, xSkillsKindLink), t.TextMuted)
	}
	if sk.Origin.GitHub != "" {
		settingsBadge(c, icGithub, "GitHub", t.TextMuted)
	}
	if n := len(skillIssuesOf(sum, sk)); n > 0 {
		settingsBadge(c, icWarnCircle, fmt.Sprint(n), t.Warning)
	}
}

func settingsBadge(c *ui.Context, icon *ui.SVG, text string, col ui.Color) {
	ui.Row(c).Gap(3).Padding(1, 6).Radius(5).Background(col.Alpha(0.13)).AlignItems(ui.Center).Shrink(0).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(10, 10).TextColor(col)
		}
		ui.Text(c, text).FontSize(10.5).FontWeight(500).TextColor(col).SingleLine()
	})
}

// skillsIssues is the diagnostics box under the list.
func (u *settingsUI) skillsIssues(c *ui.Context, sum skills.Summary, acts SkillActions) {
	if len(sum.Issues) == 0 {
		return
	}
	s := u.skillsState()
	l, t := u.L(), c.Theme()
	header := func() {
		label := tx(l, xSkillsShowIssues)
		if s.showIssues {
			label = tx(l, xSkillsHideIssues)
		}
		if PillButton(c, nil, label, false).Clicked() {
			s.showIssues = !s.showIssues
		}
	}
	settingsSection(c, tx(l, xSkillsIssuesTitle), tx(l, xSkillsIssueCount, len(sum.Issues)), header, func() {
		if !s.showIssues {
			ui.Text(c, tx(l, xSkillsIssuesHint)).FontSize(12).TextColor(t.TextMuted).Padding(9, 0)
			return
		}
		for i, is := range sum.Issues {
			settingsRule(c, i)
			u.skillIssueRow(c, sum, is, acts)
		}
	})
}

func (u *settingsUI) skillIssueRow(c *ui.Context, sum skills.Summary, is skills.Issue, acts SkillActions) {
	s := u.skillsState()
	l, t := u.L(), c.Theme()
	ui.Row(c).Gap(10).Padding(9, 0).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icWarnCircle).Size(14, 14).TextColor(t.Warning)
		ui.Column(c).Gap(2).Grow(1).Basis(0).Children(func() {
			title := skillIssueText(l, is.Kind)
			if is.Name != "" {
				title += " · " + is.Name
			}
			ui.Text(c, title).FontSize(13)
			ui.Text(c, skillPath(sum, is.Path)).FontSize(11).Font("monospace").TextColor(t.TextMuted).Selectable()
			if is.Detail != "" {
				ui.Text(c, skillPath(sum, is.Detail)).FontSize(11).TextColor(t.TextMuted).Selectable()
			}
		})
		if is.Kind != skills.IssueBrokenLink || !is.Removable {
			return
		}
		del := acts.DeleteLink
		id := "link\x00" + is.Path
		b := PillButton(c, icTrash, tx(l, xSkillsRemoveLink), del == nil || s.work.Running)
		open := s.confirm == id
		if b.Clicked() && del != nil {
			s.confirm, open = id, true
		}
		confirmPopover(c, l, b, &open, tx(l, xSkillsRemoveLinkTitle), tx(l, xSkillsRemoveLinkMsg, skillPath(sum, is.Path)), tx(l, xSkillsRemoveLink), nil, func() {
			u.skillWork(acts, func() (string, error) {
				system, err := del(sum, is.Path)
				return skillMovedText(l, system), err
			}, xSkillsDeleteFailed, nil)
		})
		if !open && s.confirm == id {
			s.confirm = ""
		}
	})
}

func skillMovedText(l l10n.Localizer, system bool) string {
	if system {
		return tx(l, xSkillsMovedSystem)
	}
	return tx(l, xSkillsMovedTrash)
}

// skillsRoots lists every location the CLI reads, present or not.
func (u *settingsUI) skillsRoots(c *ui.Context, sum skills.Summary, acts SkillActions) {
	if len(sum.Roots) == 0 {
		return
	}
	l, t := u.L(), c.Theme()
	settingsSection(c, tx(l, xSkillsRoots), tx(l, xSkillsRootsHint), nil, func() {
		for i, r := range sum.Roots {
			settingsRule(c, i)
			ui.Row(c).Gap(10).Padding(8, 0).AlignItems(ui.Center).Children(func() {
				Tile(c, skillScopeIcon(r.Scope), skillScopeTint(r.Scope), 20, false)
				ui.Column(c).Gap(2).Grow(1).Basis(0).Children(func() {
					ui.Text(c, r.Label).FontSize(12).Font("monospace").SingleLine().Ellipsis("…").Selectable()
					ui.Text(c, skillScopeName(l, r.Scope)).FontSize(11).TextColor(t.TextMuted)
				})
				if !r.Exists {
					ui.Text(c, tx(l, xSkillsRootMissing)).FontSize(11).TextColor(t.TextMuted)
					return
				}
				ui.Text(c, tx(l, xSkillsCount, r.Count)).FontSize(11).FontFeatures("tnum").TextColor(t.TextMuted)
				if acts.OpenFolder != nil && IconButton(c, icFolder, tx(l, xSkillsOpenFolder), false).Clicked() {
					u.openSkillFolder(acts, r.Path)
				}
			})
		}
	})
}

// ---- One skill ----

type skillFact struct {
	label, value string
	mono         bool
	url          string // opens in the browser from a trailing button
	color        *ui.Color
}

// skillPage is one skill: a header with its links and actions, then
// boxes of facts, files, frontmatter, diagnostics and SKILL.md.
func (u *settingsUI) skillPage(c *ui.Context, sum skills.Summary, sk skills.Skill, acts SkillActions) {
	u.loadSkillPageDetail(acts, sk)
	u.skillBreadcrumb(c, sk)
	u.skillHeader(c, sum, sk, acts)
	u.skillsNotice(c)
	l := u.L()
	u.skillStatusBox(c, sum, sk)
	skillFactsBox(c, tx(l, xSkillsSecBasics), "", skillBasicFacts(l, sum, sk))
	u.skillOriginBox(c, sk)
	u.skillFilesBox(c, sk)
	if len(sk.Fields) > 0 {
		facts := make([]skillFact, 0, len(sk.Fields))
		for _, f := range sk.Fields {
			facts = append(facts, skillFact{label: f.Key, value: f.Value})
		}
		skillFactsBox(c, "Frontmatter", tx(l, xSkillsFrontHint), facts)
	}
	if issues := skillIssuesOf(sum, sk); len(issues) > 0 {
		settingsSection(c, tx(l, xSkillsIssuesTitle), "", nil, func() {
			for i, is := range issues {
				settingsRule(c, i)
				u.skillIssueRow(c, sum, is, acts)
			}
		})
	}
	u.skillBodyBox(c, sk, acts)
}

func (u *settingsUI) skillBreadcrumb(c *ui.Context, sk skills.Skill) {
	s := u.skillsState()
	l, t := u.L(), c.Theme()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		back := ui.ButtonBase(c.Key("skill-back")).Label(tx(l, xSkillsBack)).Padding(3, 6, 3, 2).Gap(3).Radius(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		if back.Hovered() {
			back.Background(pal(t).Hover)
		}
		back.Children(func() {
			ui.Icon(c, icChevLeft).Size(13, 13).TextColor(t.Accent)
			ui.Text(c, tx(l, xSkillsSettingsTab)).FontSize(13).TextColor(t.Accent).SingleLine()
		})
		if back.Clicked() {
			s.closeItem()
		}
		ui.Text(c, "/").FontSize(13).TextColor(t.TextMuted)
		ProviderIcon(c, s.provider(), 14)
		ui.Text(c, l.Text(providerKey(s.provider()))).FontSize(13).TextColor(t.TextMuted).SingleLine()
		ui.Text(c, "/").FontSize(13).TextColor(t.TextMuted)
		ui.Text(c, skillTitle(sk)).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…").Shrink(1)
	})
}

func (u *settingsUI) skillHeader(c *ui.Context, sum skills.Summary, sk skills.Skill, acts SkillActions) {
	l, t := u.L(), c.Theme()
	Panel(c).Padding(16).Gap(12).Children(func() {
		ui.Row(c).Gap(14).AlignItems(ui.Start).Children(func() {
			skillTile(c, sk, 44)
			ui.Column(c).Gap(5).Grow(1).Basis(0).Children(func() {
				ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
					ui.Text(c, skillTitle(sk)).FontSize(18).FontWeight(700).Selectable()
					if sk.State == skills.StateOn {
						settingsBadge(c, nil, tx(l, xSkillsStateOn), t.Success)
					}
					skillBadges(c, l, sum, sk)
				})
				if sk.InvokeName != skillTitle(sk) || sk.Origin.Version != "" {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, sk.InvokeName).FontSize(12).Font("monospace").TextColor(t.TextMuted).Selectable().Shrink(1)
						if sk.Origin.Version != "" {
							ui.Text(c, "v"+strings.TrimPrefix(sk.Origin.Version, "v")).FontSize(11).TextColor(t.TextMuted)
						}
					})
				}
				if sk.Description != "" {
					ui.Text(c, sk.Description).FontSize(13).LineHeight(1.4).Selectable()
				}
				if sk.ShortDescription != "" && sk.ShortDescription != sk.Description {
					ui.Text(c, sk.ShortDescription).FontSize(12).TextColor(t.TextMuted).Selectable()
				}
			})
		})
		u.skillLinks(c, sk)
		ui.Box(c).Height(1).Background(pal(t).Line)
		u.skillActionsRow(c, sum, sk, acts)
	})
}

// skillLinks are the buttons to the skill's repository, its folder there
// and its homepage.
func (u *settingsUI) skillLinks(c *ui.Context, sk skills.Skill) {
	o := sk.Origin
	if o.Repository == "" && o.Homepage == "" {
		return
	}
	l := u.L()
	ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
		if o.Repository != "" {
			icon, label := icExternal, tx(l, xSkillsOpenRepo)
			if o.GitHub != "" {
				icon, label = icGithub, o.GitHub
			}
			if PillButton(c, icon, label, false).Tooltip(o.Repository).Clicked() {
				c.OpenURL(o.Repository)
			}
		}
		if o.Folder != "" && PillButton(c, icFolder, tx(l, xSkillsOpenOnline), false).Tooltip(o.Folder).Clicked() {
			c.OpenURL(o.Folder)
		}
		if o.Homepage != "" && o.Homepage != o.Repository && PillButton(c, icGlobe, tx(l, xSkillsOpenHomepage), false).Tooltip(o.Homepage).Clicked() {
			c.OpenURL(o.Homepage)
		}
	})
}

// skillActionsRow is the switch, the folder and copy buttons and the
// trash button with its confirmation.
func (u *settingsUI) skillActionsRow(c *ui.Context, sum skills.Summary, sk skills.Skill, acts SkillActions) {
	s := u.skillsState()
	l, t := u.L(), c.Theme()
	ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
		if skillSwitchable(sk) {
			ui.Row(c).Gap(8).Padding(0, 4, 0, 0).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tx(l, xSkillsEnabled)).FontSize(13)
				u.skillRowSwitch(c, sk, acts)
			})
		}
		if PillButton(c, icFolder, tx(l, xSkillsOpenFolder), acts.OpenFolder == nil).Clicked() {
			u.openSkillFolder(acts, sk.Dir)
		}
		if PillButton(c, icFile, tx(l, xSkillsReveal), acts.Reveal == nil).Clicked() && acts.Reveal != nil {
			acts.Reveal(sk.File)
		}
		if PillButton(c, icCopy, tx(l, xSkillsCopyPath), acts.Copy == nil).Clicked() && acts.Copy != nil {
			acts.Copy(sk.File)
			u.toast = tx(l, xSkillsCopied)
		}
		ui.Spacer(c).Grow(1)
		if !sk.Deletable {
			return
		}
		del := acts.Delete
		b := pillButton(c, icTrash, tx(l, xSkillsDelete), del == nil || s.work.Running, true)
		open := s.confirm == sk.ID
		if b.Clicked() && del != nil && !s.work.Running {
			s.confirm, open = sk.ID, true
		}
		msg := tx(l, xSkillsDeleteMessage, skillPath(sum, sk.Removal.Path))
		confirmPopover(c, l, b, &open, tx(l, xSkillsDeleteTitle), msg, tx(l, xSkillsDelete), func() { skillDeleteNotes(c, l, sk) }, func() {
			var system bool
			u.skillWork(acts, func() (string, error) {
				var err error
				system, err = del(sk, sum.Roots)
				return skillMovedText(l, system), err
			}, xSkillsDeleteFailed, func(err error) {
				if err == nil {
					notice := s.notice
					s.closeItem()
					s.notice = notice
				}
			})
		})
		if !open && s.confirm == sk.ID {
			s.confirm = ""
		}
	})
	if !sk.Deletable {
		ui.Text(c, skillReadOnlyText(l, sk)).FontSize(11).TextColor(t.TextMuted)
	}
}

func (u *settingsUI) skillStatusBox(c *ui.Context, sum skills.Summary, sk skills.Skill) {
	l, t := u.L(), c.Theme()
	stateCol := skillStateColor(t, sk.State)
	facts := []skillFact{{label: tx(l, xSkillsState), value: skillStateName(l, sk.State), color: &stateCol}}
	if why := skillReasonText(l, sk.Reason); why != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsReason), value: why})
	}
	toggle := skillToggleHint(l, sk.Provider)
	if !skillSwitchable(sk) {
		toggle = skillToggleBlockText(l, sk)
	}
	facts = append(facts, skillFact{label: tx(l, xSkillsSwitch), value: toggle})
	if sk.OverriddenBy != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsOverridden), value: skillPath(sum, sk.OverriddenBy), mono: true})
	}
	if len(sk.SharedWith) > 0 {
		facts = append(facts, skillFact{label: tx(l, xSkillsSharedWith), value: skillProviders(l, sk.SharedWith)})
	}
	skillFactsBox(c, tx(l, xSkillsState), "", facts)
}

func skillBasicFacts(l l10n.Localizer, sum skills.Summary, sk skills.Skill) []skillFact {
	facts := []skillFact{
		{label: tx(l, xSkillsSortName), value: sk.Name, mono: true},
		{label: tx(l, xSkillsInvoke), value: sk.InvokeName, mono: true},
	}
	if sk.DisplayName != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsDisplayName), value: sk.DisplayName})
	}
	facts = append(facts,
		skillFact{label: tx(l, xSkillsScope), value: skillScopeName(l, sk.Scope)},
		skillFact{label: tx(l, xSkillsSource), value: sk.RootLabel, mono: true},
	)
	if sk.Plugin != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsPlugin), value: sk.Plugin})
	}
	facts = append(facts, skillFact{label: tx(l, xSkillsLocation), value: skillPath(sum, sk.File), mono: true})
	if sk.LinkPath != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsKindLink), value: skillPath(sum, sk.LinkPath), mono: true})
	}
	if sk.LinkTarget != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsLinkTarget), value: sk.LinkTarget, mono: true})
	}
	if sk.RealFile != "" && sk.RealFile != sk.File && sk.LinkTarget == "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsRealPath), value: sk.RealFile, mono: true})
	}
	if !sk.ModTime.IsZero() {
		facts = append(facts, skillFact{label: tx(l, xSkillsModified), value: skillTime(sk.ModTime)})
	}
	return facts
}

func skillTime(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

func (u *settingsUI) skillOriginBox(c *ui.Context, sk skills.Skill) {
	l, t := u.L(), c.Theme()
	o := sk.Origin
	if o.From == skills.OriginNone && o.Homepage == "" {
		settingsSection(c, tx(l, xSkillsSecOrigin), "", nil, func() {
			ui.Text(c, tx(l, xSkillsOriginUnknown)).FontSize(12).TextColor(t.TextMuted).Padding(9, 0)
		})
		return
	}
	var facts []skillFact
	if v := skillInstallerText(l, o); v != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsInstaller), value: v})
	}
	if o.GitHub != "" {
		facts = append(facts, skillFact{label: "GitHub", value: o.GitHub, url: o.Repository})
	} else if o.Repository != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsRepository), value: o.Repository, url: o.Repository})
	}
	if o.SkillPath != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsRepoPath), value: o.SkillPath, mono: true, url: o.Folder})
	}
	if o.Branch != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsBranch), value: o.Branch, mono: true})
	}
	if o.Homepage != "" && o.Homepage != o.Repository {
		facts = append(facts, skillFact{label: tx(l, xSkillsHomepage), value: o.Homepage, url: o.Homepage})
	}
	if o.Version != "" {
		facts = append(facts, skillFact{label: tx(l, xSkillsVersion), value: o.Version})
	}
	if !o.InstalledAt.IsZero() {
		facts = append(facts, skillFact{label: tx(l, xSkillsInstalledAt), value: skillTime(o.InstalledAt)})
	}
	if !o.UpdatedAt.IsZero() && !o.UpdatedAt.Equal(o.InstalledAt) {
		facts = append(facts, skillFact{label: tx(l, xSkillsUpdatedAt), value: skillTime(o.UpdatedAt)})
	}
	skillFactsBox(c, tx(l, xSkillsSecOrigin), "", facts)
}

func skillInstallerText(l l10n.Localizer, o skills.Origin) string {
	switch o.From {
	case skills.OriginLock:
		return tx(l, xSkillsInstSkillsCLI)
	case skills.OriginGit:
		return tx(l, xSkillsInstGit)
	case skills.OriginPlugin:
		if o.Installer != "" {
			return tx(l, xSkillsInstMarket, o.Installer)
		}
		return tx(l, xSkillsScopePlugin)
	case skills.OriginFrontmatter:
		return tx(l, xSkillsInstFront)
	}
	return ""
}

// skillFactsBox is a box of label / value rows under a title.
func skillFactsBox(c *ui.Context, title, subtitle string, facts []skillFact) {
	t := c.Theme()
	settingsSection(c, title, subtitle, nil, func() {
		for i, f := range facts {
			settingsRule(c, i)
			ui.Row(c).Gap(14).Padding(8, 0).MinHeight(34).AlignItems(ui.Center).Children(func() {
				ui.Text(c, f.label).FontSize(12).TextColor(t.TextMuted).Width(118).Shrink(0)
				v := ui.Text(c, f.value).FontSize(12).Grow(1).Basis(0).Selectable()
				if f.mono {
					v.Font("monospace")
				}
				if f.color != nil {
					v.TextColor(*f.color).FontWeight(600)
				}
				if f.url != "" && IconButton(c, icExternal, f.url, false).Clicked() {
					c.OpenURL(f.url)
				}
			})
		}
	})
}

// skillFilesBox lists the folder of the skill as Inspect read it.
func (u *settingsUI) skillFilesBox(c *ui.Context, sk skills.Skill) {
	s := u.skillsState()
	d := &s.detail
	l, t := u.L(), c.Theme()
	subtitle := ""
	if d.id == sk.ID && d.task.Err == nil && !d.task.Running {
		subtitle = tx(l, xSkillsFilesCount, d.detail.FileCount, d.detail.DirCount) + " · " + skillBytes(d.detail.Size)
		if d.detail.Truncated {
			subtitle += " · " + tx(l, xSkillsFilesPartial)
		}
	}
	files := d.detail.Files
	header := func() {
		if len(files) <= skillFileRows {
			return
		}
		label := tx(l, xSkillsFilesMore, len(files))
		if d.allRows {
			label = tx(l, xSkillsFilesLess)
		}
		if PillButton(c, nil, label, false).Clicked() {
			d.allRows = !d.allRows
		}
	}
	settingsSection(c, tx(l, xSkillsSecFiles), subtitle, header, func() {
		switch {
		case d.task.Running:
			skillsLoading(c)
			return
		case d.task.Err != nil:
			ui.Text(c, tx(l, xSkillsInspectFailed, d.task.Err.Error())).FontSize(12).TextColor(t.Danger).Padding(9, 0)
			return
		case len(files) == 0:
			ui.Text(c, tx(l, xSkillsNoFiles)).FontSize(12).TextColor(t.TextMuted).Padding(9, 0)
			return
		}
		if !d.allRows && len(files) > skillFileRows {
			files = files[:skillFileRows]
		}
		for i, f := range files {
			settingsRule(c, i)
			skillFileRow(c, l, f)
		}
	})
}

func skillFileRow(c *ui.Context, l l10n.Localizer, f skills.FileEntry) {
	t := c.Theme()
	icon := icFile
	switch {
	case f.Link:
		icon = icLink
	case f.Dir:
		icon = icFolder
	}
	depth := strings.Count(f.Path, "/")
	ui.Row(c).Gap(8).Padding(6, 0, 6, float32(min(depth, 6))*14).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).Size(13, 13).TextColor(t.TextMuted)
		name := f.Path
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		ui.Text(c, name).FontSize(12).Font("monospace").SingleLine().Ellipsis("…").Grow(1).Basis(0).Tooltip(f.Path)
		if f.Skipped {
			settingsBadge(c, nil, tx(l, xSkillsSkippedDir), t.TextMuted)
		}
		if !f.Dir && !f.Link {
			ui.Text(c, skillBytes(f.Size)).FontSize(11).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine()
		}
		if !f.ModTime.IsZero() {
			ui.Text(c, f.ModTime.Local().Format("2006-01-02")).FontSize(11).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine().Width(72)
		}
	})
}

// skillBytes is a size in binary units.
func skillBytes(n int64) string {
	switch {
	case n < 0:
		return "—"
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
}

// skillBodyBox shows SKILL.md after the frontmatter on demand.
func (u *settingsUI) skillBodyBox(c *ui.Context, sk skills.Skill, acts SkillActions) {
	s := u.skillsState()
	b := &s.body
	l, t := u.L(), c.Theme()
	shown := b.id == sk.ID && b.shown
	header := func() {
		label := tx(l, xSkillsShowContent)
		if shown {
			label = tx(l, xSkillsHideContent)
		}
		if PillButton(c, nil, label, false).Clicked() {
			if shown {
				b.shown = false
			} else {
				u.loadSettingsSkillBody(acts, sk)
			}
		}
	}
	settingsSection(c, "SKILL.md", tx(l, xSkillsBodyHint), header, func() {
		switch {
		case !shown:
			ui.Text(c, tx(l, xSkillsBodyHidden)).FontSize(12).TextColor(t.TextMuted).Padding(9, 0)
		case b.task.Running:
			skillsLoading(c)
		case b.task.Err != nil:
			ui.Text(c, tx(l, xSkillsReadFailed, b.task.Err.Error())).FontSize(12).TextColor(t.Danger).Padding(9, 0)
		case strings.TrimSpace(b.text) == "":
			ui.Text(c, tx(l, xSkillsNoContent)).FontSize(12).TextColor(t.TextMuted).Padding(9, 0)
		default:
			ui.Column(c).Gap(8).Padding(10, 2).Children(func() {
				if b.truncated {
					ui.Text(c, tx(l, xSkillsTruncated)).FontSize(11).TextColor(t.TextMuted)
				}
				s.md.WrapCode = true
				markdown.View(c, b.text, &s.md)
			})
		}
	})
}

// ---- Trash ----

func (u *settingsUI) skillsTrash(c *ui.Context, acts SkillActions) {
	s := u.skillsState()
	tr := &s.trash
	l, t := u.L(), c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(0, 4).Children(func() {
		ui.Icon(c, icInfo).Size(13, 13).TextColor(t.TextMuted)
		ui.Text(c, tx(l, xSkillsTrashInfo)).FontSize(12).TextColor(t.TextMuted).Grow(1).Basis(0)
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.TextInput(c.Key("skills-trash-search"), &tr.search).Placeholder(tx(l, xSkillsTrashSearch)).Label(tx(l, xSkillsTrashSearch)).Grow(1).Basis(0)
		labels := append([]string{tx(l, xSkillsAll)}, providerLabels(l, skillsCLIProviders())...)
		PillTabs(c.Key("skills-trash-provider"), &tr.provider, false, labels...)
		if acts.TrashDir != "" && IconButton(c, icFolder, tx(l, xSkillsOpenTrashDir), acts.OpenFolder == nil).Clicked() {
			u.openSkillFolder(acts, acts.TrashDir)
		}
		empty := acts.EmptyTrash
		b := pillButton(c, icTrash, tx(l, xSkillsEmptyTrash), empty == nil || len(tr.list) == 0 || s.work.Running, true)
		open := tr.confirm == "empty"
		if b.Clicked() && empty != nil && len(tr.list) > 0 {
			tr.confirm, open = "empty", true
		}
		confirmPopover(c, l, b, &open, tx(l, xSkillsEmptyTrashTitle), tx(l, xSkillsEmptyTrashMsg, len(tr.list)), tx(l, xSkillsEmptyTrash), nil, func() {
			u.skillWork(acts, func() (string, error) {
				n, err := empty()
				return tx(l, xSkillsEmptied, n), err
			}, xSkillsPurgeFailed, nil)
		})
		if !open && tr.confirm == "empty" {
			tr.confirm = ""
		}
	})
	u.skillsNotice(c)
	switch {
	case !tr.loaded && tr.task.Err != nil:
		ui.Text(c, tx(l, xSkillsTrashFailed, tr.task.Err.Error())).FontSize(12).TextColor(t.Danger)
		return
	case !tr.loaded:
		skillsLoading(c)
		return
	case len(tr.list) == 0:
		skillsEmptyBox(c, tx(l, xSkillsTrashEmpty))
		return
	}
	var list []skills.TrashEntry
	for _, e := range tr.list {
		if trashMatches(e, tr.provider, tr.search) {
			list = append(list, e)
		}
	}
	if len(list) == 0 {
		skillsEmptyBox(c, tx(l, xSkillsNoMatch))
		return
	}
	var size int64
	for _, e := range list {
		size += max(e.Size, 0)
	}
	ui.Text(c, tx(l, xSkillsTrashTotal, len(list), skillBytes(size))).FontSize(11).FontFeatures("tnum").TextColor(t.TextMuted).Padding(0, 4)
	Panel(c).Children(func() {
		for i, e := range list {
			settingsRule(c, i)
			u.trashRow(c, e, acts)
		}
	})
}

func (u *settingsUI) trashRow(c *ui.Context, e skills.TrashEntry, acts SkillActions) {
	s := u.skillsState()
	tr := &s.trash
	l, t := u.L(), c.Theme()
	ui.Row(c).Gap(10).Padding(10, 0).AlignItems(ui.Start).Children(func() {
		icon := icSkill
		if e.Kind == skills.TrashBrokenLink || e.IsLink {
			icon = icLink
		}
		Tile(c, icon, TintGray, 30, false)
		ui.Column(c).Gap(3).Grow(1).Basis(0).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, trashTitle(e)).FontSize(13).FontWeight(600).SingleLine().Ellipsis("…").Shrink(1)
				ProviderIcon(c, store.Provider(e.Provider), 13)
				for _, p := range e.SharedWith {
					ProviderIcon(c, store.Provider(p), 13)
				}
				switch {
				case e.Kind == skills.TrashBrokenLink:
					settingsBadge(c, icLink, tx(l, xSkillsKindBrokenLink), t.Warning)
				case e.IsLink:
					settingsBadge(c, icLink, tx(l, xSkillsKindLink), t.TextMuted)
				}
				if e.Origin.GitHub != "" {
					settingsBadge(c, icGithub, e.Origin.GitHub, t.TextMuted)
				}
			})
			if e.Description != "" {
				ui.Text(c, e.Description).FontSize(12).TextColor(t.TextMuted).MaxLines(2).Ellipsis("…")
			}
			ui.Text(c, tx(l, xSkillsOriginal)+" "+valueOr(e.OriginalLabel, e.Original)).FontSize(11).Font("monospace").TextColor(t.TextMuted).Selectable()
			meta := tx(l, xSkillsDeletedAt) + " " + skillTime(e.DeletedAt)
			if e.Size >= 0 {
				meta += " · " + skillBytes(e.Size)
			}
			if n := len(e.Covers); n > 1 {
				meta += " · " + tx(l, xSkillsCount, n)
			}
			ui.Text(c, meta).FontSize(11).FontFeatures("tnum").TextColor(t.TextMuted)
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if acts.Reveal != nil && e.Item != "" && IconButton(c, icFolder, tx(l, xSkillsReveal), false).Clicked() {
				acts.Reveal(e.Item)
			}
			restore := acts.Restore
			if PillButton(c, icUndo, tx(l, xSkillsRestore), restore == nil || s.work.Running).Clicked() && restore != nil {
				id := e.ID
				u.skillWork(acts, func() (string, error) {
					got, err := restore(id)
					return tx(l, xSkillsRestored, valueOr(got.OriginalLabel, got.Original)), err
				}, xSkillsRestoreFailed, nil)
			}
			purge := acts.Purge
			key := "purge:" + e.ID
			b := pillButton(c, icTrash, tx(l, xSkillsPurge), purge == nil || s.work.Running, true)
			open := tr.confirm == key
			if b.Clicked() && purge != nil {
				tr.confirm, open = key, true
			}
			confirmPopover(c, l, b, &open, tx(l, xSkillsPurgeTitle), tx(l, xSkillsPurgeMsg), tx(l, xSkillsPurge), nil, func() {
				id := e.ID
				u.skillWork(acts, func() (string, error) { return tx(l, xSkillsPurged), purge(id) }, xSkillsPurgeFailed, nil)
			})
			if !open && tr.confirm == key {
				tr.confirm = ""
			}
		})
	})
}

func trashTitle(e skills.TrashEntry) string {
	for _, v := range []string{e.DisplayName, e.InvokeName, e.Name} {
		if v != "" {
			return v
		}
	}
	return e.ID
}
