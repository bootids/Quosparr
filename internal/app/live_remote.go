package app

import (
	"context"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/licoy/quosparr/internal/alerts"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/remote"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
)

// remoteEnabled reports whether the app runs on the remote data source: no
// local collection exists and everything renders from the remote status file.
func remoteEnabled(prefs store.Preferences) bool {
	return prefs.RemoteSource != nil && prefs.RemoteSource.Enabled && prefs.RemoteSource.Host != ""
}

// serviceRuntime is the method set the app needs from the running
// coordinator, local or remote. Local-only operations go through
// liveApp.localSvc and are gated behind remoteMode.
type serviceRuntime interface {
	Start(context.Context)
	Close()
	Snapshot() runway.Snapshot
	Preferences() store.Preferences
	WidgetSnapshot(now time.Time) export.Snapshot
	SetWidgetRequirements(export.Requirements)
	ReportWidgetError(error)
	SetPanelOpen(open bool)
}

// remoteRuntime adapts *runway.RemoteService to serviceRuntime. The extra
// methods are no-ops: the remote service has no widget requirements, no
// panel-local scans and no alerts. Preferences are kept for the widget
// refresh interval and the widget snapshot choices.
type remoteRuntime struct {
	*runway.RemoteService
	prefsMu sync.Mutex
	prefs   store.Preferences
}

func (r *remoteRuntime) Preferences() store.Preferences {
	r.prefsMu.Lock()
	defer r.prefsMu.Unlock()
	return r.prefs.Clone()
}

// SetPreferences stores the app-side copy and forwards the new preferences
// to the remote service so rendering choices apply without a restart.
func (r *remoteRuntime) SetPreferences(p store.Preferences) {
	r.prefsMu.Lock()
	r.prefs = p.Clone()
	r.prefsMu.Unlock()
	if r.RemoteService != nil {
		r.RemoteService.SetPreferences(p)
	}
}

func (r *remoteRuntime) SetWidgetRequirements(export.Requirements) {}
func (r *remoteRuntime) ReportWidgetError(error)                   {}
func (r *remoteRuntime) SetPanelOpen(bool)                         {}

// remoteMode reports whether this runtime renders remote data only.
func (rt *liveApp) remoteMode() bool { return rt.remoteSvc != nil }

// newRemoteRuntime builds the remote-mode coordinator: an ssh client, the
// remote service and the widget mirror. It creates no local collection, no
// usage index and no alerts.
func newRemoteRuntime(opts BackgroundOptions, prefs store.Preferences, langs []string) *liveApp {
	src := prefs.RemoteSource
	client := remote.New(remote.Config{Host: src.Host, Port: src.Port, Dir: src.Dir})
	return newRemoteRuntimeWithFetcher(opts, prefs, langs, client)
}

// newRemoteRuntimeWithFetcher is the testable core of newRemoteRuntime: the
// fetcher is injected so tests can drive the remote decode without SSH.
func newRemoteRuntimeWithFetcher(opts BackgroundOptions, prefs store.Preferences, langs []string, fetcher runway.RemoteFetcher) *liveApp {
	widgetRequirements := initialWidgetRequirements(opts.Settings)
	widgets := newWidgetCoordinator(opts.Home, widgetRequirements != 0)
	rt := &liveApp{widgets: widgets, version: opts.Version, langs: langs}
	var svc serviceRuntime
	dispatch := &snapshotDispatch{post: mygo.RunOnMain, apply: func(snap runway.Snapshot) {
		if opts.Host != nil && LiveService() == svc {
			publishLive(opts.Host, snap)
		}
	}}
	widgetUI := &snapshotDispatch{post: func(fn func()) { fn() }, apply: func(snap runway.Snapshot) {
		if widgetRequirements != 0 {
			_ = widgets.publish(svc.WidgetSnapshot(time.Now()))
		}
	}}
	rr := &remoteRuntime{prefs: prefs}
	rr.RemoteService = runway.NewRemote(runway.RemoteOptions{
		Fetcher:         fetcher,
		Preferences:     prefs,
		SystemLanguages: langs,
		AlertsPath:      alerts.DefaultPath(opts.Home),
		OnEvent:         func(e runway.Event) { deliverEvent(opts, e) },
		OnSnapshot: func(snap runway.Snapshot) {
			snap = rebuildRemoteSnapshotDetail(snap, rr.RawStatus())
			dispatch.publish(snap)
			widgetUI.publish(snap)
		},
	})
	svc = rr
	rt.svc, rt.remoteSvc = rr, rr
	widgets.onRequirements = rr.SetWidgetRequirements
	widgets.onError = rr.ReportWidgetError
	widgets.interval = func() time.Duration {
		return time.Duration(rr.Preferences().WidgetRefreshIntervalSeconds) * time.Second
	}
	rt.dispatch, rt.widgetDispatch = dispatch, widgetUI
	return rt
}

// rebuildRemoteSnapshotDetail restores what BuildRemoteSnapshot cannot carry
// in the Snapshot itself: the Grok billing detail rebuilt from the exported
// panel mirror, so the popover renders the remote data like the local one.
func rebuildRemoteSnapshotDetail(snap runway.Snapshot, raw export.StatusSnapshot) runway.Snapshot {
	if raw.Panel != nil && raw.Panel.Grok != nil && raw.Panel.Grok.Billing != nil {
		snap.Grok.Detail = grokBillingFromPanel(raw.Panel.Grok.Billing)
	}
	return snap
}

// remotePanelActions is the read-only action set of the remote mode: the
// manual refresh stays and maps to RemoteService.Refresh; every account,
// repair and detail writer stays nil, which disables its control.
func (rt *liveApp) remotePanelActions(s *Settings) PanelActions {
	return PanelActions{
		Refresh: func() { rt.requestRefresh(s, "") },
	}
}

// localService returns the local coordinator: the explicit field, or the
// svc a caller installed directly (tests build runtimes that way).
func (rt *liveApp) localService() *runway.Service {
	if rt.localSvc != nil {
		return rt.localSvc
	}
	if s, ok := rt.svc.(*runway.Service); ok {
		return s
	}
	return nil
}

func (rt *liveApp) reloadAccounts() {
	if ls := rt.localService(); ls != nil {
		ls.ReloadAccounts()
	}
}

func (rt *liveApp) refreshAllAccountQuotas() {
	if ls := rt.localService(); ls != nil {
		<-ls.RefreshAllAccountQuotas()
	}
}

func (rt *liveApp) refreshAccountQuota(id string) <-chan struct{} {
	if ls := rt.localService(); ls != nil {
		return ls.RefreshAccountQuota(id)
	}
	return nil
}

func (rt *liveApp) fullRefresh() {
	if rt.remoteSvc != nil {
		<-rt.remoteSvc.Refresh()
		return
	}
	if ls := rt.localService(); ls != nil {
		<-ls.Refresh(usage.RefreshForce)
	}
}

func (rt *liveApp) switchAccount(ctx context.Context, id string) error {
	ls := rt.localService()
	if ls == nil {
		return errCodexUnavailable
	}
	return ls.SwitchAccount(ctx, id)
}
