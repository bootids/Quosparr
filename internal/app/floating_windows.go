//go:build windows

package app

import (
	"time"
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var (
	procDwmSetWindowAttribute        = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	procSHQueryUserNotificationState = windows.NewLazySystemDLL("shell32.dll").NewProc("SHQueryUserNotificationState")
)

const (
	dwmwaWindowCornerPreference = 33
	dwmwcpRound                 = 2
	// QUERY_USER_NOTIFICATION_STATE values of a full-screen app.
	qunsBusy                 = 2
	qunsRunningD3DFullScreen = 3
	qunsPresentationMode     = 4
)

// floatingTransparent: a window of native UI keeps its redirection bitmap on
// Windows, so the card fills it and Windows 11 rounds its corners.
func floatingTransparent() bool { return false }

func setFloatingOpacity(win *mygo.Window, v float64) { win.SetOpacity(v) }

func setupFloatingWindow(win *mygo.Window, _ bool) {
	hwnd := win.NativeHandle()
	if hwnd == 0 || procDwmSetWindowAttribute.Find() != nil {
		return
	}
	pref := uint32(dwmwcpRound)
	// Windows 10 has no such attribute and keeps square corners.
	procDwmSetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&pref)), unsafe.Sizeof(pref))
}

// fullScreenBusy reports whether a full-screen app, game or presentation
// runs, which an always-on-top window would cover.
func fullScreenBusy() bool {
	if procSHQueryUserNotificationState.Find() != nil {
		return false
	}
	var state int32
	if r, _, _ := procSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&state))); r != 0 {
		return false
	}
	return state == qunsBusy || state == qunsRunningD3DFullScreen || state == qunsPresentationMode
}

// watchFullScreen reports the full-screen state on the main thread as it
// changes, every two seconds, until the returned func is called.
func watchFullScreen(report func(bool)) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		last := false
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if busy := fullScreenBusy(); busy != last {
					last = busy
					mygo.RunOnMain(func() { report(busy) })
				}
			}
		}
	}()
	return func() { close(stop) }
}

// fadeFloatingOpacityNative: no native fade here; the ticker path runs.
func fadeFloatingOpacityNative(*mygo.Window, float64) bool { return false }
