package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/cursor"
	"github.com/licoy/quosparr/internal/deepseek"
	"github.com/licoy/quosparr/internal/elevenlabs"
	"github.com/licoy/quosparr/internal/kimi"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/minimax"
	"github.com/licoy/quosparr/internal/pixellab"
	"github.com/licoy/quosparr/internal/platform"
	"github.com/licoy/quosparr/internal/store"
)

func TestAccountTabsListEveryProviderInOrder(t *testing.T) {
	want := []store.Provider{store.ProviderCodex, store.ProviderClaude, store.ProviderGrok, store.ProviderZai, store.ProviderCursor, store.ProviderKimi, store.ProviderAgy, store.ProviderDeepSeek, store.ProviderMiniMax, store.ProviderPixelLab, store.ProviderElevenLabs}
	got := accountTabProviders()
	if len(got) != len(want) {
		t.Fatalf("tabs = %v, want %v", got, want)
	}
	for i, p := range got {
		if p != want[i] {
			t.Fatalf("tab %d = %s, want %s (all: %v)", i, p, want[i], got)
		}
		if accountKindProvider(i) != p {
			t.Fatalf("kind %d maps to %s, want %s", i, accountKindProvider(i), p)
		}
	}
	// The header offers every tab, labeled by the shared provider names.
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	u := newSettingsUI(e.s, SettingsOptions{})
	f := accountFormOf(u)
	l := e.s.L()
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() { accountProviderHeader(c, u, f) })
	}, 1200, 120)
	for _, p := range want {
		if !tt.HasText(l.Text(providerKey(p))) {
			t.Errorf("tab label of %s missing; texts %q", p, tt.Texts())
		}
	}
}

func TestCursorConfigSaveRejectsInvalidToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
	e := newEnv(t, nil)
	rt := newRuntime(BackgroundOptions{Home: home, Settings: e.s, Version: "test"})
	setLive(rt)
	t.Cleanup(func() {
		rt.svc.Close()
		if rt.repo != nil {
			_ = rt.repo.Close()
		}
		setLive(nil)
	})
	u, tt := openSettingsView(t, e, tabAccounts)
	l := e.s.L()
	f := accountFormOf(u)
	f.kind = accountKindCursor
	f.mcfg.cursorToken = "not-a-jwt"
	tt.Frame()
	if err := tt.Click(tx(l, xMeterConfigSave)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.busy {
		t.Fatal("an invalid token started a save job")
	}
	if f.mcfg.err != tx(l, xCursorTokenInvalid) || !tt.HasText(tx(l, xCursorTokenInvalid)) {
		t.Fatalf("rejection missing (err %q); texts %q", f.mcfg.err, tt.Texts())
	}
	if _, err := os.Stat(cursor.ConfigPath(platform.RunwayDir(home))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid token was written: %v", err)
	}
}

func TestKimiConfigSaveWritesTrimmedKeyAndClearRemovesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
	t.Setenv("KIMI_CODE_API_KEY", "")
	t.Setenv("KIMI_SHARE_DIR", "")
	e := newEnv(t, nil)
	rt := newRuntime(BackgroundOptions{Home: home, Settings: e.s, Version: "test"})
	setLive(rt)
	t.Cleanup(func() {
		rt.svc.Close()
		if rt.repo != nil {
			_ = rt.repo.Close()
		}
		setLive(nil)
	})
	root := platform.RunwayDir(home)
	u, tt := openSettingsView(t, e, tabAccounts)
	l := e.s.L()
	f := accountFormOf(u)
	f.kind = accountKindKimi
	f.mcfg.kimiKey = "  sk-kimi-synthetic  "
	tt.Frame()
	if err := tt.Click(tx(l, xMeterConfigSave)); err != nil {
		t.Fatal(err)
	}
	waitMeterConfigIdle(t, e, f)
	tt.Frame()
	key, ok, err := kimi.LoadConfig(root)
	if err != nil || !ok || key != "sk-kimi-synthetic" {
		t.Fatalf("saved config = %q, %v, %v", key, ok, err)
	}
	if f.mcfg.kimiKey != "" {
		t.Fatal("the draft key was kept after saving")
	}
	if !tt.HasText(tx(l, xMeterConfigKeyHint, meter.KeyHint("sk-kimi-synthetic"))) || tt.HasText("sk-kimi-synthetic") {
		t.Fatalf("the status must show the fingerprint only; texts %q", tt.Texts())
	}
	// The remove button asks the OS to confirm; the confirmed path is the
	// shared removal below.
	if !tt.HasText(l.Text(l10n.KeyAccountsDelete)) {
		t.Fatalf("remove action missing; texts %q", tt.Texts())
	}
	if err := rt.removeMeterConfig(store.ProviderKimi); err != nil {
		t.Fatal(err)
	}
	if key, ok, err := kimi.LoadConfig(root); err != nil || ok || key != "" {
		t.Fatalf("config survived the removal: %q, %v, %v", key, ok, err)
	}
}

func TestDeepseekConfigSaveWritesTrimmedKeyAndClearRemovesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
	t.Setenv("DEEPSEEK_API_KEY", "")
	e := newEnv(t, nil)
	rt := newRuntime(BackgroundOptions{Home: home, Settings: e.s, Version: "test"})
	setLive(rt)
	t.Cleanup(func() {
		rt.svc.Close()
		if rt.repo != nil {
			_ = rt.repo.Close()
		}
		setLive(nil)
	})
	root := platform.RunwayDir(home)
	u, tt := openSettingsView(t, e, tabAccounts)
	l := e.s.L()
	f := accountFormOf(u)
	f.kind = accountKindDeepseek
	f.mcfg.deepseekKey = "  sk-deepseek-synthetic  "
	tt.Frame()
	if err := tt.Click(tx(l, xMeterConfigSave)); err != nil {
		t.Fatal(err)
	}
	waitMeterConfigIdle(t, e, f)
	tt.Frame()
	key, ok, err := deepseek.LoadConfig(root)
	if err != nil || !ok || key != "sk-deepseek-synthetic" {
		t.Fatalf("saved config = %q, %v, %v", key, ok, err)
	}
	if f.mcfg.deepseekKey != "" {
		t.Fatal("the draft key was kept after saving")
	}
	if !tt.HasText(tx(l, xMeterConfigKeyHint, meter.KeyHint("sk-deepseek-synthetic"))) || tt.HasText("sk-deepseek-synthetic") {
		t.Fatalf("the status must show the fingerprint only; texts %q", tt.Texts())
	}
	if err := rt.removeMeterConfig(store.ProviderDeepSeek); err != nil {
		t.Fatal(err)
	}
	if key, ok, err := deepseek.LoadConfig(root); err != nil || ok || key != "" {
		t.Fatalf("config survived the removal: %q, %v, %v", key, ok, err)
	}
}

func TestMinimaxConfigSaveWritesKeyAndRegion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
	t.Setenv("MINIMAX_CODING_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")
	e := newEnv(t, nil)
	rt := newRuntime(BackgroundOptions{Home: home, Settings: e.s, Version: "test"})
	setLive(rt)
	t.Cleanup(func() {
		rt.svc.Close()
		if rt.repo != nil {
			_ = rt.repo.Close()
		}
		setLive(nil)
	})
	root := platform.RunwayDir(home)
	u, tt := openSettingsView(t, e, tabAccounts)
	l := e.s.L()
	f := accountFormOf(u)
	f.kind = accountKindMinimax
	f.mcfg.minimaxKey = "  sk-minimax-synthetic  "
	f.mcfg.minimaxRegion = minimax.RegionIntl
	tt.Frame()
	if err := tt.Click(tx(l, xMeterConfigSave)); err != nil {
		t.Fatal(err)
	}
	waitMeterConfigIdle(t, e, f)
	tt.Frame()
	key, region, ok, err := minimax.LoadConfig(root)
	if err != nil || !ok || key != "sk-minimax-synthetic" || region != minimax.RegionIntl {
		t.Fatalf("saved config = %q, %q, %v, %v", key, region, ok, err)
	}
	if !tt.HasText(tx(l, xMinimaxRegionIntl)) {
		t.Fatalf("the status must show the effective region; texts %q", tt.Texts())
	}
	if tt.HasText("sk-minimax-synthetic") {
		t.Fatalf("the status must not show the key; texts %q", tt.Texts())
	}
	// A region outside the two known values is stored as unset, and the
	// status then falls back to the China endpoint.
	f.mcfg.minimaxKey = "sk-minimax-second"
	f.mcfg.minimaxRegion = "bogus"
	if err := tt.Click(tx(l, xMeterConfigSave)); err != nil {
		t.Fatal(err)
	}
	waitMeterConfigIdle(t, e, f)
	tt.Frame()
	key, region, ok, err = minimax.LoadConfig(root)
	if err != nil || !ok || key != "sk-minimax-second" || region != "" {
		t.Fatalf("normalized config = %q, %q, %v, %v", key, region, ok, err)
	}
	if !tt.HasText(tx(l, xMinimaxRegionCn)) {
		t.Fatalf("an unset region must read as China; texts %q", tt.Texts())
	}
	if err := rt.removeMeterConfig(store.ProviderMiniMax); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := minimax.LoadConfig(root); err != nil || ok {
		t.Fatalf("config survived the removal: %v, %v", ok, err)
	}
}

func TestPixellabConfigSaveWritesTrimmedKeyAndClearRemovesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
	t.Setenv("PIXELLAB_API_KEY", "")
	e := newEnv(t, nil)
	rt := newRuntime(BackgroundOptions{Home: home, Settings: e.s, Version: "test"})
	setLive(rt)
	t.Cleanup(func() {
		rt.svc.Close()
		if rt.repo != nil {
			_ = rt.repo.Close()
		}
		setLive(nil)
	})
	root := platform.RunwayDir(home)
	u, tt := openSettingsView(t, e, tabAccounts)
	l := e.s.L()
	f := accountFormOf(u)
	f.kind = accountKindPixellab
	f.mcfg.pixellabKey = "  sk-pixellab-synthetic  "
	tt.Frame()
	if err := tt.Click(tx(l, xMeterConfigSave)); err != nil {
		t.Fatal(err)
	}
	waitMeterConfigIdle(t, e, f)
	tt.Frame()
	key, ok, err := pixellab.LoadConfig(root)
	if err != nil || !ok || key != "sk-pixellab-synthetic" {
		t.Fatalf("saved config = %q, %v, %v", key, ok, err)
	}
	if !tt.HasText(tx(l, xMeterConfigKeyHint, meter.KeyHint("sk-pixellab-synthetic"))) || tt.HasText("sk-pixellab-synthetic") {
		t.Fatalf("the status must show the fingerprint only; texts %q", tt.Texts())
	}
	if err := rt.removeMeterConfig(store.ProviderPixelLab); err != nil {
		t.Fatal(err)
	}
	if key, ok, err := pixellab.LoadConfig(root); err != nil || ok || key != "" {
		t.Fatalf("config survived the removal: %q, %v, %v", key, ok, err)
	}
}

func TestElevenlabsConfigSaveWritesTrimmedKeyAndClearRemovesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
	t.Setenv("ELEVENLABS_API_KEY", "")
	e := newEnv(t, nil)
	rt := newRuntime(BackgroundOptions{Home: home, Settings: e.s, Version: "test"})
	setLive(rt)
	t.Cleanup(func() {
		rt.svc.Close()
		if rt.repo != nil {
			_ = rt.repo.Close()
		}
		setLive(nil)
	})
	root := platform.RunwayDir(home)
	u, tt := openSettingsView(t, e, tabAccounts)
	l := e.s.L()
	f := accountFormOf(u)
	f.kind = accountKindElevenlabs
	f.mcfg.elevenlabsKey = "  sk-elevenlabs-synthetic  "
	tt.Frame()
	if err := tt.Click(tx(l, xMeterConfigSave)); err != nil {
		t.Fatal(err)
	}
	waitMeterConfigIdle(t, e, f)
	tt.Frame()
	key, ok, err := elevenlabs.LoadConfig(root)
	if err != nil || !ok || key != "sk-elevenlabs-synthetic" {
		t.Fatalf("saved config = %q, %v, %v", key, ok, err)
	}
	if !tt.HasText(tx(l, xMeterConfigKeyHint, meter.KeyHint("sk-elevenlabs-synthetic"))) || tt.HasText("sk-elevenlabs-synthetic") {
		t.Fatalf("the status must show the fingerprint only; texts %q", tt.Texts())
	}
	if err := rt.removeMeterConfig(store.ProviderElevenLabs); err != nil {
		t.Fatal(err)
	}
	if key, ok, err := elevenlabs.LoadConfig(root); err != nil || ok || key != "" {
		t.Fatalf("config survived the removal: %q, %v, %v", key, ok, err)
	}
}

func TestMeterConfigSaveRequiresKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", filepath.Join(home, "grok"))
	e := newEnv(t, nil)
	rt := newRuntime(BackgroundOptions{Home: home, Settings: e.s, Version: "test"})
	setLive(rt)
	t.Cleanup(func() {
		rt.svc.Close()
		if rt.repo != nil {
			_ = rt.repo.Close()
		}
		setLive(nil)
	})
	u := newSettingsUI(e.s, SettingsOptions{})
	f := accountFormOf(u)
	l := e.s.L()
	cases := []struct {
		kind int
		save func()
		want string
	}{
		{accountKindDeepseek, func() { u.saveDeepseekConfig(f, rt) }, tx(l, xDeepseekKeyRequired)},
		{accountKindMinimax, func() { u.saveMinimaxConfig(f, rt) }, tx(l, xMinimaxKeyRequired)},
		{accountKindPixellab, func() { u.savePixellabConfig(f, rt) }, tx(l, xPixellabKeyRequired)},
		{accountKindElevenlabs, func() { u.saveElevenlabsConfig(f, rt) }, tx(l, xElevenlabsKeyRequired)},
	}
	for _, tc := range cases {
		f.kind = tc.kind
		f.mcfg.err = ""
		tc.save()
		if f.mcfg.err != tc.want {
			t.Fatalf("kind %d err = %q, want %q", tc.kind, f.mcfg.err, tc.want)
		}
		if f.busy {
			t.Fatalf("kind %d started a save job without a key", tc.kind)
		}
	}
}

// waitMeterConfigIdle drains the main queue until the form's busy action
// finished.
func waitMeterConfigIdle(t *testing.T, e *env, f *accountForm) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for f.busy && time.Now().Before(deadline) {
		e.q.drain()
		time.Sleep(time.Millisecond)
	}
	e.q.drain()
	if f.busy {
		t.Fatal("the account action did not finish")
	}
}
