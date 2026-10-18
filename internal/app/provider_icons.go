package app

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

//go:embed assets/providers/codex-color.svg
var codexProviderSVG []byte

//go:embed assets/providers/codex.svg
var codexMonoProviderSVG []byte

//go:embed assets/providers/claude.svg
var claudeMonoProviderSVG []byte

//go:embed assets/providers/grok.svg
var grokProviderSVG []byte

//go:embed assets/providers/claude-color.svg
var claudeProviderSVG []byte

//go:embed assets/providers/cursor.svg
var cursorProviderSVG []byte

//go:embed assets/providers/kimi.svg
var kimiMonoProviderSVG []byte

//go:embed assets/providers/zai.svg
var zaiProviderSVG []byte

//go:embed assets/providers/antigravity-color.svg
var agyProviderSVG []byte

//go:embed assets/providers/antigravity.svg
var agyMonoProviderSVG []byte

//go:embed assets/providers/deepseek-color.svg
var deepseekProviderSVG []byte

//go:embed assets/providers/deepseek.svg
var deepseekMonoProviderSVG []byte

//go:embed assets/providers/minimax-color.svg
var minimaxProviderSVG []byte

//go:embed assets/providers/minimax.svg
var minimaxMonoProviderSVG []byte

//go:embed assets/providers/pixellab-color.svg
var pixellabProviderSVG []byte

//go:embed assets/providers/pixellab.svg
var pixellabMonoProviderSVG []byte

//go:embed assets/providers/elevenlabs.svg
var elevenlabsProviderSVG []byte

var (
	// The brands that publish monochrome-only marks.
	cursorProviderIcon     = ui.MustParseSVG(cursorProviderSVG)
	zaiProviderIcon        = ui.MustParseSVG(zaiProviderSVG)
	elevenlabsProviderIcon = ui.MustParseSVG(elevenlabsProviderSVG)
	kimiMonoProviderIcon   = ui.MustParseSVG(kimiMonoProviderSVG)

	codexProviderIcon    = ui.MustParseSVG(codexProviderSVG)
	grokProviderIcon     = ui.MustParseSVG(grokProviderSVG)
	claudeProviderIcon   = ui.MustParseSVG(claudeProviderSVG)
	agyProviderIcon      = ui.MustParseSVG(agyProviderSVG)
	deepseekProviderIcon = ui.MustParseSVG(deepseekProviderSVG)
	minimaxProviderIcon  = ui.MustParseSVG(minimaxProviderSVG)
	pixellabProviderIcon = ui.MustParseSVG(pixellabProviderSVG)

	codexMonoProviderIcon    = ui.MustParseSVG(codexMonoProviderSVG)
	claudeMonoProviderIcon   = ui.MustParseSVG(claudeMonoProviderSVG)
	agyMonoProviderIcon      = ui.MustParseSVG(agyMonoProviderSVG)
	deepseekMonoProviderIcon = ui.MustParseSVG(deepseekMonoProviderSVG)
	minimaxMonoProviderIcon  = ui.MustParseSVG(minimaxMonoProviderSVG)
	pixellabMonoProviderIcon = ui.MustParseSVG(pixellabMonoProviderSVG)
)

// ProviderIcon preserves the official logo's colors; monochrome SVGs inherit
// the text color. Unknown providers use the interface's neutral plug icon.
// Cursor, Kimi, Z.ai and ElevenLabs publish monochrome-only marks, so their
// logos follow the text color like Grok's.
func ProviderIcon(c *ui.Context, provider store.Provider, size float32) ui.Element {
	switch provider {
	case store.ProviderCodex:
		return ui.Image(c, codexProviderIcon).Size(size, size)
	case store.ProviderGrok:
		return ui.Image(c, grokProviderIcon).Size(size, size)
	case store.ProviderClaude:
		return ui.Image(c, claudeProviderIcon).Size(size, size)
	case store.ProviderCursor:
		return ui.Image(c, cursorProviderIcon).Size(size, size)
	case store.ProviderKimi:
		return ui.Image(c, kimiMonoProviderIcon).Size(size, size)
	case store.ProviderZai:
		return ui.Image(c, zaiProviderIcon).Size(size, size)
	case store.ProviderAgy:
		return ui.Image(c, agyProviderIcon).Size(size, size)
	case store.ProviderDeepSeek:
		return ui.Image(c, deepseekProviderIcon).Size(size, size)
	case store.ProviderMiniMax:
		return ui.Image(c, minimaxProviderIcon).Size(size, size)
	case store.ProviderPixelLab:
		return ui.Image(c, pixellabProviderIcon).Size(size, size)
	case store.ProviderElevenLabs:
		return ui.Image(c, elevenlabsProviderIcon).Size(size, size)
	default:
		return ui.Icon(c, icPlug).Size(size, size).TextColor(c.Theme().TextMuted)
	}
}

// ProviderMonoIcon is the monochrome logo, drawn in the text color.
func ProviderMonoIcon(c *ui.Context, provider store.Provider, size float32) ui.Element {
	switch provider {
	case store.ProviderCodex:
		return ui.Image(c, codexMonoProviderIcon).Size(size, size)
	case store.ProviderClaude:
		return ui.Image(c, claudeMonoProviderIcon).Size(size, size)
	case store.ProviderKimi:
		return ui.Image(c, kimiMonoProviderIcon).Size(size, size)
	case store.ProviderAgy:
		return ui.Image(c, agyMonoProviderIcon).Size(size, size)
	case store.ProviderDeepSeek:
		return ui.Image(c, deepseekMonoProviderIcon).Size(size, size)
	case store.ProviderMiniMax:
		return ui.Image(c, minimaxMonoProviderIcon).Size(size, size)
	case store.ProviderPixelLab:
		return ui.Image(c, pixellabMonoProviderIcon).Size(size, size)
	default:
		return ProviderIcon(c, provider, size)
	}
}

// ProviderTabs follows PillTabs' selection and accessibility semantics, with
// the provider logo preceding each caller-localized label. With no labels it
// shows only icons, retaining the localized brand names for accessibility.
func ProviderTabs(c *ui.Context, selected *int, disabled bool, labels ...string) bool {
	return providerTabsOf(c, store.Providers(), selected, disabled, labels...)
}

// providerTabsOf is ProviderTabs over a subset of the providers, which the
// labels and the selected index follow.
func providerTabsOf(c *ui.Context, providers []store.Provider, selected *int, disabled bool, labels ...string) bool {
	t := c.Theme()
	if len(labels) == 0 {
		labels = make([]string, len(providers))
	}
	l := l10n.New(l10n.English)
	seg := ui.SegmentedBase(c, selected, len(labels))
	seg.Track.Padding(3).Gap(2).Radius(7).Wrap().Shrink(1).Background(t.Text.Alpha(0.06)).Children(func() {
		for i, label := range labels {
			name := label
			if name == "" && i < len(providers) {
				name = l.Text(providerKey(providers[i]))
			}
			s := seg.Segment(i).Padding(5, 7).Gap(5).Radius(5).Disabled(disabled).Label(name).Tooltip(name)
			col := t.TextMuted
			if i == *selected {
				if !disabled {
					col = t.Text
				}
				s.Background(pal(t).Inset).Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.12))
			}
			s.Children(func() {
				provider := store.Provider("")
				if i < len(providers) {
					provider = providers[i]
				}
				ProviderIcon(c, provider, 16).TextColor(col)
				if label != "" {
					ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(col).SingleLine()
				}
			})
		}
	})
	return seg.Track.Changed()
}
