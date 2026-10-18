package app

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/skills"
	"github.com/licoy/quosparr/internal/store"
)

type settingsSkillCalls struct {
	deleted  []string
	inspect  []string
	restored []string
	purged   []string
	emptied  int
	copied   []string
}

const settingsSkillsRepo = "https://github.com/acme/agent-skills"

func settingsSkillsFixture() map[store.Provider]skills.Summary {
	res := skillsFixture()
	sum := res[store.ProviderCodex]
	sum.Skills[0].Origin = skills.Origin{
		From: skills.OriginLock, Repository: settingsSkillsRepo, GitHub: "acme/agent-skills",
		Folder: settingsSkillsRepo + "/tree/HEAD/skills/review", SkillPath: "skills/review",
		InstalledAt: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
	}
	res[store.ProviderCodex] = sum
	return res
}

func settingsTrashFixture() []skills.TrashEntry {
	return []skills.TrashEntry{
		{ID: "t1", Kind: skills.TrashSkill, Provider: skills.ProviderCodex, Name: "lint", InvokeName: "lint", Description: "Lint the code",
			Original: skillsTestRoot + "/lint", OriginalLabel: "~/.codex/skills/lint", DeletedAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), Size: 2048},
		{ID: "t2", Kind: skills.TrashBrokenLink, Provider: skills.ProviderClaude, Name: "gone",
			Original: "/home/u/.claude/skills/gone", OriginalLabel: "~/.claude/skills/gone", DeletedAt: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), Size: -1},
	}
}

// settingsSkillsView opens the settings window on the skills tab with
// fake actions and runs the first scan and trash read.
func settingsSkillsView(t *testing.T, system bool) (*settingsUI, *ui.Tester, chan func(), *settingsSkillCalls) {
	t.Helper()
	calls := &settingsSkillCalls{}
	trash := settingsTrashFixture()
	acts := SkillActions{
		Query:      func(context.Context) (map[store.Provider]skills.Summary, error) { return settingsSkillsFixture(), nil },
		ReadBody:   func(skills.Skill) (string, bool, error) { return "# Steps", false, nil },
		SetEnabled: func(skills.Skill, bool) error { return nil },
		Delete: func(s skills.Skill, _ []skills.RootInfo) (bool, error) {
			calls.deleted = append(calls.deleted, s.Removal.Path)
			return system, nil
		},
		DeleteLink: func(skills.Summary, string) (bool, error) { return system, nil },
		OpenFolder: func(string) error { return nil },
		Reveal:     func(string) {},
		Copy:       func(p string) { calls.copied = append(calls.copied, p) },
		Inspect: func(_ context.Context, s skills.Skill) (skills.Detail, error) {
			calls.inspect = append(calls.inspect, s.Name)
			return skills.Detail{Files: []skills.FileEntry{{Path: "SKILL.md", Size: 512}, {Path: "scripts", Dir: true}, {Path: "scripts/run.sh", Size: 64}}, FileCount: 2, DirCount: 1, Size: 576}, nil
		},
		TrashDir:  "/home/u/.quosparr/skills-trash",
		ListTrash: func(context.Context) ([]skills.TrashEntry, error) { return trash, nil },
		Restore: func(id string) (skills.TrashEntry, error) {
			calls.restored = append(calls.restored, id)
			return trash[0], nil
		},
		Purge:      func(id string) error { calls.purged = append(calls.purged, id); return nil },
		EmptyTrash: func() (int, error) { calls.emptied++; return len(trash), nil },
	}
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Log("load:", err)
	}
	e.s.Apply()
	queue := detailMainQueue(t) // after newEnv, which runs main work inline
	u := newSettingsUI(e.s, SettingsOptions{Version: "9.9.9", Skills: func() SkillActions { return acts }})
	u.tab = tabSkills
	tt := ui.NewTester(u.view, settingsWidth, settingsHeight)
	tt.Frame()
	settleSkills(t, tt, queue, 2) // the scan and the trash
	return u, tt, queue, calls
}

// settleSkills runs n finished background steps, in whatever order they
// arrive, and draws a frame.
func settleSkills(t *testing.T, tt *ui.Tester, queue chan func(), n int) {
	t.Helper()
	for range n {
		finishDetail(t, queue)
	}
	tt.Frame()
}

func TestSettingsSkillsTabFollowsNotificationsInItsOwnGroup(t *testing.T) {
	i := slices.Index(settingsTabs, tabSkills)
	if i <= 0 || settingsTabs[i-1] != tabRemote || settingsTabs[i+1] != tabAdvanced {
		t.Fatalf("tabs = %v", settingsTabs)
	}
	if !sidebarGroupStarts[tabSkills] || !sidebarGroupStarts[tabAdvanced] {
		t.Fatal("the skills tab does not start its own sidebar group")
	}
}

func TestSettingsSkillsListsProvidersStatesAndFilters(t *testing.T) {
	l := l10n.New(l10n.English)
	u, tt, _, _ := settingsSkillsView(t, false)
	for _, text := range []string{"Codex", "Claude", "Grok", tx(l, xSkillsTrash), tx(l, xSkillsAll), "review", "pdf", "imagegen", "GitHub", tx(l, xSkillsRoots)} {
		if !hasExact(tt, text) {
			t.Fatalf("skills tab missing %q; texts %q", text, tt.Texts())
		}
	}
	// The state cells filter: only pdf is off.
	if err := tt.Click(tx(l, xSkillsStateOff)); err != nil {
		t.Fatal(err)
	}
	if !hasExact(tt, "pdf") || hasExact(tt, "review") || hasExact(tt, "imagegen") {
		t.Fatalf("state filter did not apply; texts %q", tt.Texts())
	}
	if !hasExact(tt, tx(l, xSkillsShowing, 1, 3)) {
		t.Fatalf("count of shown skills missing; texts %q", tt.Texts())
	}
	f := &u.skills.filter[0]
	f.state, f.flags = "", skillFlagGitHub
	tt.Frame()
	if !hasExact(tt, "review") || hasExact(tt, "pdf") {
		t.Fatalf("GitHub flag did not filter; texts %q", tt.Texts())
	}
	if err := tt.Click(tx(l, xSkillsClearFilters)); err != nil {
		t.Fatal(err)
	}
	if f.flags != 0 || !hasExact(tt, "pdf") {
		t.Fatalf("clear filters left flags=%d; texts %q", f.flags, tt.Texts())
	}
	f.search = "acme"
	tt.Frame()
	if !hasExact(tt, "review") || hasExact(tt, "pdf") {
		t.Fatalf("search by repository did not filter; texts %q", tt.Texts())
	}
}

func TestSettingsSkillPageShowsOriginFilesAndLinks(t *testing.T) {
	l := l10n.New(l10n.English)
	u, tt, queue, calls := settingsSkillsView(t, false)
	if err := tt.Click("review"); err != nil {
		t.Fatal(err)
	}
	settleSkills(t, tt, queue, 1) // Inspect
	if len(calls.inspect) != 1 || calls.inspect[0] != "review" {
		t.Fatalf("inspect calls %v", calls.inspect)
	}
	for _, text := range []string{tx(l, xSkillsSecOrigin), tx(l, xSkillsInstSkillsCLI), "skills/review", tx(l, xSkillsSecFiles), "run.sh", tx(l, xSkillsFilesCount, 2, 1) + " · 576 B", tx(l, xSkillsOpenOnline)} {
		if !hasExact(tt, text) {
			t.Fatalf("skill page missing %q; texts %q", text, tt.Texts())
		}
	}
	if err := tt.Click("acme/agent-skills"); err != nil {
		t.Fatal(err)
	}
	if got := tt.OpenedURLs(); len(got) != 1 || got[0] != settingsSkillsRepo {
		t.Fatalf("opened %v", got)
	}
	if err := tt.Click(tx(l, xSkillsCopyPath)); err != nil {
		t.Fatal(err)
	}
	if len(calls.copied) != 1 || calls.copied[0] != skillsTestRoot+"/review/SKILL.md" {
		t.Fatalf("copied %v", calls.copied)
	}
	if err := tt.Click(tx(l, xSkillsBack)); err != nil {
		t.Fatal(err)
	}
	if u.skills.open != "" {
		t.Fatal("the breadcrumb did not return to the list")
	}
}

func TestSettingsSkillPageWithoutOriginSaysUnknown(t *testing.T) {
	l := l10n.New(l10n.English)
	u, tt, queue, _ := settingsSkillsView(t, false)
	u.skills.open = "codex\x00" + skillsTestShared + "/pdf/SKILL.md"
	tt.Frame()
	settleSkills(t, tt, queue, 1)
	if !hasExact(tt, tx(l, xSkillsOriginUnknown)) || hasExact(tt, tx(l, xSkillsOpenOnline)) {
		t.Fatalf("texts %q", tt.Texts())
	}
}

func TestSettingsSkillDeleteNamesTheSystemTrashFallback(t *testing.T) {
	l := l10n.New(l10n.English)
	u, tt, queue, calls := settingsSkillsView(t, true)
	u.skills.open = "codex\x00" + skillsTestShared + "/pdf/SKILL.md"
	tt.Frame()
	settleSkills(t, tt, queue, 1)
	if err := tt.Click(tx(l, xSkillsDelete)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	cancel, ok := tt.Find(l.Text(l10n.KeyCancel))
	if !ok {
		t.Fatalf("Cancel missing; texts %q", tt.Texts())
	}
	tt.ClickAt(cancel.X+cancel.W+8+20, cancel.Y+cancel.H/2)
	settleSkills(t, tt, queue, 3) // the move, then the scan and the trash
	if len(calls.deleted) != 1 || calls.deleted[0] != skillsTestShared+"/pdf" {
		t.Fatalf("deleted %v", calls.deleted)
	}
	if u.skills.open != "" || !hasExact(tt, tx(l, xSkillsMovedSystem)) {
		t.Fatalf("after delete: open=%q texts %q", u.skills.open, tt.Texts())
	}
}

func TestSettingsSkillsTrashRestoresPurgesAndFilters(t *testing.T) {
	l := l10n.New(l10n.English)
	u, tt, queue, calls := settingsSkillsView(t, false)
	if err := tt.Click(tx(l, xSkillsTrash)); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"lint", "Lint the code", "gone", tx(l, xSkillsKindBrokenLink), tx(l, xSkillsTrashTotal, 2, "2.0 KB")} {
		if !hasExact(tt, text) {
			t.Fatalf("trash missing %q; texts %q", text, tt.Texts())
		}
	}
	u.skills.trash.provider = 2 // Claude
	tt.Frame()
	if hasExact(tt, "lint") || !hasExact(tt, "gone") {
		t.Fatalf("provider filter did not apply; texts %q", tt.Texts())
	}
	u.skills.trash.provider = 0
	tt.Frame()
	if err := tt.Click(tx(l, xSkillsRestore)); err != nil {
		t.Fatal(err)
	}
	settleSkills(t, tt, queue, 3)
	if len(calls.restored) != 1 || calls.restored[0] != "t1" || !hasExact(tt, tx(l, xSkillsRestored, "~/.codex/skills/lint")) {
		t.Fatalf("restored %v; texts %q", calls.restored, tt.Texts())
	}
	if err := tt.Click(tx(l, xSkillsPurge)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasExact(tt, tx(l, xSkillsPurgeTitle)) || len(calls.purged) != 0 {
		t.Fatalf("purge did not confirm first; purged %v", calls.purged)
	}
	cancel, _ := tt.Find(l.Text(l10n.KeyCancel))
	tt.ClickAt(cancel.X+cancel.W+8+20, cancel.Y+cancel.H/2)
	settleSkills(t, tt, queue, 3)
	if len(calls.purged) != 1 || calls.purged[0] != "t1" {
		t.Fatalf("purged %v", calls.purged)
	}
}

func TestFilterSkillsSortsAndMatchesFlags(t *testing.T) {
	sum := settingsSkillsFixture()[store.ProviderCodex]
	names := func(list []skills.Skill) []string {
		var out []string
		for _, sk := range list {
			out = append(out, sk.Name)
		}
		return out
	}
	if got := names(filterSkills(sum, skillFilter{sort: skillSortName})); !slices.Equal(got, []string{"imagegen", "pdf", "review"}) {
		t.Fatalf("by name %v", got)
	}
	if got := names(filterSkills(sum, skillFilter{flags: skillFlagShared})); !slices.Equal(got, []string{"pdf"}) {
		t.Fatalf("shared %v", got)
	}
	if got := names(filterSkills(sum, skillFilter{flags: skillFlagDeletable | skillFlagSwitchable})); !slices.Equal(got, []string{"review", "pdf"}) {
		t.Fatalf("deletable and switchable %v", got)
	}
	if got := names(filterSkills(sum, skillFilter{state: skills.StateOn, source: skillsTestRoot})); len(got) > 2 {
		t.Fatalf("state and source %v", got)
	}
}
