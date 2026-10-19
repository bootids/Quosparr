package app

import (
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// NSWindowCollectionBehavior bits.
const (
	nsCanJoinAllSpaces    = 1 << 0
	nsStationary          = 1 << 4
	nsIgnoresCycle        = 1 << 6
	nsFullScreenAuxiliary = 1 << 8
)

// floatingTransparent: the window shows the desktop around its rounded card.
func floatingTransparent() bool { return true }

// setFloatingOpacity sets the floating window's opacity. It sends
// setAlphaValue: itself: the framework's float bridge dropped the dock
// fade's rapid updates on darwin, leaving the window near transparent.
func setFloatingOpacity(win *mygo.Window, v float64) {
	if w := objc.ID(win.NativeHandle()); w != 0 {
		w.Send(objc.RegisterName("setAlphaValue:"), v)
	}
}

// fadeFloatingOpacityNative fades the window's opacity through AppKit
// itself: the animator proxy runs the fade in Core Animation, one message
// instead of a write cadence, which darwin drops.
func fadeFloatingOpacityNative(win *mygo.Window, op float64) bool {
	w := objc.ID(win.NativeHandle())
	if w == 0 {
		return false
	}
	runOnMain(func() {
		ctx := objc.ID(objc.GetClass("NSAnimationContext"))
		if ctx == 0 {
			w.Send(objc.RegisterName("setAlphaValue:"), op)
			return
		}
		ctx.Send(objc.RegisterName("beginGrouping"))
		cur := objc.Send[objc.ID](ctx, objc.RegisterName("currentContext"))
		cur.Send(objc.RegisterName("setDuration:"), float64(dockAnimTime)/float64(time.Second))
		anim := objc.Send[objc.ID](w, objc.RegisterName("animator"))
		anim.Send(objc.RegisterName("setAlphaValue:"), op)
		ctx.Send(objc.RegisterName("endGrouping"))
	})
	return true
}

// setupFloatingWindow keeps the window on every Space, out of Mission
// Control and the window cycle, and, when asked, over full-screen apps,
// whose Spaces it otherwise stays out of.
func setupFloatingWindow(win *mygo.Window, overFullScreen bool) {
	w := objc.ID(win.NativeHandle())
	if w == 0 {
		return
	}
	behavior := uint64(nsCanJoinAllSpaces | nsStationary | nsIgnoresCycle)
	if overFullScreen {
		behavior |= nsFullScreenAuxiliary
	}
	w.Send(objc.RegisterName("setCollectionBehavior:"), behavior)
}

// watchFullScreen: full-screen apps have Spaces of their own on macOS, which
// the window does not join unless set up for it.
func watchFullScreen(func(bool)) func() { return func() {} }
