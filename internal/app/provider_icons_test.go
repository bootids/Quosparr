package app

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

func TestProviderIconsPreserveColorAndFollowTheme(t *testing.T) {
	for _, data := range [][]byte{
		codexProviderSVG, grokProviderSVG, claudeProviderSVG,
		cursorProviderSVG, kimiMonoProviderSVG, zaiProviderSVG, agyProviderSVG,
		deepseekProviderSVG, minimaxProviderSVG, pixellabProviderSVG, elevenlabsProviderSVG,
	} {
		svg, err := ui.ParseSVG(data)
		if err != nil {
			t.Fatal(err)
		}
		if w, h := svg.Size(); w <= 0 || w != h {
			t.Fatalf("provider intrinsic size is %v×%v", w, h)
		}
	}
	// Every provider's logo must stay readable on both themes: the color
	// artwork keeps its palette, the monochrome-only marks (grok, cursor,
	// kimi, zai, elevenlabs) follow the text color like their call sites do.
	monoMarks := map[store.Provider]bool{
		store.ProviderGrok: true, store.ProviderCursor: true,
		store.ProviderKimi: true, store.ProviderZai: true, store.ProviderElevenLabs: true,
	}
	providers := append([]store.Provider{}, store.Providers()...)
	for _, dark := range []bool{false, true} {
		tt := ui.NewTester(func(c *ui.Context) {
			theme := applyTheme(c)
			ui.Row(c).Fill().Background(theme.Background).Gap(6).Padding(6).Children(func() {
				for _, p := range providers {
					icon := ProviderIcon(c, p, 32).Label(string(p))
					if monoMarks[p] {
						icon.TextColor(theme.Text)
					}
				}
			})
		}, 560, 56)
		tt.SetDark(dark)
		bg := tt.Image().RGBAAt(0, 0)
		for _, p := range providers {
			box, ok := tt.Find(string(p))
			if !ok {
				t.Fatalf("icon of %s not found", p)
			}
			contrast := 0
			for y := int(box.Y); y < int(box.Y+box.H); y++ {
				for x := int(box.X); x < int(box.X+box.W); x++ {
					px := tt.Image().RGBAAt(x, y)
					for _, d := range []int{int(px.R) - int(bg.R), int(px.G) - int(bg.G), int(px.B) - int(bg.B)} {
						if d > 100 || d < -100 {
							contrast++
							break
						}
					}
				}
			}
			if contrast < 60 {
				t.Fatalf("%s is not readable with dark=%v (%d contrast pixels)", p, dark, contrast)
			}
		}
	}
	// The Codex gradient must survive rendering, not just parsing.
	tt := ui.NewTester(func(c *ui.Context) {
		theme := applyTheme(c)
		ui.Row(c).Fill().Background(theme.Background).Gap(12).Padding(12).Children(func() {
			ProviderIcon(c, store.ProviderCodex, 48).Label("codex")
		})
	}, 96, 72)
	codexBox, _ := tt.Find("codex")
	if coloredProviderPixels(tt.Image(), codexBox) < 100 {
		t.Fatal("Codex lost its official gradient")
	}
}

func coloredProviderPixels(img *image.RGBA, box ui.Rect) int {
	n := 0
	for y := int(box.Y); y < int(box.Y+box.H); y++ {
		for x := int(box.X); x < int(box.X+box.W); x++ {
			px := img.RGBAAt(x, y)
			if int(px.B)-int(px.R) > 35 {
				n++
			}
		}
	}
	return n
}

func TestProviderTabsSelectDisableAndCompactNames(t *testing.T) {
	l := l10n.New(l10n.English)
	labels := []string{l.Text(l10n.KeyProviderCodex), l.Text(l10n.KeyProviderClaude)}
	for _, compact := range []bool{false, true} {
		selected, changes, disabled := 0, 0, false
		args := labels
		if compact {
			args = nil
		}
		tt := ui.NewTester(func(c *ui.Context) {
			applyTheme(c)
			if ProviderTabs(c, &selected, disabled, args...) {
				changes++
			}
		}, 400, 40)
		if err := tt.Click(labels[1]); err != nil {
			t.Fatal(err)
		}
		if selected != 1 || changes != 1 {
			t.Fatalf("click state selected=%d, changes=%d", selected, changes)
		}
		tt.Key(0, ui.KeyLeft)
		if selected != 0 || changes != 2 {
			t.Fatalf("keyboard state selected=%d, changes=%d", selected, changes)
		}
		disabled = true
		tt.Frame()
		_ = tt.Click(labels[1])
		if selected != 0 || changes != 2 {
			t.Fatal("disabled provider tab changed selection")
		}
		if r, ok := tt.Find(labels[1]); !ok || r.X+r.W > 400 {
			t.Fatalf("provider tab outside compact panel: %+v", r)
		}
	}
}

func TestProviderIconPreview(t *testing.T) {
	dir := os.Getenv("RUNWAY_PROVIDER_QA_DIR")
	if dir == "" {
		t.Skip("set RUNWAY_PROVIDER_QA_DIR to export provider previews")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	l := l10n.New(l10n.English)
	for _, dark := range []bool{false, true} {
		selected := 0
		tt := ui.NewTester(func(c *ui.Context) {
			theme := applyTheme(c)
			ui.Column(c).Fill().Padding(20).Gap(16).Background(theme.Background).Children(func() {
				ui.Row(c).Gap(18).Children(func() {
					for _, provider := range append(store.Providers(), store.Provider("unknown")) {
						ProviderIcon(c, provider, 48).TextColor(theme.Text)
					}
				})
				ProviderTabs(c.Key("labels"), &selected, false, l.Text(l10n.KeyProviderCodex), l.Text(l10n.KeyProviderGrok))
				ProviderTabs(c.Key("compact"), &selected, false)
				ProviderTabs(c.Key("disabled"), &selected, true, l.Text(l10n.KeyProviderCodex), l.Text(l10n.KeyProviderGrok))
			})
		}, 820, 240)
		tt.SetDark(dark)
		name := "provider-icons-light.png"
		if dark {
			name = "provider-icons-dark.png"
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("write preview: %v %v", err, closeErr)
		}
	}
}
