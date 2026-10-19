package store

import (
	"encoding/json"
	"slices"
)

// FloatingStyle is how the desktop floating window draws the quotas. Only the
// Go app knows it, so the capsule is the empty value and is not written.
type FloatingStyle string

// FloatingStyle values.
const (
	FloatingStyleCapsule FloatingStyle = ""
	FloatingStyleCard    FloatingStyle = "card"
	FloatingStyleRings   FloatingStyle = "rings"
	FloatingStyleBattery FloatingStyle = "battery"
)

// FloatingStyles returns every valid FloatingStyle.
func FloatingStyles() []FloatingStyle {
	return []FloatingStyle{FloatingStyleCapsule, FloatingStyleCard, FloatingStyleRings, FloatingStyleBattery}
}

// Valid reports whether v is a known value.
func (v FloatingStyle) Valid() bool { return slices.Contains(FloatingStyles(), v) }

// Limits of the floating window opacity, in percent.
const (
	MinFloatingOpacity = 40
	MaxFloatingOpacity = 100
)

// FloatingOpacityOptions are the opacities the settings UI offers.
func FloatingOpacityOptions() []int { return []int{100, 90, 80, 70, 60, 50} }

// Limits of the edge snap and reveal distances, in DIPs.
const (
	MinFloatingEdgeSnap       = 4
	MaxFloatingEdgeSnap       = 64
	DefaultFloatingEdgeSnap   = 16
	MinFloatingEdgeReveal     = 2
	MaxFloatingEdgeReveal     = 48
	DefaultFloatingEdgeReveal = 8
)

// FloatingEdgeSnapOptions are the snap distances the settings UI offers.
func FloatingEdgeSnapOptions() []int { return []int{8, 16, 32, 64} }

// FloatingEdgeRevealOptions are the reveal distances the settings UI offers.
func FloatingEdgeRevealOptions() []int { return []int{4, 8, 16, 32} }

// FloatingWindow is the desktop floating window. It is Go-only: written as
// one object, left out while every field is at its default, and an
// unreadable value is the default.
type FloatingWindow struct {
	Enabled bool          `json:"enabled,omitempty"`
	Style   FloatingStyle `json:"style,omitempty"`
	// Providers are the providers shown, in Providers() order; empty
	// follows the main panel's provider.
	Providers []Provider `json:"providers,omitempty"`
	// Opacity is in percent; 0 stands for fully opaque.
	Opacity int  `json:"opacity,omitempty"`
	Locked  bool `json:"locked,omitempty"`
	// OverFullScreen keeps the window on screen while another app is full
	// screen; otherwise it steps aside.
	OverFullScreen bool `json:"overFullScreen,omitempty"`
	// AppFilter shows or hides the window by the app in front, from Apps.
	AppFilter FloatingAppFilter `json:"appFilter,omitempty"`
	// EdgeHide docks the window at the left or right of the display it is
	// on when a drag ends within EdgeSnap of that side, hiding it until
	// the pointer comes within EdgeReveal of the side.
	EdgeHide bool `json:"edgeHide,omitempty"`
	// EdgeSide is the side last docked at, kept across restarts; empty
	// when not docked. Cleared when edge hiding is off.
	EdgeSide FloatingEdgeSide `json:"edgeSide,omitempty"`
	// EdgeSnap is how far from a side, in DIPs, a drag ends docked; 0 is
	// the default.
	EdgeSnap int `json:"edgeSnap,omitempty"`
	// EdgeReveal is how close to the docked side the pointer comes to
	// show the window again, in DIPs; 0 is the default.
	EdgeReveal int           `json:"edgeReveal,omitempty"`
	Apps       []FloatingApp `json:"apps,omitempty"`
}

// FloatingAppFilter is how the app in front shows or hides the floating
// window. The desktop in front always shows it.
type FloatingAppFilter string

// FloatingAppFilter values.
const (
	FloatingAppFilterOff FloatingAppFilter = ""
	// FloatingAppFilterAllow shows the window only while a listed app is
	// in front.
	FloatingAppFilterAllow FloatingAppFilter = "allow"
	// FloatingAppFilterBlock hides the window while a listed app is in
	// front.
	FloatingAppFilterBlock FloatingAppFilter = "block"
)

// FloatingAppFilters returns every valid FloatingAppFilter.
func FloatingAppFilters() []FloatingAppFilter {
	return []FloatingAppFilter{FloatingAppFilterOff, FloatingAppFilterAllow, FloatingAppFilterBlock}
}

// Valid reports whether v is a known value.
func (v FloatingAppFilter) Valid() bool { return slices.Contains(FloatingAppFilters(), v) }

// MaxFloatingApps is how many apps the list keeps.
const MaxFloatingApps = 64

// FloatingEdgeSide is the screen side the floating window is docked at and
// hides beside, when edge hiding is on.
type FloatingEdgeSide string

// FloatingEdgeSide values: none is the empty zero value and is not written.
const (
	FloatingEdgeNone  FloatingEdgeSide = ""
	FloatingEdgeLeft  FloatingEdgeSide = "left"
	FloatingEdgeRight FloatingEdgeSide = "right"
)

// Valid reports whether v is a known side.
func (v FloatingEdgeSide) Valid() bool {
	return v == FloatingEdgeLeft || v == FloatingEdgeRight
}

// FloatingApp is an app of the floating window's list. ID is what the system
// knows it by: its bundle identifier on macOS (or its path), its
// AppUserModelID, known-folder path or executable on Windows, and its
// desktop file ID or executable on Linux. Path, where it is, draws its icon.
type FloatingApp struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// OpacityPercent is the opacity in percent, 100 for the default.
func (f FloatingWindow) OpacityPercent() int {
	if f.Opacity == 0 {
		return MaxFloatingOpacity
	}

	return f.Opacity
}

// EdgeSnapPx is the snap distance in DIPs, DefaultFloatingEdgeSnap for 0.
func (f FloatingWindow) EdgeSnapPx() int {
	if f.EdgeSnap == 0 {
		return DefaultFloatingEdgeSnap
	}
	return f.EdgeSnap
}

// EdgeRevealPx is the reveal distance in DIPs, DefaultFloatingEdgeReveal for 0.
func (f FloatingWindow) EdgeRevealPx() int {
	if f.EdgeReveal == 0 {
		return DefaultFloatingEdgeReveal
	}
	return f.EdgeReveal
}

func (f FloatingWindow) clone() FloatingWindow {
	f.Providers = slices.Clone(f.Providers)
	f.Apps = slices.Clone(f.Apps)
	return f
}

// normalize drops unknown styles and providers and clamps the opacity, with
// full opacity stored as the default.
func (f *FloatingWindow) normalize() {
	if !f.Style.Valid() {
		f.Style = FloatingStyleCapsule
	}
	f.Providers = canonicalProviders(f.Providers, len(Providers()))
	if len(f.Providers) == 0 {
		f.Providers = nil
	}
	if f.Opacity != 0 {
		f.Opacity = max(MinFloatingOpacity, min(MaxFloatingOpacity, f.Opacity))
	}
	if f.Opacity == MaxFloatingOpacity {
		f.Opacity = 0
	}
	if !f.AppFilter.Valid() {
		f.AppFilter = FloatingAppFilterOff
	}
	if !f.EdgeSide.Valid() {
		f.EdgeSide = FloatingEdgeNone
	}
	if !f.EdgeHide {
		f.EdgeSide = FloatingEdgeNone
	}
	if f.EdgeSnap != 0 {
		f.EdgeSnap = max(MinFloatingEdgeSnap, min(MaxFloatingEdgeSnap, f.EdgeSnap))
	}
	if f.EdgeSnap == DefaultFloatingEdgeSnap {
		f.EdgeSnap = 0
	}
	if f.EdgeReveal != 0 {
		f.EdgeReveal = max(MinFloatingEdgeReveal, min(MaxFloatingEdgeReveal, f.EdgeReveal))
	}
	if f.EdgeReveal == DefaultFloatingEdgeReveal {
		f.EdgeReveal = 0
	}
	f.Apps = canonicalFloatingApps(f.Apps)
}

// canonicalFloatingApps drops apps without an ID and repeated IDs, up to
// MaxFloatingApps.
func canonicalFloatingApps(apps []FloatingApp) []FloatingApp {
	var out []FloatingApp
	seen := map[string]bool{}
	for _, a := range apps {
		if a.ID == "" || seen[a.ID] || len(out) == MaxFloatingApps {
			continue
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	return out
}

// HasApp reports whether the list has the app of id.
func (f FloatingWindow) HasApp(id string) bool {
	return slices.ContainsFunc(f.Apps, func(a FloatingApp) bool { return a.ID == id })
}

// FloatingSelection is what the floating window shows: the chosen
// providers, or the main panel's.
func (p Preferences) FloatingSelection() []Provider {
	if sel := canonicalProviders(p.FloatingWindow.Providers, len(Providers())); len(sel) > 0 {
		return sel
	}
	if p.SelectedProvider.Valid() {
		return []Provider{p.SelectedProvider}
	}
	return []Provider{ProviderCodex}
}

// readFloatingWindow decodes the Go-only floating window leniently, as
// readThemeColor: an unreadable object is the default.
func readFloatingWindow(r *reader, key string) FloatingWindow {
	raw, ok := r.take(key)
	var f FloatingWindow
	if !ok || json.Unmarshal(raw, &f) != nil {
		return FloatingWindow{}
	}
	f.normalize()
	return f
}
