package app

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

// floatingMargin is how far from the right of the work area the window
// first shows, vertically centered.
const floatingMargin = 32

// floatingState is the desktop floating window: an always-on-top card with
// the quotas, which a click opens the panel from and a drag moves.
type floatingState struct {
	win  *mygo.Window
	w, h int
	// overFullScreen is the setting the window was set up with.
	overFullScreen bool
	stopWatch      func()
	offDisplays    func()
	// fullScreenHidden is true while another app is full screen, appHidden
	// while the app in front hides the window; hidden is whether it is.
	fullScreenHidden bool
	appHidden        bool
	hidden           bool
	// followKey is the filter and the apps followed, empty when not.
	followKey  string
	stopFollow func()
	// docked is the side the window is docked at, none when it is not;
	// dockShown is whether the pointer keeps the docked window shown;
	// dockAway is that its slide past the edge finished, so it hides at
	// the OS level; dockKey is what the edge watcher is keyed by, empty
	// when off.
	docked       store.FloatingEdgeSide
	dockShown    bool
	dockAway     bool
	dockKey      string
	stopDock     func()
	stopDockAnim func()
	// stopFade stops the dock opacity fade, if one runs.
	stopFade func()
	drag     floatingDrag
}

// floatingDrag moves the window from the cursor's screen position, so a
// press that does not move stays a click.
type floatingDrag struct {
	active, moved bool
	cursor        mygo.Point
	x, y          int
}

// SetFloatingWindow changes the floating window settings.
func (s *Settings) SetFloatingWindow(change func(*store.FloatingWindow)) bool {
	return s.Update(func(p *store.Preferences) { change(&p.FloatingWindow) })
}

// floatingContent is what the window shows: the coordinator's view, or the
// chosen providers waiting for it.
func (a *App) floatingContent(prefs store.Preferences) runway.FloatingView {
	v := a.Snapshot().Floating
	if len(v.Providers) > 0 {
		return v
	}
	l := a.L()
	for _, p := range prefs.FloatingSelection() {
		v.Providers = append(v.Providers, runway.FloatingProvider{Provider: p, Name: l.Text(providerKey(p)), Status: "--"})
	}
	return v
}

// syncFloating opens, updates or closes the window after the settings or the
// data changed. It runs on the main thread.
func (a *App) syncFloating() {
	prefs := a.settings.Prefs()
	f := prefs.FloatingWindow
	if !f.Enabled || a.tray == nil || a.startupErr != nil {
		a.closeFloating()
		return
	}
	w, h := a.floatingWindowSize(prefs)
	if a.floating.win == nil {
		a.openFloating(prefs, w, h)
		return
	}
	win := a.floating.win
	if a.floating.overFullScreen != f.OverFullScreen {
		// Changed in place: a new window would lose its place.
		a.setFloatingOverFullScreen(f.OverFullScreen)
	}
	a.syncFloatingFollow(f)
	a.syncFloatingDock(f)
	if w != a.floating.w || h != a.floating.h {
		a.resizeFloating(w, h)
	}
	// A dock fade owns the opacity while it runs; snapping to the setting
	// mid-fade would make the appear or vanish look instant.
	if a.floating.stopFade == nil {
		setFloatingOpacity(win, float64(f.OpacityPercent())/100)
	}
	win.Invalidate()
}

func (a *App) floatingWindowSize(prefs store.Preferences) (int, int) {
	w, h := floatingSize(prefs.FloatingWindow.Style, a.floatingContent(prefs), prefs.FontFamily)
	return int(math.Ceil(float64(w))), int(math.Ceil(float64(h)))
}

func (a *App) openFloating(prefs store.Preferences, w, h int) {
	f := prefs.FloatingWindow
	area := mygo.Screen.PrimaryDisplay().WorkArea
	win := mygo.NewWindow(mygo.WindowOptions{
		StateKey:          "floatingWindow",
		Title:             "Quosparr",
		Width:             w,
		Height:            h,
		UseContentSize:    true,
		X:                 area.X + area.Width - w - floatingMargin,
		Y:                 area.Y + (area.Height-h)/2,
		Hidden:            true,
		Frameless:         true,
		Transparent:       floatingTransparent(),
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		AlwaysOnTop:       true,
		SkipTaskbar:       true,
		Opacity:           float64(f.OpacityPercent()) / 100,
		Content:           ui.View(a.floatingView),
	})
	a.floating.win, a.floating.w, a.floating.h = win, w, h
	// The saved state brings back the last size too; the content decides it.
	win.SetContentSize(w, h)
	win.SetVisibleOnAllWorkspaces(true)
	// The opposite of the setting, so that this sets the window up.
	a.floating.overFullScreen = !f.OverFullScreen
	a.setFloatingOverFullScreen(f.OverFullScreen)
	a.syncFloatingFollow(f)
	a.keepFloatingOnScreen()
	win.OnClosed(func() {
		if a.floating.win == win {
			a.stopFloatingWatchers()
			a.floating = floatingState{}
		}
	})
	a.syncFloatingDock(f)
	a.floating.appHidden = a.floating.followKey != ""
	if a.floating.appHidden {
		// Hidden until the follower says otherwise; it was created hidden.
		a.floating.hidden = true
		return
	}
	// Undocked the window just shows; docked, syncFloatingDock already
	// hid it at its off-screen place, and showing would flash a sliver.
	if a.floating.docked == store.FloatingEdgeNone {
		win.ShowInactive()
	}
}

// setFloatingOverFullScreen lets the window show above full-screen apps,
// or makes it give way to them.
func (a *App) setFloatingOverFullScreen(over bool) {
	win := a.floating.win
	if win == nil || a.floating.overFullScreen == over {
		return
	}
	a.floating.overFullScreen = over
	setupFloatingWindow(win, over)
	if a.floating.stopWatch != nil {
		a.floating.stopWatch()
		a.floating.stopWatch = nil
	}
	if !over {
		a.floating.stopWatch = watchFullScreen(a.floatingFullScreen)
		return
	}
	a.floating.fullScreenHidden = false
	a.showFloatingIfDue()
}

// floatingFollowKey is what the window follows the app in front by, or
// empty when it does not.
func floatingFollowKey(f store.FloatingWindow) string {
	if f.AppFilter == store.FloatingAppFilterOff || !foregroundSupported() {
		return ""
	}
	var b strings.Builder
	b.WriteString(string(f.AppFilter))
	for _, app := range f.Apps {
		b.WriteString("\n" + app.ID + "\x00" + app.Path)
	}
	return b.String()
}

// syncFloatingFollow starts, restarts or stops following the app in front
// as the filter or the apps change.
func (a *App) syncFloatingFollow(f store.FloatingWindow) {
	key := floatingFollowKey(f)
	if key == a.floating.followKey {
		return
	}
	if a.floating.stopFollow != nil {
		a.floating.stopFollow()
		a.floating.stopFollow = nil
	}
	a.floating.followKey = key
	if key == "" {
		a.floating.appHidden = false
		a.showFloatingIfDue()
		return
	}
	f.Apps = slices.Clone(f.Apps)
	a.floating.stopFollow = watchForeground(f, func(visible bool) {
		a.floating.appHidden = !visible
		a.showFloatingIfDue()
	})
}

// syncFloatingDock starts, restarts or stops edge docking as the setting,
// the side last docked at or the reveal distance change. It runs on the
// main thread and is cheap on unchanged input.
func (a *App) syncFloatingDock(f store.FloatingWindow) {
	key := floatingDockKey(f)
	if key == a.floating.dockKey {
		return
	}
	a.stopDockWatch()
	a.stopFloatingDockAnim()
	a.floating.dockKey = key
	if key == "" {
		a.floating.docked = store.FloatingEdgeNone
		a.floating.dockShown = false
		a.floating.dockAway = false
		a.showFloatingIfDue()
		return
	}
	side := f.EdgeSide
	if !side.Valid() {
		side = store.FloatingEdgeNone
	}
	a.floating.docked = side
	a.floating.dockShown = false
	if side != store.FloatingEdgeNone {
		// Places the window at its off-screen hidden place and hides it.
		a.slideFloatingDock(false, false)
		a.startDockWatch(side, f.EdgeRevealPx())
		return
	}
	a.showFloatingIfDue()
}

// stopDockWatch stops the edge watcher, if one runs.
func (a *App) stopDockWatch() {
	if a.floating.stopDock != nil {
		a.floating.stopDock()
		a.floating.stopDock = nil
	}
}

// startDockWatch reports, on the main thread, whether the pointer keeps the
// docked window shown: near the docked edge of the work area, or over the
// window itself while it is shown; it replaces any watcher that ran before.
func (a *App) startDockWatch(side store.FloatingEdgeSide, reveal int) {
	a.stopDockWatch()
	win := a.floating.win
	if win == nil {
		return
	}
	sample := func() (mygo.Rectangle, mygo.Rectangle) {
		b := win.Bounds()
		area := mygo.Screen.DisplayNearestPoint(mygo.Point{X: b.X + b.Width/2, Y: b.Y + b.Height/2}).WorkArea
		return b, area
	}
	a.floating.stopDock = watchDockEdge(side, reveal, a.floating.dockShown, sample, func(near bool) {
		a.floating.dockShown = near
		a.slideFloatingDock(near, true)
	})
}

// dockFloatingRelease ends a drag with edge hiding on: it docks the window
// at the side the drag ends near, or just keeps it on screen.
func (a *App) dockFloatingRelease(f store.FloatingWindow) {
	win := a.floating.win
	if win == nil {
		return
	}
	b := win.Bounds()
	area := mygo.Screen.DisplayNearestPoint(mygo.Point{X: b.X + b.Width/2, Y: b.Y + b.Height/2}).WorkArea
	side := floatingDockSide(b, area, f.EdgeSnapPx())
	// The dock state goes in before the preference write: writing runs the
	// settings listeners, whose syncFloatingDock would see a stale dock
	// key, hide the just-docked window off-screen and turn the snap below
	// into a vanish-and-reappear. With the key already set, that sync is
	// a no-op and the release owns the window.
	f.EdgeSide = side
	a.floating.docked, a.floating.dockKey = side, floatingDockKey(f)
	a.settings.SetFloatingWindow(func(p *store.FloatingWindow) { p.EdgeSide = side })
	if side == store.FloatingEdgeNone {
		a.floating.dockShown = false
		a.floating.dockAway = false
		a.stopDockWatch()
		a.stopFloatingDockAnim()
		a.keepFloatingOnScreen()
		a.showFloatingIfDue()
		return
	}
	// The pointer is on the window at release, so it stays shown and the
	// slide snaps it flush to the edge.
	a.floating.dockShown = true
	a.floating.dockAway = false
	a.startDockWatch(side, f.EdgeRevealPx())
	a.slideFloatingDock(true, true)
}

// showFloatingIfDue hides the window while another app is full screen, the
// app in front hides it or its dock slide past the edge finished, and
// shows it again after; dock slides are driven by their callers instead.
func (a *App) showFloatingIfDue() {
	win := a.floating.win
	if win == nil {
		return
	}
	hide := a.floating.fullScreenHidden || a.floating.appHidden || a.floating.dockAway
	if hide == a.floating.hidden {
		return
	}
	a.floating.hidden = hide
	if hide {
		win.Hide()
	} else {
		win.ShowInactive()
	}
}

// resizeFloating fits the window to new content, keeping its vertical
// center and the edge nearer the side of the display it is on; a docked
// window stays at its docked edge instead.
func (a *App) resizeFloating(w, h int) {
	win := a.floating.win
	b := win.Bounds()
	area := mygo.Screen.DisplayMatching(b).WorkArea
	a.floating.w, a.floating.h = w, h
	win.SetContentSize(w, h)
	if a.floating.docked != store.FloatingEdgeNone {
		a.slideFloatingDock(a.floating.dockShown, false)
		return
	}
	nb := win.Bounds()
	x, y := floatingAnchor(b, nb.Width, nb.Height, area)
	nb.X, nb.Y = clampFloating(mygo.Rectangle{X: x, Y: y, Width: nb.Width, Height: nb.Height}, area)
	win.SetPosition(nb.X, nb.Y)
}

// floatingAnchor is where a window at b goes when it becomes w by h: on
// the right half of area it keeps its right edge, else its left edge, and
// it keeps its vertical center.
func floatingAnchor(b mygo.Rectangle, w, h int, area mygo.Rectangle) (int, int) {
	x := b.X
	if b.X+b.Width/2 > area.X+area.Width/2 {
		x = b.X + b.Width - w
	}
	return x, b.Y + (b.Height-h)/2
}

// animateFloatingDock slides the window's X to x over dockAnimTime, eased
// cubic out, replacing any slide that runs — a replaced or stopped slide
// never reports done. It is a no-op without a window or when already at
// x; done, when set, runs on the main thread after the final position is
// set, only when the slide finishes naturally.
func (a *App) animateFloatingDock(x, y int, done func()) {
	win := a.floating.win
	if win == nil {
		return
	}
	b := win.Bounds()
	dx, dy := b.X-x, b.Y-y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx <= 1 && dy <= 1 {
		return
	}
	a.stopFloatingDockAnim()
	from, start := b.X, time.Now()
	stop := make(chan struct{})
	a.floating.stopDockAnim = func() { close(stop) }
	go func() {
		t := time.NewTicker(dockAnimTick)
		defer t.Stop()
		for {
			f := floatingDockEase(float64(time.Since(start)) / float64(dockAnimTime))
			cx := from + int(float64(x-from)*f)
			runOnMain(func() {
				select {
				case <-stop:
				default:
					win.SetPosition(cx, y)
					if f >= 1 && done != nil {
						done()
					}
				}
			})
			if f >= 1 {
				return
			}
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
	}()
}

// stopFloatingDockAnim stops the dock slide, if one runs.
func (a *App) stopFloatingDockAnim() {
	if a.floating.stopDockAnim != nil {
		a.floating.stopDockAnim()
		a.floating.stopDockAnim = nil
	}
}

// animateFloatingOpacity fades the window's opacity from from to op over
// dockAnimTime, eased like the dock slide, replacing any fade that runs.
// It runs its steps on the main thread, like the dock slide, and is a no-op
// without a window or when from == op.
// dockAppearAlpha is where the appear fade starts: not zero, which macOS
// would drop from compositing and so cancel the fade.
const dockAppearAlpha = 0.05

func (a *App) animateFloatingOpacity(from, op float64) {
	win := a.floating.win
	if win == nil || from == op {
		return
	}
	a.stopFloatingDockFade()
	stop := make(chan struct{})
	a.floating.stopFade = func() {
		close(stop)
		// Stopping must not write: a fade-out stopped by a reveal would
		// otherwise post its own zero after the new fade began, leaving
		// the window stuck invisible.
	}
	if fadeFloatingOpacityNative(win, op) {
		// The platform fades on its own; the settings own the opacity
		// again once that fade has had its time.
		time.AfterFunc(dockAnimTime, func() {
			if a.floating.stopFade != nil {
				a.floating.stopFade = nil
			}
		})
		return
	}
	start := time.Now()
	go func() {
		t := time.NewTicker(dockAnimTick)
		defer t.Stop()
		for {
			f := floatingDockEase(float64(time.Since(start)) / float64(dockAnimTime))
			o := from + (op-from)*f
			if f >= 1 {
				o = op
			}
			final := f >= 1
			runOnMain(func() {
				select {
				case <-stop:
				default:
					setFloatingOpacity(win, o)
					if final {
						// The fade is over: the settings own the
						// opacity again. Main-thread serial access
						// makes the clear safe next to a replacing
						// stop, which closes stop first.
						a.floating.stopFade = nil
					}
				}
			})
			if final {
				return
			}
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
	}()
}

// stopFloatingDockFade stops the dock fade, if one runs, snapping the
// window to the value the fade was going for.
func (a *App) stopFloatingDockFade() {
	if a.floating.stopFade != nil {
		a.floating.stopFade()
		a.floating.stopFade = nil
	}
}

// slideFloatingDock puts the docked window at its shown or hidden place at
// the docked edge of the display it is on. Appear and disappear animate
// through opacity, since macOS moves a window that sits fully off-screen
// back on screen when shown, so position alone cannot animate the appear
// there — and that clamp does not place the window flush, so wherever
// showing moved it, it snaps to the flush place and the fade is the appear
// (Windows and Linux keep the hidden window fully off-screen, and the
// position slide brings it in from the docked edge beside the fade). The
// hidden place slides the window past the edge — macOS keeps a sliver on
// screen, so the slide ends by hiding the window — and the hide restores
// the configured opacity while hidden. Without animation everything applies
// at once. A drag owns the window instead.
func (a *App) slideFloatingDock(shown, animate bool) {
	win := a.floating.win
	if win == nil || a.floating.docked == store.FloatingEdgeNone || a.floating.drag.active {
		return
	}
	b := win.Bounds()
	area := mygo.Screen.DisplayNearestPoint(mygo.Point{X: b.X + b.Width/2, Y: b.Y + b.Height/2}).WorkArea
	_, y := clampFloating(b, area)
	target := floatingDockX(a.floating.docked, area, b.Width, shown)
	cur := win.Bounds()
	dx := cur.X - target
	if dx < 0 {
		dx = -dx
	}
	op := float64(a.settings.Prefs().FloatingWindow.OpacityPercent()) / 100
	if shown {
		a.floating.dockAway = false
		a.stopFloatingDockFade()
		// The window appears from nearly invisible: macOS clamps a shown
		// window back on screen, so this fade is the appear animation
		// there, and a fully zero alpha would drop the window out of
		// compositing, which cancels the fade; a small start keeps it in
		// the tree.
		setFloatingOpacity(win, dockAppearAlpha)
		a.showFloatingIfDue()
		// The clamp of a shown window does not place it flush: docked left
		// it lands inset from the edge, and the leftover slide would then
		// come in from the wrong side. Where showing moved the window, the
		// fade at the flush place is the appear; elsewhere (Windows and
		// Linux keep the hidden window fully off-screen) the slide below
		// brings it in from the docked edge.
		if now := win.Bounds(); now.X != cur.X {
			win.SetPosition(target, y)
		}
		a.animateFloatingOpacity(dockAppearAlpha, op)
		switch {
		case animate && dx > 1:
			a.animateFloatingDock(target, y, nil)
		case cur.X != target || cur.Y != y:
			win.SetPosition(target, y)
		}
		return
	}
	// The slide-out may finish after the pointer returns, so the guard
	// keeps a stale completion from hiding a window shown again.
	hide := func() {
		if a.floating.docked != store.FloatingEdgeNone && !a.floating.dockShown {
			a.floating.dockAway = true
			a.showFloatingIfDue()
			setFloatingOpacity(win, op)
		}
	}
	if animate && dx > 1 {
		a.animateFloatingOpacity(op, 0)
		a.animateFloatingDock(target, y, hide)
		return
	}
	if cur.X != target || cur.Y != y {
		win.SetPosition(target, y)
	}
	hide()
	a.stopFloatingDockFade()
	setFloatingOpacity(win, op)
}

func (a *App) stopFloatingWatchers() {
	if a.floating.stopWatch != nil {
		a.floating.stopWatch()
	}
	if a.floating.stopFollow != nil {
		a.floating.stopFollow()
	}
	if a.floating.stopDock != nil {
		a.floating.stopDock()
	}
	a.stopFloatingDockAnim()
	a.stopFloatingDockFade()
	if a.floating.offDisplays != nil {
		a.floating.offDisplays()
	}
}

func (a *App) closeFloating() {
	win := a.floating.win
	if win == nil {
		return
	}
	a.stopFloatingWatchers()
	a.floating = floatingState{}
	win.Close()
}

// floatingFullScreen hides the window while another app is full screen
// (Windows; macOS keeps full-screen apps in Spaces of their own).
func (a *App) floatingFullScreen(busy bool) {
	if a.floating.win == nil {
		return
	}
	a.floating.fullScreenHidden = busy
	a.showFloatingIfDue()
}

// keepFloatingOnScreen moves the window back into the work area of the
// display it is mostly on, after a drag or a display change.
func (a *App) keepFloatingOnScreen() {
	win := a.floating.win
	if win == nil {
		return
	}
	if a.floating.docked != store.FloatingEdgeNone {
		// A docked window sits at its docked edge, hidden or not.
		a.slideFloatingDock(a.floating.dockShown, false)
		return
	}
	b := win.Bounds()
	x, y := clampFloating(b, mygo.Screen.DisplayMatching(b).WorkArea)
	if x != b.X || y != b.Y {
		win.SetPosition(x, y)
	}
}

// clampFloating is the position that keeps b inside area, by the margin
// where it fits.
func clampFloating(b, area mygo.Rectangle) (int, int) {
	if area.Width <= 0 || area.Height <= 0 {
		return b.X, b.Y
	}
	clamp := func(v, lo, size, span int) int {
		hi := lo + span - size
		if hi < lo {
			return lo
		}
		return max(lo, min(hi, v))
	}
	return clamp(b.X, area.X, b.Width, area.Width), clamp(b.Y, area.Y, b.Height, area.Height)
}

// floatingView draws the window and handles the pointer: a click opens the
// panel, a drag moves the window, the secondary button opens a menu.
func (a *App) floatingView(c *ui.Context) {
	prefs := a.settings.Prefs()
	t := applyThemeColor(c, prefs.ThemeColor, prefs.FontFamily)
	c.Root().Background(ui.Transparent)
	v := a.floatingContent(prefs)
	style, family := prefs.FloatingWindow.Style, prefs.FontFamily
	ink := floatingInkFor(t)
	root := ui.Box(c).Fill().Role(ui.RoleButton).Label(floatingTooltip(v)).Tooltip(floatingTooltip(v)).
		Draw(func(p *ui.Painter, r ui.Rect) {
			drawFloating(p, r, style, v, ink, family, floatingCornerRadius(style, r.H))
		})
	root.ContextMenu(func(m *ui.Menu) { a.floatingMenu(m, prefs) })
	// Dragged holds until the button goes up, unlike Pressed, which ends
	// as soon as the pointer gets ahead of the moving window.
	_, _, held := root.Dragged()
	clicked := root.Clicked()
	a.trackFloatingDrag(held, prefs.FloatingWindow)
	if clicked && !a.floating.drag.moved {
		a.floatingClicked()
	}
	if !held {
		a.floating.drag = floatingDrag{}
	}
}

// trackFloatingDrag follows a press: past a few DIPs it is a drag, which
// moves the window unless its position is locked, and docks it at an edge
// of the display when edge hiding is on.
func (a *App) trackFloatingDrag(held bool, f store.FloatingWindow) {
	d, win := &a.floating.drag, a.floating.win
	if win == nil {
		return
	}
	if !held {
		if d.moved && !f.Locked {
			if f.EdgeHide {
				a.dockFloatingRelease(f)
			} else {
				a.keepFloatingOnScreen()
			}
		}
		return
	}
	if !d.active {
		// The window follows the pointer now; a dock slide and fade give
		// way, and an invisible window is never dragged.
		a.stopFloatingDockAnim()
		a.stopFloatingDockFade()
		setFloatingOpacity(win, float64(f.OpacityPercent())/100)
		cursor := mygo.Screen.CursorScreenPoint()
		x, y := win.Position()
		*d = floatingDrag{active: true, cursor: cursor, x: x, y: y}
		return
	}
	cursor := mygo.Screen.CursorScreenPoint()
	dx, dy := cursor.X-d.cursor.X, cursor.Y-d.cursor.Y
	if !d.moved && dx*dx+dy*dy < 9 {
		return
	}
	d.moved = true
	if !f.Locked {
		win.SetPosition(d.x+dx, d.y+dy)
	}
}

// floatingClicked opens the panel, or closes it. The panel closes itself as
// soon as the press takes the focus away, so a click right after that is
// the one that closed it.
func (a *App) floatingClicked() {
	if a.panel != nil {
		a.ClosePanel()
		return
	}
	if time.Since(a.panelClosedAt) < 400*time.Millisecond {
		return
	}
	a.OpenPanel()
}

func (a *App) floatingMenu(m *ui.Menu, prefs store.Preferences) {
	l := a.L()
	if m.Item(l.Text(l10n.KeyOpenControlPanel)).Chosen() {
		a.OpenPanel()
	}
	if m.Item(l.Text(l10n.KeySettings)).Chosen() {
		a.OpenSettingsTab(int(tabFloating))
	}
	m.Separator()
	m.Submenu(tx(l, xFloatingStyle), func(sub *ui.Menu) {
		for _, st := range store.FloatingStyles() {
			if sub.Item(floatingStyleName(l, st)).Checked(prefs.FloatingWindow.Style == st).Chosen() {
				a.settings.SetFloatingWindow(func(f *store.FloatingWindow) { f.Style = st })
			}
		}
	})
	if m.Item(tx(l, xFloatingLocked)).Checked(prefs.FloatingWindow.Locked).Chosen() {
		a.settings.SetFloatingWindow(func(f *store.FloatingWindow) { f.Locked = !prefs.FloatingWindow.Locked })
	}
	m.Separator()
	if m.Item(tx(l, xFloatingHide)).Chosen() {
		a.settings.SetFloatingWindow(func(f *store.FloatingWindow) { f.Enabled = false })
	}
}

func floatingStyleName(l l10n.Localizer, st store.FloatingStyle) string {
	switch st {
	case store.FloatingStyleCard:
		return tx(l, xFloatingStyleCard)
	case store.FloatingStyleRings:
		return tx(l, xFloatingStyleRings)
	case store.FloatingStyleBattery:
		return tx(l, xFloatingStyleBattery)
	}
	return tx(l, xFloatingStyleCapsule)
}
