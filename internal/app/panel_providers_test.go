package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

func TestPanelProviderChecksKeepAtLeastOne(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	if !e.s.SetSelectedProvider(store.ProviderGrok) || !e.s.SetPanelProviders([]store.Provider{store.ProviderCodex, store.ProviderGrok}) {
		t.Fatal("setup refused")
	}
	u := newSettingsUI(e.s, SettingsOptions{})
	tt := ui.NewTester(func(c *ui.Context) {
		settingsGroup(c, "", "", nil, func(row rowFunc) {
			u.panelProviderRows(c, row, e.s.Prefs(), false)
		})
	}, settingsWidth, settingsHeight)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if err := tt.Click(u.text(l10n.KeyProviderGrok)); err != nil {
		t.Fatal(err)
	}
	got := e.s.Prefs()
	if got.SelectedProvider != store.ProviderCodex || !slices.Equal(got.PanelProviders, []store.Provider{store.ProviderCodex}) {
		t.Fatalf("after hiding the selection: provider=%s list=%v", got.SelectedProvider, got.PanelProviders)
	}
	tt.Frame()
	_ = tt.Click(u.text(l10n.KeyProviderCodex))
	if got = e.s.Prefs(); !slices.Equal(got.PanelProviders, []store.Provider{store.ProviderCodex}) {
		t.Fatalf("the last provider was cleared: %v", got.PanelProviders)
	}
}

func TestPanelProviderScopeStartsFromEveryProvider(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	u := newSettingsUI(e.s, SettingsOptions{})
	tt := ui.NewTester(func(c *ui.Context) {
		settingsGroup(c, "", "", nil, func(row rowFunc) {
			u.panelProviderRows(c, row, e.s.Prefs(), false)
		})
	}, settingsWidth, settingsHeight)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	checks := u.text(l10n.KeyStatusBarProviders)
	if tt.HasText(checks) {
		t.Fatal("show-all offers provider checks")
	}
	focusSettingsAction(t, tt, tx(u.L(), xPanelProviders))
	tt.Key(0, ui.KeySpace)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := e.s.Prefs().PanelProviders; !slices.Equal(got, selectableProviders()) {
		t.Fatalf("custom starts as %v, want every selectable provider", got)
	}
	if !tt.HasText(checks) {
		t.Fatal("custom mode did not show the checks")
	}
}

func TestSinglePanelProviderHidesTheSwitcher(t *testing.T) {
	e := newEnv(t, nil)
	_, tt := openSettingsView(t, e, tabDisplay)
	l := e.s.L()
	desc := tx(l, xProviderDescription)
	if !tt.HasText(desc) || !tt.HasText(tx(l, xPanelProvidersAll)) {
		t.Fatalf("default display page: %q", tt.Texts())
	}
	if !e.s.SetPanelProviders([]store.Provider{store.ProviderCodex}) {
		t.Fatal("setup refused")
	}
	tt.Frame()
	if tt.HasText(desc) {
		t.Fatal("one provider still offers a switcher")
	}
	if !tt.HasText(tx(l, xPanelProvidersHint)) {
		t.Fatal("the provider row lost its hint")
	}
}

func TestPanelStripFollowsTheShownProviders(t *testing.T) {
	l := l10n.New(l10n.English)
	var selected store.Provider
	a, tt := newPanel(t, func(p *store.Preferences) {
		p.PanelProviders = []store.Provider{store.ProviderCodex, store.ProviderClaude}
	}, panelFixture(), PanelActions{})
	a.SetPanelActions(PanelActions{SelectProvider: func(p store.Provider) {
		selected = p
		a.settings.SetSelectedProvider(p)
	}})
	tt.Frame()
	if hasExact(tt, l.Text(l10n.KeyProviderGrok)) || !hasExact(tt, l.Text(l10n.KeyProviderClaude)) {
		t.Fatalf("strip: %q", tt.Texts())
	}
	if err := tt.Click(l.Text(l10n.KeyProviderClaude)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if selected != store.ProviderClaude || a.settings.Prefs().SelectedProvider != store.ProviderClaude {
		t.Fatalf("click selected %s, prefs %s", selected, a.settings.Prefs().SelectedProvider)
	}

	_, only := newPanel(t, func(p *store.Preferences) {
		p.PanelProviders = []store.Provider{store.ProviderCodex}
	}, panelFixture(), PanelActions{})
	if hasExact(only, l.Text(l10n.KeyProviderCodex)) || hasExact(only, l.Text(l10n.KeyProviderGrok)) || hasExact(only, l.Text(l10n.KeyProviderClaude)) {
		t.Fatalf("one provider still draws a strip: %q", only.Texts())
	}
}
