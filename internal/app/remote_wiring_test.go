package app

import (
	"context"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/remote"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

func TestRemoteEnabled(t *testing.T) {
	cases := []struct {
		name  string
		prefs store.Preferences
		want  bool
	}{
		{"no source", store.Preferences{}, false},
		{"disabled", store.Preferences{RemoteSource: &store.RemoteSource{Host: "host"}}, false},
		{"no host", store.Preferences{RemoteSource: &store.RemoteSource{Enabled: true}}, false},
		{"enabled", store.Preferences{RemoteSource: &store.RemoteSource{Enabled: true, Host: "user@host"}}, true},
	}
	for _, c := range cases {
		if got := remoteEnabled(c.prefs); got != c.want {
			t.Errorf("%s: remoteEnabled = %v, want %v", c.name, got, c.want)
		}
	}
}

// waitForRemoteTask drains the main queue until the task's completion (and
// its message) has been delivered.
func waitForRemoteTask(t *testing.T, e *env, task *Task) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e.q.drain()
		if !task.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("remote test task did not finish")
}

func TestRemoteDraftSource(t *testing.T) {
	d := &remoteDraftState{enabled: true, host: "  user@host  ", port: "2222", dir: " /srv/quosparr "}
	src, err := d.source()
	if err != nil {
		t.Fatal(err)
	}
	if !src.Enabled || src.Host != "user@host" || src.Port != 2222 || src.Dir != "/srv/quosparr" {
		t.Fatalf("source = %+v", src)
	}
	src, err = (&remoteDraftState{}).source()
	if err != nil || src == nil || src.Port != 0 {
		t.Fatalf("empty draft: %+v, %v", src, err)
	}
	for _, bad := range []string{"0", "65536", "-1", "ssh"} {
		if _, err := (&remoteDraftState{port: bad}).source(); err == nil {
			t.Errorf("port %q accepted", bad)
		}
	}
}

func TestRemoteDraftValidation(t *testing.T) {
	base := store.DefaultPreferences()
	if err := remoteDraftError(base, &store.RemoteSource{Enabled: true, Host: "host"}); err != nil {
		t.Errorf("valid source rejected: %v", err)
	}
	if err := remoteDraftError(base, &store.RemoteSource{Enabled: true}); err == nil {
		t.Error("enabled source without a host passed validation")
	}
	if err := remoteDraftError(base, &store.RemoteSource{Port: 70000}); err == nil {
		t.Error("out-of-range port passed validation")
	}
}

func TestRemoteTestConnectionStateMachine(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	u := newSettingsUI(e.s, SettingsOptions{})
	prev := remoteTestConnection
	called := 0
	remoteTestConnection = func(context.Context, remote.Config) error {
		called++
		return remote.ErrConnectionFailed
	}
	t.Cleanup(func() { remoteTestConnection = prev })

	u.remote.load(e.s.Prefs())
	u.remote.enabled, u.remote.host = true, "user@host"
	u.testRemoteConnection()
	waitForRemoteTask(t, e, &u.remote.test)
	e.q.drain()
	if called != 1 {
		t.Fatalf("test connection ran %d times", called)
	}
	if u.remote.test.Running {
		t.Error("task still running")
	}
	if !u.remote.msgErr || u.remote.msg != e.s.L().Text(l10n.KeyRemoteSourceConnectionFailed) {
		t.Fatalf("msg = %q (err %v)", u.remote.msg, u.remote.msgErr)
	}

	// A second click while idle runs again and can succeed.
	remoteTestConnection = func(context.Context, remote.Config) error { return nil }
	u.testRemoteConnection()
	waitForRemoteTask(t, e, &u.remote.test)
	e.q.drain()
	if u.remote.msgErr || u.remote.msg != e.s.L().Text(l10n.KeyRemoteSourceTestSucceeded) {
		t.Fatalf("success msg = %q (err %v)", u.remote.msg, u.remote.msgErr)
	}
}

func TestRemoteSaveDraftStoresPreferences(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	u := newSettingsUI(e.s, SettingsOptions{})
	u.remote.load(e.s.Prefs())
	u.remote.enabled, u.remote.host, u.remote.port = true, "user@host", "2222"
	if !u.remoteUnsaved() {
		t.Fatal("draft reported saved")
	}
	u.saveRemoteSource()
	e.settle()
	got := e.stored().RemoteSource
	if got == nil || !got.Enabled || got.Host != "user@host" || got.Port != 2222 {
		t.Fatalf("stored remote source = %+v", got)
	}
	if u.remote.msgErr {
		t.Fatalf("save message = %q", u.remote.msg)
	}

	// An invalid draft shows an error and saves nothing.
	u.remote.host, u.remote.enabled = "", true
	u.saveRemoteSource()
	if !u.remote.msgErr {
		t.Error("invalid draft saved without an error")
	}
}

func TestRemoteModeGatesLocalActions(t *testing.T) {
	rt := &liveApp{remoteSvc: &remoteRuntime{}}
	if !rt.remoteMode() {
		t.Fatal("runtime not in remote mode")
	}
	if (&liveApp{}).remoteMode() {
		t.Fatal("empty runtime counted as remote")
	}

	var actions PanelActions
	installAccountActions(rt, nil, &actions)
	if actions.Accounts.Switch != nil || actions.Accounts.Delete != nil || actions.Accounts.RefreshAll != nil {
		t.Fatal("remote mode installed account writers")
	}

	installDetailActions(rt, &actions)
	if actions.QueryCost != nil || actions.QueryReaction != nil || actions.RepairSessions != nil {
		t.Fatal("remote mode installed detail writers")
	}
	if actions.Skills.Query == nil || actions.Skills.ReadBody == nil {
		t.Error("remote mode dropped read-only skills actions")
	}
	if actions.Skills.SetEnabled != nil || actions.Skills.Delete != nil || actions.Skills.Restore != nil || actions.Skills.EmptyTrash != nil {
		t.Error("remote mode installed skills writers")
	}

	panel := rt.remotePanelActions(nil)
	if panel.Refresh == nil {
		t.Error("manual refresh missing in remote mode")
	}
	if panel.SwitchAccount != nil {
		t.Error("remote mode installed account switching")
	}
}

func TestRemoteRuntimePreferenceBridge(t *testing.T) {
	rr := &remoteRuntime{prefs: store.DefaultPreferences()}
	p := rr.Preferences()
	p.Language = "fr"
	if rr.Preferences().Language == "fr" {
		t.Error("Preferences returned the caller's copy")
	}
	p = store.DefaultPreferences()
	p.Language = "fr"
	rr.SetPreferences(p)
	if rr.Preferences().Language != "fr" {
		t.Error("SetPreferences did not store the copy")
	}
	rr.SetWidgetRequirements(0)
	rr.ReportWidgetError(nil)
	rr.SetPanelOpen(true)
}

func TestRemoteRuntimeImplementsServiceRuntime(t *testing.T) {
	var _ serviceRuntime = &remoteRuntime{}
	var _ serviceRuntime = (*runway.Service)(nil)
}
