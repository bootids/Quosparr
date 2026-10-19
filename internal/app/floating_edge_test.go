package app

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/licoy/quosparr/internal/store"
)

func TestFloatingDockSide(t *testing.T) {
	// A display from (0,0) to (1280,720); a 200x120 window.
	area := mygo.Rectangle{X: 0, Y: 0, Width: 1280, Height: 720}
	cases := []struct {
		name string
		b    mygo.Rectangle
		snap int
		want store.FloatingEdgeSide
	}{
		{
			name: "left within snap",
			b:    mygo.Rectangle{X: 8, Y: 100, Width: 200, Height: 120},
			snap: 16,
			want: store.FloatingEdgeLeft,
		},
		{
			name: "right within snap",
			b:    mygo.Rectangle{X: 1080, Y: 100, Width: 200, Height: 120},
			snap: 16,
			want: store.FloatingEdgeRight,
		},
		{
			name: "flush left",
			b:    mygo.Rectangle{X: 0, Y: 100, Width: 200, Height: 120},
			snap: 16,
			want: store.FloatingEdgeLeft,
		},
		{
			name: "away from both sides",
			b:    mygo.Rectangle{X: 500, Y: 100, Width: 200, Height: 120},
			snap: 16,
			want: store.FloatingEdgeNone,
		},
		{
			name: "both within snap takes the nearer side",
			b:    mygo.Rectangle{X: 20, Y: 100, Width: 1220, Height: 120},
			snap: 40,
			want: store.FloatingEdgeLeft,
		},
		{
			name: "both within snap takes the nearer right",
			b:    mygo.Rectangle{X: 30, Y: 100, Width: 1260, Height: 120},
			snap: 40,
			want: store.FloatingEdgeRight,
		},
		{
			name: "both within snap on a tie",
			b:    mygo.Rectangle{X: 20, Y: 100, Width: 1240, Height: 120},
			snap: 40,
			want: store.FloatingEdgeNone,
		},
		{
			name: "dragged past the left edge docks",
			b:    mygo.Rectangle{X: -30, Y: 100, Width: 200, Height: 120},
			snap: 16,
			want: store.FloatingEdgeLeft,
		},
		{
			name: "dragged past the right edge docks",
			b:    mygo.Rectangle{X: 1300, Y: 100, Width: 200, Height: 120},
			snap: 16,
			want: store.FloatingEdgeRight,
		},
		{
			name: "degenerate area",
			b:    mygo.Rectangle{X: 0, Y: 100, Width: 200, Height: 120},
			snap: 16,
			want: store.FloatingEdgeNone,
		},
	}
	for _, tc := range cases {
		a := area
		if tc.name == "degenerate area" {
			a = mygo.Rectangle{X: 0, Y: 0, Width: 0, Height: 720}
		}
		if got := floatingDockSide(tc.b, a, tc.snap); got != tc.want {
			t.Errorf("%s: floatingDockSide = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFloatingDockBounds(t *testing.T) {
	area := mygo.Rectangle{X: 100, Y: 50, Width: 1280, Height: 720}
	cases := []struct {
		name string
		b    mygo.Rectangle
		side store.FloatingEdgeSide
		want mygo.Rectangle
	}{
		{
			name: "flush left keeps size and Y",
			b:    mygo.Rectangle{X: 108, Y: 200, Width: 200, Height: 120},
			side: store.FloatingEdgeLeft,
			want: mygo.Rectangle{X: 100, Y: 200, Width: 200, Height: 120},
		},
		{
			name: "flush right keeps size and Y",
			b:    mygo.Rectangle{X: 1200, Y: 200, Width: 200, Height: 120},
			side: store.FloatingEdgeRight,
			want: mygo.Rectangle{X: 1180, Y: 200, Width: 200, Height: 120},
		},
		{
			name: "none leaves the bounds alone",
			b:    mygo.Rectangle{X: 500, Y: 200, Width: 200, Height: 120},
			side: store.FloatingEdgeNone,
			want: mygo.Rectangle{X: 500, Y: 200, Width: 200, Height: 120},
		},
	}
	for _, tc := range cases {
		if got := floatingDockBounds(tc.b, area, tc.side); got != tc.want {
			t.Errorf("%s: floatingDockBounds = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestFloatingDockHotGrowsOnAllSides(t *testing.T) {
	b := mygo.Rectangle{X: 100, Y: 200, Width: 60, Height: 40}
	want := mygo.Rectangle{X: 92, Y: 192, Width: 76, Height: 56}
	if got := floatingDockHot(b, 8); got != want {
		t.Errorf("floatingDockHot = %+v, want %+v", got, want)
	}
	if got := floatingDockHot(b, 0); got != b {
		t.Errorf("floatingDockHot with no reveal = %+v, want %+v", got, b)
	}
}

func TestFloatingDockNear(t *testing.T) {
	r := mygo.Rectangle{X: 100, Y: 200, Width: 60, Height: 40}
	cases := []struct {
		name string
		p    mygo.Point
		want bool
	}{
		{name: "inside", p: mygo.Point{X: 130, Y: 220}, want: true},
		{name: "top left corner", p: mygo.Point{X: 100, Y: 200}, want: true},
		{name: "bottom right corner", p: mygo.Point{X: 160, Y: 240}, want: true},
		{name: "left of it", p: mygo.Point{X: 99, Y: 220}, want: false},
		{name: "above it", p: mygo.Point{X: 130, Y: 199}, want: false},
		{name: "right of it", p: mygo.Point{X: 161, Y: 220}, want: false},
		{name: "below it", p: mygo.Point{X: 130, Y: 241}, want: false},
	}
	for _, tc := range cases {
		if got := floatingDockNear(tc.p, r); got != tc.want {
			t.Errorf("%s: floatingDockNear = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFloatingDockEdgeNear(t *testing.T) {
	// A display from (100,50) to (1380,770); reveal of 8 px.
	area := mygo.Rectangle{X: 100, Y: 50, Width: 1280, Height: 720}
	reveal := 8
	cases := []struct {
		name string
		p    mygo.Point
		side store.FloatingEdgeSide
		want bool
	}{
		// Left edge: within reveal on either side of it, at any height.
		{name: "left on edge top", p: mygo.Point{X: 100, Y: 50}, side: store.FloatingEdgeLeft, want: true},
		{name: "left on edge bottom", p: mygo.Point{X: 100, Y: 770}, side: store.FloatingEdgeLeft, want: true},
		{name: "left on edge middle", p: mygo.Point{X: 100, Y: 410}, side: store.FloatingEdgeLeft, want: true},
		{name: "left just inside", p: mygo.Point{X: 105, Y: 300}, side: store.FloatingEdgeLeft, want: true},
		{name: "left just outside", p: mygo.Point{X: 95, Y: 300}, side: store.FloatingEdgeLeft, want: true},
		{name: "left at reveal", p: mygo.Point{X: 108, Y: 300}, side: store.FloatingEdgeLeft, want: true},
		{name: "left beyond reveal", p: mygo.Point{X: 109, Y: 300}, side: store.FloatingEdgeLeft, want: false},
		{name: "left beyond reveal outside", p: mygo.Point{X: 91, Y: 300}, side: store.FloatingEdgeLeft, want: false},
		{name: "left above area", p: mygo.Point{X: 100, Y: 49}, side: store.FloatingEdgeLeft, want: false},
		{name: "left below area", p: mygo.Point{X: 100, Y: 771}, side: store.FloatingEdgeLeft, want: false},
		// Right edge mirrored.
		{name: "right on edge top", p: mygo.Point{X: 1380, Y: 50}, side: store.FloatingEdgeRight, want: true},
		{name: "right on edge bottom", p: mygo.Point{X: 1380, Y: 770}, side: store.FloatingEdgeRight, want: true},
		{name: "right just inside", p: mygo.Point{X: 1375, Y: 300}, side: store.FloatingEdgeRight, want: true},
		{name: "right just outside", p: mygo.Point{X: 1385, Y: 300}, side: store.FloatingEdgeRight, want: true},
		{name: "right at reveal", p: mygo.Point{X: 1372, Y: 300}, side: store.FloatingEdgeRight, want: true},
		{name: "right beyond reveal", p: mygo.Point{X: 1371, Y: 300}, side: store.FloatingEdgeRight, want: false},
		{name: "right beyond reveal outside", p: mygo.Point{X: 1389, Y: 300}, side: store.FloatingEdgeRight, want: false},
		{name: "right above area", p: mygo.Point{X: 1380, Y: 49}, side: store.FloatingEdgeRight, want: false},
		{name: "right below area", p: mygo.Point{X: 1380, Y: 771}, side: store.FloatingEdgeRight, want: false},
		// No side never reveals.
		{name: "none on edge", p: mygo.Point{X: 100, Y: 300}, side: store.FloatingEdgeNone, want: false},
	}
	for _, tc := range cases {
		if got := floatingDockEdgeNear(tc.p, area, tc.side, reveal); got != tc.want {
			t.Errorf("%s: floatingDockEdgeNear = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFloatingDockRevealed(t *testing.T) {
	// A display from (100,50) to (1380,770); a 300x120 window docked
	// right; reveal of 4 px.
	area := mygo.Rectangle{X: 100, Y: 50, Width: 1280, Height: 720}
	reveal := 4
	// Hidden, macOS clamps the fully off-screen window (target x 1380)
	// back to a 40 px sliver on screen, like the probe on a real display
	// measured; shown, it sits flush at 1080.
	clamped := mygo.Rectangle{X: 1340, Y: 300, Width: 300, Height: 120}
	flush := mygo.Rectangle{X: 1080, Y: 300, Width: 300, Height: 120}
	cases := []struct {
		name  string
		shown bool
		b     mygo.Rectangle
		p     mygo.Point
		want  bool
	}{
		// Hidden: only the reveal distance to the edge reveals. The
		// clamped sliver the OS leaves on screen must not.
		{name: "hidden over sliver", shown: false, b: clamped, p: mygo.Point{X: 1360, Y: 340}, want: false},
		{name: "hidden deep over clamped window", shown: false, b: clamped, p: mygo.Point{X: 1400, Y: 340}, want: false},
		{name: "hidden within reveal", shown: false, b: clamped, p: mygo.Point{X: 1377, Y: 340}, want: true},
		{name: "hidden within reveal outside", shown: false, b: clamped, p: mygo.Point{X: 1383, Y: 340}, want: true},
		{name: "hidden beyond reveal", shown: false, b: clamped, p: mygo.Point{X: 1370, Y: 340}, want: false},
		// Shown: the window grown by reveal keeps it shown.
		{name: "shown over window", shown: true, b: flush, p: mygo.Point{X: 1200, Y: 340}, want: true},
		{name: "shown within reveal of window", shown: true, b: flush, p: mygo.Point{X: 1078, Y: 340}, want: true},
		{name: "shown past window", shown: true, b: flush, p: mygo.Point{X: 1070, Y: 340}, want: false},
		{name: "shown above window", shown: true, b: flush, p: mygo.Point{X: 1200, Y: 100}, want: false},
		{name: "shown at edge still near", shown: true, b: flush, p: mygo.Point{X: 1379, Y: 340}, want: true},
	}
	for _, tc := range cases {
		if got := floatingDockRevealed(tc.shown, tc.p, tc.b, area, store.FloatingEdgeRight, reveal); got != tc.want {
			t.Errorf("%s: floatingDockRevealed = %v, want %v", tc.name, got, tc.want)
		}
	}
	// Hidden on the left mirrors it: a clamp that keeps 44 px on screen
	// (target x -200 landed at -156) reveals nothing past the reveal.
	leftClamped := mygo.Rectangle{X: -156, Y: 300, Width: 300, Height: 120}
	if floatingDockRevealed(false, mygo.Point{X: 130, Y: 340}, leftClamped, area, store.FloatingEdgeLeft, reveal) {
		t.Error("hidden over left sliver revealed")
	}
	if !floatingDockRevealed(false, mygo.Point{X: 103, Y: 340}, leftClamped, area, store.FloatingEdgeLeft, reveal) {
		t.Error("hidden within reveal of left edge not revealed")
	}
}

func TestFloatingDockX(t *testing.T) {
	area := mygo.Rectangle{X: 100, Y: 50, Width: 1280, Height: 720}
	const width = 200
	cases := []struct {
		name  string
		side  store.FloatingEdgeSide
		shown bool
		want  int
	}{
		{name: "left shown", side: store.FloatingEdgeLeft, shown: true, want: 100},
		{name: "left hidden", side: store.FloatingEdgeLeft, shown: false, want: -100},
		{name: "right shown", side: store.FloatingEdgeRight, shown: true, want: 1180},
		{name: "right hidden", side: store.FloatingEdgeRight, shown: false, want: 1380},
		{name: "none shown", side: store.FloatingEdgeNone, shown: true, want: 100},
		{name: "none hidden", side: store.FloatingEdgeNone, shown: false, want: 100},
	}
	for _, tc := range cases {
		if got := floatingDockX(tc.side, area, width, tc.shown); got != tc.want {
			t.Errorf("%s: floatingDockX = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestFloatingDockEase(t *testing.T) {
	if got := floatingDockEase(0); got != 0 {
		t.Errorf("floatingDockEase(0) = %v, want 0", got)
	}
	if got := floatingDockEase(1); got != 1 {
		t.Errorf("floatingDockEase(1) = %v, want 1", got)
	}
	if got := floatingDockEase(0.5); got != 0.875 {
		t.Errorf("floatingDockEase(0.5) = %v, want 0.875", got)
	}
	if got := floatingDockEase(-0.5); got != 0 {
		t.Errorf("floatingDockEase(-0.5) = %v, want 0", got)
	}
	if got := floatingDockEase(1.5); got != 1 {
		t.Errorf("floatingDockEase(1.5) = %v, want 1", got)
	}
	prev := 0.0
	for i := 0; i <= 20; i++ {
		got := floatingDockEase(float64(i) / 20)
		if got < prev {
			t.Fatalf("floatingDockEase not monotonic at %d: %v after %v", i, got, prev)
		}
		prev = got
	}
}
