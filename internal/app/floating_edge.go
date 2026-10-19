package app

import (
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/licoy/quosparr/internal/store"
)

// dockPoll is how often the pointer is looked up while the floating window
// is docked at an edge of the display.
const dockPoll = 120 * time.Millisecond

// Animation timing: total slide time and step.
const (
	dockAnimTime = 180 * time.Millisecond
	dockAnimTick = 16 * time.Millisecond
)

// floatingDockSide is the side a drag ends at: the side of area within snap
// of b, the nearer one when both are, and none on a tie or a degenerate area.
// A window dragged past the edge counts as being at it.
func floatingDockSide(b, area mygo.Rectangle, snap int) store.FloatingEdgeSide {
	if area.Width <= 0 {
		return store.FloatingEdgeNone
	}
	left, right := b.X-area.X, (area.X+area.Width)-(b.X+b.Width)
	switch {
	case left <= snap && right <= snap:
		// Both are at the edge: the strictly nearer one, none on a tie.
		if left < right {
			return store.FloatingEdgeLeft
		}
		if right < left {
			return store.FloatingEdgeRight
		}
		return store.FloatingEdgeNone
	case left <= snap:
		return store.FloatingEdgeLeft
	case right <= snap:
		return store.FloatingEdgeRight
	}
	return store.FloatingEdgeNone
}

// floatingDockBounds is b flushed to side of area, size and place across the
// side unchanged; none leaves it as it is.
func floatingDockBounds(b mygo.Rectangle, area mygo.Rectangle, side store.FloatingEdgeSide) mygo.Rectangle {
	switch side {
	case store.FloatingEdgeLeft:
		b.X = area.X
	case store.FloatingEdgeRight:
		b.X = area.X + area.Width - b.Width
	}
	return b
}

// floatingDockHot is b grown by reveal on all four sides: the zone that
// keeps a shown docked window shown. It is shown-state hysteresis only:
// a hidden window's live bounds cannot seed it, because macOS clamps a
// window moved fully off-screen back to a sliver on screen.
func floatingDockHot(b mygo.Rectangle, reveal int) mygo.Rectangle {
	return mygo.Rectangle{
		X:      b.X - reveal,
		Y:      b.Y - reveal,
		Width:  b.Width + 2*reveal,
		Height: b.Height + 2*reveal,
	}
}

// floatingDockNear reports whether p is inside r, edges included.
func floatingDockNear(p mygo.Point, r mygo.Rectangle) bool {
	return p.X >= r.X && p.X <= r.X+r.Width && p.Y >= r.Y && p.Y <= r.Y+r.Height
}

// floatingDockEdgeNear reports whether p is within reveal of the docked
// side of area, anywhere along it: the reveal zone is the pointer's
// distance to the screen edge, not to the window.
func floatingDockEdgeNear(p mygo.Point, area mygo.Rectangle, side store.FloatingEdgeSide, reveal int) bool {
	if p.Y < area.Y || p.Y > area.Y+area.Height {
		return false
	}
	switch side {
	case store.FloatingEdgeLeft:
		d := p.X - area.X
		if d < 0 {
			d = -d
		}
		return d <= reveal
	case store.FloatingEdgeRight:
		d := p.X - (area.X + area.Width)
		if d < 0 {
			d = -d
		}
		return d <= reveal
	default:
		return false
	}
}

// floatingDockRevealed is one poll of the edge watcher: a pointer within
// reveal of the docked edge of the work area reveals the window, and one
// over its bounds grown by reveal keeps a shown window shown. The bounds
// term is shown-only: macOS clamps a window moved fully off-screen back
// to a sliver on screen, so hidden live bounds would reach sliver+reveal
// into the display and trigger the reveal from far away.
func floatingDockRevealed(shown bool, p mygo.Point, b, area mygo.Rectangle, side store.FloatingEdgeSide, reveal int) bool {
	if floatingDockEdgeNear(p, area, side, reveal) {
		return true
	}
	return shown && floatingDockNear(p, floatingDockHot(b, reveal))
}

// floatingDockX is the window's left edge at side of area: flush with the
// edge when shown, just past it off the display when hidden.
func floatingDockX(side store.FloatingEdgeSide, area mygo.Rectangle, width int, shown bool) int {
	switch side {
	case store.FloatingEdgeLeft:
		if shown {
			return area.X
		}
		return area.X - width
	case store.FloatingEdgeRight:
		if shown {
			return area.X + area.Width - width
		}
		return area.X + area.Width
	default:
		return area.X
	}
}

// floatingDockEase eases t in [0,1] cubic out: 1-(1-t)^3, clamped.
func floatingDockEase(t float64) float64 {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return 1 - (1-t)*(1-t)*(1-t)
}

// watchDockEdge reports on the main thread whether the pointer keeps the
// docked window shown: near the docked edge of the sampled work area, or
// — only while it is shown — over the sampled window bounds grown by
// reveal as hysteresis. shown seeds that state, so a watcher restarted
// while the window is shown keeps it shown. The samples are read live on
// the watcher goroutine, so the window may move or resize while it runs.
func watchDockEdge(side store.FloatingEdgeSide, reveal int, shown bool, sample func() (mygo.Rectangle, mygo.Rectangle), report func(near bool)) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(dockPoll)
		defer t.Stop()
		var near, first bool = shown, true
		for {
			b, area := sample()
			p := mygo.Screen.CursorScreenPoint()
			v := floatingDockRevealed(near, p, b, area, side, reveal)
			if first || v != near {
				near, first = v, false
				runOnMain(func() {
					select {
					case <-stop:
					default:
						report(v)
					}
				})
			}
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
	}()
	return func() { close(stop) }
}

// floatingDockKey is what the edge watcher is keyed by: the docked side and
// the reveal distance, empty when edge hiding is off.
func floatingDockKey(f store.FloatingWindow) string {
	if !f.EdgeHide {
		return ""
	}
	return string(f.EdgeSide) + "|" + strconv.Itoa(f.EdgeRevealPx())
}
