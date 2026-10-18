package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/remote"
	"github.com/licoy/quosparr/internal/store"
)

// remoteDraftState is the remote data source form: the enabled switch, the
// ssh target and the test-connection task. The draft loads from the saved
// preferences on the page's first frame.
type remoteDraftState struct {
	loaded  bool
	enabled bool
	host    string
	port    string
	dir     string
	test    Task
	msg     string
	msgErr  bool
}

// remoteTestTimeout bounds one connection test.
const remoteTestTimeout = 10 * time.Second

// remoteTestConnection is the seam the tests replace; it never runs ssh in
// tests.
var remoteTestConnection = func(ctx context.Context, cfg remote.Config) error {
	ctx, cancel := context.WithTimeout(ctx, remoteTestTimeout)
	defer cancel()
	return remote.New(cfg).TestConnection(ctx)
}

// loadRemoteDraft fills the form from the saved preferences once.
func (r *remoteDraftState) load(p store.Preferences) {
	if r.loaded {
		return
	}
	r.loaded = true
	if p.RemoteSource != nil {
		r.enabled = p.RemoteSource.Enabled
		r.host = p.RemoteSource.Host
		if p.RemoteSource.Port != 0 {
			r.port = strconv.Itoa(p.RemoteSource.Port)
		}
		r.dir = p.RemoteSource.Dir
	}
}

// remoteDraft builds the RemoteSource the form describes. Port is 0 (the
// ssh default) when the field is empty.
func (r *remoteDraftState) source() (*store.RemoteSource, error) {
	port := 0
	if s := strings.TrimSpace(r.port); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 || v > 65535 {
			return nil, errRemotePort
		}
		port = v
	}
	return &store.RemoteSource{
		Enabled: r.enabled,
		Host:    strings.TrimSpace(r.host),
		Port:    port,
		Dir:     strings.TrimSpace(r.dir),
	}, nil
}

var errRemotePort = errors.New("remote source: invalid port")

// remoteDraftError reports whether saving src would fail validation.
func remoteDraftError(prefs store.Preferences, src *store.RemoteSource) error {
	p := prefs.Clone()
	p.RemoteSource = src
	return p.Validate()
}

// remoteUnsaved reports whether the draft differs from the preferences.
func (u *settingsUI) remoteUnsaved() bool {
	src, err := u.remote.source()
	if err != nil {
		return true
	}
	saved := u.s.Prefs().RemoteSource
	switch {
	case saved == nil:
		return src.Enabled || src.Host != "" || src.Port != 0 || src.Dir != ""
	case src.Enabled != saved.Enabled || src.Host != saved.Host || src.Port != saved.Port || src.Dir != saved.Dir:
		return true
	}
	return false
}

// ---- Remote data source page ----

func (u *settingsUI) remotePage(c *ui.Context) {
	s, l, t := u.s, u.L(), c.Theme()
	p := s.Prefs()
	u.remote.load(p)
	ro := s.ReadOnly()
	d := &u.remote
	settingsGroup(c, l.Text(l10n.KeyRemoteSource), l.Text(l10n.KeyRemoteSourceDescription), nil, func(row rowFunc) {
		row(l.Text(l10n.KeyRemoteSourceMode), "", func() {
			type mode int
			const (
				local mode = iota
				remoteOn
			)
			if v, ok := pills(c, "remote-mode", pick(!d.enabled, local, remoteOn), []mode{local, remoteOn}, func(m mode) string {
				if m == local {
					return l.Text(l10n.KeyRemoteSourceLocal)
				}
				return l.Text(l10n.KeyRemoteSourceRemote)
			}, ro); ok {
				d.enabled = v == remoteOn
				d.msg = ""
			}
		})
		if d.enabled {
			row(l.Text(l10n.KeyRemoteSourceHost), "", func() {
				ui.TextInput(c.Key("remote-host"), &d.host).Width(230).Placeholder("user@host").
					Label(l.Text(l10n.KeyRemoteSourceHost)).Disabled(ro)
			})
			row(l.Text(l10n.KeyRemoteSourcePort), "", func() {
				ui.TextInput(c.Key("remote-port"), &d.port).Width(230).Placeholder("22").
					Label(l.Text(l10n.KeyRemoteSourcePort)).Disabled(ro)
			})
			row(l.Text(l10n.KeyRemoteSourceDir), "", func() {
				ui.TextInput(c.Key("remote-dir"), &d.dir).Width(230).Placeholder("~/.quosparr").
					Label(l.Text(l10n.KeyRemoteSourceDir)).Disabled(ro)
			})
		}
		row("", "", func() {
			ui.Text(c, l.Text(l10n.KeyRemoteSourceRemoteExportHint)).FontSize(11).TextColor(t.TextMuted)
		})
		row("", "", func() {
			ui.Text(c, l.Text(l10n.KeyRemoteSourceRestartHint)).FontSize(11).TextColor(t.TextMuted)
		})
		row("", "", func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(2, 0, 8, 0).Children(func() {
				col := t.Success
				if d.msgErr {
					col = t.Danger
				}
				ui.Text(c, d.msg).FontSize(12).TextColor(col).Grow(1).Basis(0)
				test := l.Text(l10n.KeyRemoteSourceTest)
				if d.test.Running {
					test = l.Text(l10n.KeyRemoteSourceTesting)
				}
				if PillButton(c, nil, test, ro || d.test.Running || !d.enabled || strings.TrimSpace(d.host) == "").Clicked() {
					u.testRemoteConnection()
				}
				save := l.Text(l10n.KeyNetworkProxySave)
				if !ro && u.remoteUnsaved() {
					if WarningPillButton(c, save).Clicked() {
						u.saveRemoteSource()
					}
				} else {
					PillButton(c, nil, save, true)
				}
			})
		})
	})
}

// testRemoteConnection runs the ssh probe against the draft; only the
// error category reaches the message line.
func (u *settingsUI) testRemoteConnection() {
	if u.remote.test.Running {
		return
	}
	d := &u.remote
	src, err := d.source()
	if err != nil {
		d.msg, d.msgErr = u.L().Text(l10n.KeyRemoteSourceConnectionFailed), true
		return
	}
	d.msg = ""
	runTask(&d.test, u.post, func() error {
		return remoteTestConnection(context.Background(), remote.Config{Host: src.Host, Port: src.Port, Dir: src.Dir})
	}, func(err error) {
		l := u.L()
		if err == nil {
			d.msg, d.msgErr = l.Text(l10n.KeyRemoteSourceTestSucceeded), false
			return
		}
		// Only the category is shown: the ssh binary missing and a failed
		// connection or login share the connection-failed text; the error
		// itself (exit code, stderr) never reaches the UI.
		d.msg, d.msgErr = l.Text(l10n.KeyRemoteSourceConnectionFailed), true
	})
}

// saveRemoteSource validates the draft, saves it and explains that the
// switch applies after a restart.
func (u *settingsUI) saveRemoteSource() {
	d := &u.remote
	src, err := d.source()
	if err != nil {
		d.msg, d.msgErr = u.L().Text(l10n.KeyRemoteSourceConnectionFailed), true
		return
	}
	if verr := remoteDraftError(u.s.Prefs(), src); verr != nil {
		d.msg, d.msgErr = verr.Error(), true
		return
	}
	if u.s.SetRemoteSource(src) {
		d.msg, d.msgErr = "", false
		u.toast = u.L().Text(l10n.KeyRemoteSourceRestartHint)
	}
}
