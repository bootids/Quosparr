//go:build !darwin && !windows

package app

import "github.com/egoist/mygo"

// floatingTransparent: a compositing desktop shows through the corners.
func floatingTransparent() bool { return true }

func setupFloatingWindow(*mygo.Window, bool) {}

func setFloatingOpacity(win *mygo.Window, v float64) { win.SetOpacity(v) }

// watchFullScreen: Linux desktops raise full-screen windows above the
// others themselves.
func watchFullScreen(func(bool)) func() { return func() {} }

// fadeFloatingOpacityNative: no native fade here; the ticker path runs.
func fadeFloatingOpacityNative(*mygo.Window, float64) bool { return false }
