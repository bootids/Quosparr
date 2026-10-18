package app

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/zai"
)

func zaiTestRuntime(t *testing.T) (*liveApp, *zai.Store) {
	t.Helper()
	root := t.TempDir()
	st, err := zai.LoadStore(filepath.Join(root, "quosparr"))
	if err != nil {
		t.Fatal(err)
	}
	return &liveApp{zaiStore: st}, st
}

func waitZaiIdle(t *testing.T, e *env, f *accountForm) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for f.busy && time.Now().Before(deadline) {
		e.q.drain()
		time.Sleep(time.Millisecond)
	}
	e.q.drain()
	if f.busy {
		t.Fatal("account action did not finish")
	}
}

func zaiPage(t *testing.T, u *settingsUI, f *accountForm, rt *liveApp) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		applyTheme(c)
		ui.Column(c).Fill().Padding(16).Children(func() { renderZaiAccounts(c, u, f, rt) })
	}, 700, 550)
	return tt
}

func TestZaiAddWritesStoreAndRowAppears(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	u := newSettingsUI(e.s, SettingsOptions{})
	f := accountFormOf(u)
	f.kind = accountKindZai
	rt, st := zaiTestRuntime(t)
	setLive(rt)
	t.Cleanup(func() { setLive(nil) })
	tt := zaiPage(t, u, f, rt)
	f.zai = zaiDraft{label: "Team", key: "dummy-zai-key", region: zai.RegionIntl, scheme: zai.SchemeRaw}
	f.mode = modeZaiAdd
	u.submitZaiEditor(f)
	waitZaiIdle(t, e, f)
	rows, err := st.Rows()
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v err = %v", rows, err)
	}
	if rows[0].Label != "Team" || rows[0].Region != zai.RegionIntl || rows[0].AuthScheme != zai.SchemeRaw {
		t.Fatalf("stored row = %+v", rows[0])
	}
	if msg := rt.zaiSetCurrent(u.L(), rows[0].ID); msg != "" {
		t.Fatalf("set current failed: %s", msg)
	}
	tt.Frame()
	if !tt.HasText("Team") || !tt.HasText(u.text(l10n.KeyAccountsCurrent)) {
		t.Fatal("row or current marker missing after add")
	}
}
func TestZaiEditKeepsKeyWhenEmpty(t *testing.T) {
	_, st := zaiTestRuntime(t)
	id, err := st.Add("Team", "dummy-zai-key", zai.RegionIntl, zai.SchemeRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCurrent(id); err != nil {
		t.Fatal(err)
	}
	if err := st.Update(id, "Renamed", nil, zai.RegionCN, zai.SchemeBearer); err != nil {
		t.Fatal(err)
	}
	rows, err := st.Rows()
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v err = %v", rows, err)
	}
	if rows[0].Label != "Renamed" || rows[0].Region != zai.RegionCN || rows[0].AuthScheme != zai.SchemeBearer {
		t.Fatalf("row after edit = %+v", rows[0])
	}
	if key, err := st.LoadKey(id); err != nil || key != "dummy-zai-key" {
		t.Fatalf("key changed by empty edit: %q err = %v", key, err)
	}
}

func TestZaiSetCurrentAndDelete(t *testing.T) {
	l := l10n.New(l10n.English)
	rt, st := zaiTestRuntime(t)
	a, err := st.Add("A", "dummy-zai-key-a", zai.RegionIntl, zai.SchemeRaw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.Add("B", "dummy-zai-key-b", zai.RegionIntl, zai.SchemeRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCurrent(a); err != nil {
		t.Fatal(err)
	}
	if msg := rt.zaiSetCurrent(l, b); msg != "" {
		t.Fatalf("set current failed: %s", msg)
	}
	if cur, err := st.Current(); err != nil || cur != b {
		t.Fatalf("current = %q err = %v", cur, err)
	}
	if msg := rt.removeZai(l, b); msg != "" {
		t.Fatalf("delete failed: %s", msg)
	}
	rows, err := st.Rows()
	if err != nil || len(rows) != 1 || rows[0].ID != a {
		t.Fatalf("rows after delete = %+v err = %v", rows, err)
	}
}

func TestZaiEditorRender(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	u := newSettingsUI(e.s, SettingsOptions{})
	f := accountFormOf(u)
	f.kind = accountKindZai
	rt, _ := zaiTestRuntime(t)
	f.zai = zaiDraft{label: "Team", key: "dummy-zai-key", region: zai.RegionCN, scheme: zai.SchemeBearer, editID: "x"}
	f.mode = modeZaiEdit
	tt := zaiPage(t, u, f, rt)
	l := u.L()
	for _, want := range []string{tx(l, xAPIEdit), l.Text(l10n.KeyZaiApiKeyLabel), l.Text(l10n.KeyZaiRegionCn), l.Text(l10n.KeyZaiAuthSchemeBearer)} {
		if !tt.HasText(want) {
			t.Fatalf("editor text %q missing", want)
		}
	}
	if tt.HasText("dummy-zai-key") {
		t.Fatal("editor rendered the key")
	}
}

func TestZaiTabProviders(t *testing.T) {
	if got := accountTabProviders(); !slices.Equal(got, []store.Provider{store.ProviderCodex, store.ProviderClaude, store.ProviderGrok, store.ProviderZai, store.ProviderCursor, store.ProviderKimi, store.ProviderAgy}) {
		t.Fatalf("accountTabProviders() = %v", got)
	}
}
