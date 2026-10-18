package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/agy"
	"github.com/licoy/quosparr/internal/cursor"
	"github.com/licoy/quosparr/internal/deepseek"
	"github.com/licoy/quosparr/internal/elevenlabs"
	"github.com/licoy/quosparr/internal/kimi"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/minimax"
	"github.com/licoy/quosparr/internal/pixellab"
	"github.com/licoy/quosparr/internal/platform"
	"github.com/licoy/quosparr/internal/store"
)

// meterConfigDraft is the editor state of the single-configuration provider
// tabs (Cursor, Kimi, Antigravity). A secret is only held between the
// password field and the config file; it is never rendered or echoed.
type meterConfigDraft struct {
	cursorToken   string
	kimiKey       string
	agyBin        string
	agyProfileDir string
	deepseekKey   string
	minimaxKey    string
	minimaxRegion string
	pixellabKey   string
	elevenlabsKey string
	err           string
}

// clearFor drops the draft of one provider, called on the main thread after
// its configuration was removed.
func (d *meterConfigDraft) clearFor(p store.Provider) {
	switch p {
	case store.ProviderCursor:
		d.cursorToken = ""
	case store.ProviderKimi:
		d.kimiKey = ""
	case store.ProviderAgy:
		d.agyBin, d.agyProfileDir = "", ""
	case store.ProviderDeepSeek:
		d.deepseekKey = ""
	case store.ProviderMiniMax:
		d.minimaxKey, d.minimaxRegion = "", ""
	case store.ProviderPixelLab:
		d.pixellabKey = ""
	case store.ProviderElevenLabs:
		d.elevenlabsKey = ""
	}
	d.err = ""
}

// renderCursorConfig is the Cursor tab: the resolved session (token saved
// here first, then Cursor's own login) and the token editor.
func renderCursorConfig(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) {
	l := u.L()
	dimText(c, tx(l, xCursorConfigIntro))
	meterConfigStatusCard(c, cursorConfigStatusLines(l, platform.RunwayDir(rt.home), rt.home))
	ui.Field(c, tx(l, xCursorTokenLabel), func() {
		ui.TextInput(c.Key("cursor-token"), &f.mcfg.cursorToken).Password().Placeholder(tx(l, xCursorTokenPlaceholder)).Disabled(f.busy)
	})
	meterConfigFormFooter(c, u, f, l.Text(l10n.KeyAccountsDelete), true, func() { u.clearCursorConfig(f, rt) }, func() { u.saveCursorConfig(f, rt) })
}

// renderKimiConfig is the Kimi tab: the resolved credential (key saved here,
// then KIMI_CODE_API_KEY, then the Kimi CLI login) and the key editor.
func renderKimiConfig(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) {
	l := u.L()
	dimText(c, tx(l, xKimiConfigIntro))
	meterConfigStatusCard(c, kimiConfigStatusLines(l, platform.RunwayDir(rt.home), rt.home))
	ui.Field(c, tx(l, xKimiKeyLabel), func() {
		ui.TextInput(c.Key("kimi-key"), &f.mcfg.kimiKey).Password().Placeholder(tx(l, xKimiKeyPlaceholder)).Disabled(f.busy)
	})
	meterConfigFormFooter(c, u, f, l.Text(l10n.KeyAccountsDelete), true, func() { u.clearKimiConfig(f, rt) }, func() { u.saveKimiConfig(f, rt) })
}

// renderAgyConfig is the Antigravity tab: where the CLI binary and profile
// directory were found, and the path overrides.
func renderAgyConfig(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) {
	l := u.L()
	dimText(c, tx(l, xAgyConfigIntro))
	meterConfigStatusCard(c, agyConfigStatusLines(l, platform.RunwayDir(rt.home), rt.home))
	ui.Field(c, tx(l, xAgyConfigBinLabel), func() {
		ui.TextInput(c.Key("agy-bin"), &f.mcfg.agyBin).Placeholder(tx(l, xAgyConfigBinPlaceholder)).Disabled(f.busy)
	})
	ui.Field(c, tx(l, xAgyConfigProfileLabel), func() {
		ui.TextInput(c.Key("agy-profile"), &f.mcfg.agyProfileDir).Placeholder(tx(l, xAgyConfigProfilePlaceholder)).Disabled(f.busy)
	})
	meterConfigFormFooter(c, u, f, tx(l, xAgyConfigReset), false, func() { u.resetAgyConfig(f, rt) }, func() { u.saveAgyConfig(f, rt) })
}

// renderDeepseekConfig is the DeepSeek tab: the resolved key (key saved here,
// then DEEPSEEK_API_KEY; DeepSeek has no credentials file) and the key editor.
func renderDeepseekConfig(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) {
	l := u.L()
	dimText(c, tx(l, xDeepseekConfigIntro))
	meterConfigStatusCard(c, deepseekConfigStatusLines(l, platform.RunwayDir(rt.home)))
	ui.Field(c, tx(l, xDeepseekKeyLabel), func() {
		ui.TextInput(c.Key("deepseek-key"), &f.mcfg.deepseekKey).Password().Placeholder(tx(l, xDeepseekKeyPlaceholder)).Disabled(f.busy)
	})
	meterConfigFormFooter(c, u, f, l.Text(l10n.KeyAccountsDelete), true, func() { u.clearDeepseekConfig(f, rt) }, func() { u.saveDeepseekConfig(f, rt) })
}

// renderMinimaxConfig is the MiniMax tab: the resolved key (key saved here,
// then the MINIMAX env vars, then the MiniMax CLI login), the effective
// region, and the key editor with its region picker.
func renderMinimaxConfig(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) {
	l := u.L()
	dimText(c, tx(l, xMinimaxConfigIntro))
	meterConfigStatusCard(c, minimaxConfigStatusLines(l, platform.RunwayDir(rt.home), rt.home))
	ui.Field(c, tx(l, xMinimaxKeyLabel), func() {
		ui.TextInput(c.Key("minimax-key"), &f.mcfg.minimaxKey).Password().Placeholder(tx(l, xMinimaxKeyPlaceholder)).Disabled(f.busy)
	})
	if v, ok := choose(c, "minimax-region", tx(l, xMinimaxRegionLabel), f.mcfg.minimaxRegion, []string{minimax.RegionCN, minimax.RegionIntl}, func(v string) string { return minimaxRegionText(l, v) }, f.busy); ok {
		f.mcfg.minimaxRegion = v
	}
	meterConfigFormFooter(c, u, f, l.Text(l10n.KeyAccountsDelete), true, func() { u.clearMinimaxConfig(f, rt) }, func() { u.saveMinimaxConfig(f, rt) })
}

// renderPixellabConfig is the PixelLab tab: the resolved key (key saved
// here, then PIXELLAB_API_KEY, then the PixelLab CLI token) and the editor.
func renderPixellabConfig(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) {
	l := u.L()
	dimText(c, tx(l, xPixellabConfigIntro))
	meterConfigStatusCard(c, pixellabConfigStatusLines(l, platform.RunwayDir(rt.home), rt.home))
	ui.Field(c, tx(l, xPixellabKeyLabel), func() {
		ui.TextInput(c.Key("pixellab-key"), &f.mcfg.pixellabKey).Password().Placeholder(tx(l, xPixellabKeyPlaceholder)).Disabled(f.busy)
	})
	meterConfigFormFooter(c, u, f, l.Text(l10n.KeyAccountsDelete), true, func() { u.clearPixellabConfig(f, rt) }, func() { u.savePixellabConfig(f, rt) })
}

// renderElevenlabsConfig is the ElevenLabs tab: the resolved key (key saved
// here, then ELEVENLABS_API_KEY, then the elevenlabs CLI key) and the editor.
func renderElevenlabsConfig(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) {
	l := u.L()
	dimText(c, tx(l, xElevenlabsConfigIntro))
	meterConfigStatusCard(c, elevenlabsConfigStatusLines(l, platform.RunwayDir(rt.home), rt.home))
	ui.Field(c, tx(l, xElevenlabsKeyLabel), func() {
		ui.TextInput(c.Key("elevenlabs-key"), &f.mcfg.elevenlabsKey).Password().Placeholder(tx(l, xElevenlabsKeyPlaceholder)).Disabled(f.busy)
	})
	meterConfigFormFooter(c, u, f, l.Text(l10n.KeyAccountsDelete), true, func() { u.clearElevenlabsConfig(f, rt) }, func() { u.saveElevenlabsConfig(f, rt) })
}

// meterConfigFormFooter closes a single-configuration tab: the validation
// error, then the clear and save actions.
func meterConfigFormFooter(c *ui.Context, u *settingsUI, f *accountForm, clearLabel string, saveNeedsDraft bool, onClear, onSave func()) {
	l := u.L()
	if f.mcfg.err != "" {
		ui.Text(c, f.mcfg.err).FontSize(12).TextColor(c.Theme().Danger)
	}
	saveDisabled := f.busy
	if saveNeedsDraft {
		saveDisabled = saveDisabled || strings.TrimSpace(meterConfigDraftSecret(f)) == ""
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if f.busy {
			ui.Spinner(c).Size(14, 14)
		}
		ui.Spacer(c).Grow(1)
		if PillButton(c, nil, clearLabel, f.busy).Clicked() {
			onClear()
		}
		if PillButton(c, nil, tx(l, xMeterConfigSave), saveDisabled).Clicked() {
			onSave()
		}
	})
}

// meterConfigDraftSecret is the secret of the active single-configuration
// tab, for the save button's empty check only.
func meterConfigDraftSecret(f *accountForm) string {
	switch accountKindProvider(f.kind) {
	case store.ProviderCursor:
		return f.mcfg.cursorToken
	case store.ProviderKimi:
		return f.mcfg.kimiKey
	case store.ProviderDeepSeek:
		return f.mcfg.deepseekKey
	case store.ProviderMiniMax:
		return f.mcfg.minimaxKey
	case store.ProviderPixelLab:
		return f.mcfg.pixellabKey
	case store.ProviderElevenLabs:
		return f.mcfg.elevenlabsKey
	}
	return ""
}

// meterConfigStatusCard is the current-configuration summary that leads a
// single-configuration tab.
func meterConfigStatusCard(c *ui.Context, lines []string) {
	ui.Column(c).Gap(4).Padding(12).Radius(12).Background(c.Theme().Surface).Children(func() {
		for _, s := range lines {
			dimText(c, s)
		}
	})
}

// meterConfigExpiry formats a credential expiry as a status-card line.
func meterConfigExpiry(l l10n.Localizer, at time.Time) string {
	return l.Text(l10n.KeyExpiresAt) + " " + at.In(time.Local).Format("2006-01-02 15:04")
}

// cursorConfigStatusLines describes the resolved Cursor session without ever
// including the token itself: the source, the user and the expiry.
func cursorConfigStatusLines(l l10n.Localizer, root, home string) []string {
	token, ok, err := cursor.LoadConfig(root)
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	if ok {
		cred, usable, terr := cursor.CredentialFromToken(token)
		if terr != nil || !usable {
			return []string{tx(l, xCursorConfigSourceConfigured), tx(l, xCursorConfigInvalid)}
		}
		return meterConfigIdentityLines(l, tx(l, xCursorConfigSourceConfigured), cred.User, cred.HasExpiry, cred.ExpiresAt)
	}
	cred, ok, err := cursor.LoadCredential(context.Background(), cursor.StateDBPath(home))
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	if !ok {
		return []string{tx(l, xMeterConfigNotConfigured)}
	}
	return meterConfigIdentityLines(l, tx(l, xCursorConfigSourceDiscovered), cred.User, cred.HasExpiry, cred.ExpiresAt)
}

// kimiConfigStatusLines describes the resolved Kimi credential by its source;
// a key is only ever shown as its fingerprint.
func kimiConfigStatusLines(l l10n.Localizer, root, home string) []string {
	key, ok, err := kimi.LoadConfig(root)
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	if ok {
		return []string{tx(l, xKimiConfigSourceConfigured), tx(l, xMeterConfigKeyHint, meter.KeyHint(key))}
	}
	if env := os.Getenv("KIMI_CODE_API_KEY"); env != "" {
		return []string{tx(l, xKimiConfigSourceEnv), tx(l, xMeterConfigKeyHint, meter.KeyHint(env))}
	}
	cred, ok, err := kimi.LoadCredential("", kimi.CredentialsPath(os.Getenv("KIMI_SHARE_DIR"), home))
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	if !ok {
		return []string{tx(l, xMeterConfigNotConfigured)}
	}
	return meterConfigIdentityLines(l, tx(l, xKimiConfigSourceFile), "", cred.HasExpiry, cred.ExpiresAt)
}

// deepseekConfigStatusLines describes the resolved DeepSeek key by its
// source; DeepSeek has no local credentials file to discover. A key is only
// ever shown as its fingerprint.
func deepseekConfigStatusLines(l l10n.Localizer, root string) []string {
	key, ok, err := deepseek.LoadConfig(root)
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	if ok {
		return []string{tx(l, xDeepseekConfigSourceConfigured), tx(l, xMeterConfigKeyHint, meter.KeyHint(key))}
	}
	if env := deepseek.EnvKey(); env != "" {
		return []string{tx(l, xDeepseekConfigSourceEnv), tx(l, xMeterConfigKeyHint, meter.KeyHint(env))}
	}
	return []string{tx(l, xMeterConfigNotConfigured)}
}

// minimaxConfigStatusLines describes the resolved MiniMax key by its source,
// followed by the effective region. A key is only ever shown as its
// fingerprint.
func minimaxConfigStatusLines(l l10n.Localizer, root, home string) []string {
	key, region, ok, err := minimax.LoadConfig(root)
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	var source string
	if ok {
		source = tx(l, xMinimaxConfigSourceConfigured)
	} else if env := minimax.EnvKey(); env != "" {
		key, source = env, tx(l, xMinimaxConfigSourceEnv)
	} else {
		fileKey, fileRegion, fileOK, fileErr := minimax.LoadCredential("", "", minimax.ConfigFilePath(home))
		if fileErr != nil {
			return []string{tx(l, xMeterConfigReadFailed)}
		}
		if !fileOK {
			return []string{tx(l, xMeterConfigNotConfigured)}
		}
		key, region, source = fileKey, fileRegion, tx(l, xMinimaxConfigSourceFile)
	}
	return []string{source, tx(l, xMeterConfigKeyHint, meter.KeyHint(key)), tx(l, xMinimaxRegionLabel) + ": " + minimaxRegionText(l, region)}
}

// minimaxRegionText labels a MiniMax region; an unset region is served by
// the China endpoint, matching the minimax package's default.
func minimaxRegionText(l l10n.Localizer, region string) string {
	if region == minimax.RegionIntl {
		return tx(l, xMinimaxRegionIntl)
	}
	return tx(l, xMinimaxRegionCn)
}

// pixellabConfigStatusLines describes the resolved PixelLab key by its
// source; a key is only ever shown as its fingerprint.
func pixellabConfigStatusLines(l l10n.Localizer, root, home string) []string {
	key, ok, err := pixellab.LoadConfig(root)
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	if ok {
		return []string{tx(l, xPixellabConfigSourceConfigured), tx(l, xMeterConfigKeyHint, meter.KeyHint(key))}
	}
	if env := pixellab.EnvKey(); env != "" {
		return []string{tx(l, xPixellabConfigSourceEnv), tx(l, xMeterConfigKeyHint, meter.KeyHint(env))}
	}
	key, ok, err = pixellab.LoadCredential("", pixellab.TokenFilePath(home))
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	if !ok {
		return []string{tx(l, xMeterConfigNotConfigured)}
	}
	return []string{tx(l, xPixellabConfigSourceFile), tx(l, xMeterConfigKeyHint, meter.KeyHint(key))}
}

// elevenlabsConfigStatusLines describes the resolved ElevenLabs key by its
// source; a key is only ever shown as its fingerprint.
func elevenlabsConfigStatusLines(l l10n.Localizer, root, home string) []string {
	key, ok, err := elevenlabs.LoadConfig(root)
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	if ok {
		return []string{tx(l, xElevenlabsConfigSourceConfigured), tx(l, xMeterConfigKeyHint, meter.KeyHint(key))}
	}
	if env := elevenlabs.EnvKey(); env != "" {
		return []string{tx(l, xElevenlabsConfigSourceEnv), tx(l, xMeterConfigKeyHint, meter.KeyHint(env))}
	}
	key, ok, err = elevenlabs.LoadCredential("", elevenlabs.APIKeyFilePath(home))
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	if !ok {
		return []string{tx(l, xMeterConfigNotConfigured)}
	}
	return []string{tx(l, xElevenlabsConfigSourceFile), tx(l, xMeterConfigKeyHint, meter.KeyHint(key))}
}

// agyConfigStatusLines reports the Antigravity CLI discovery: the binary, the
// profile directory, and whether saved overrides are in effect.
func agyConfigStatusLines(l l10n.Localizer, root, home string) []string {
	cfg, ok, err := agy.LoadConfig(root)
	if err != nil {
		return []string{tx(l, xMeterConfigReadFailed)}
	}
	binOverride, dirOverride := "", ""
	if ok {
		binOverride, dirOverride = cfg.Bin, cfg.ProfileDir
	}
	// Probe the binary side of the discovery with a profile directory that
	// always exists (home), so only the binary is judged.
	bin, binFound := agy.DetectIn(binOverride, home, home)
	profile := agyConfigProfileDir(dirOverride, home)
	_, statErr := os.Stat(profile)
	lines := make([]string, 0, 3)
	if binFound {
		lines = append(lines, tx(l, xAgyConfigBinFound, bin))
	} else {
		lines = append(lines, tx(l, xAgyConfigBinMissing))
	}
	if statErr == nil {
		lines = append(lines, tx(l, xAgyConfigProfilePresent, profile))
	} else {
		lines = append(lines, tx(l, xAgyConfigProfileMissing))
	}
	if ok {
		lines = append(lines, tx(l, xAgyConfigOverrides))
	}
	return lines
}

// agyConfigProfileDir is the profile directory shown in the status card: the
// configured override, else agy's documented default (see agy.DetectIn).
func agyConfigProfileDir(dirOverride, home string) string {
	if dirOverride != "" {
		return dirOverride
	}
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

// meterConfigIdentityLines is the shared status-card tail: the source, the
// user and the expiry of a resolved credential.
func meterConfigIdentityLines(l l10n.Localizer, source, user string, hasExpiry bool, expiresAt time.Time) []string {
	lines := []string{source}
	if user != "" {
		lines = append(lines, tx(l, xMeterConfigUser, user))
	}
	if hasExpiry {
		lines = append(lines, meterConfigExpiry(l, expiresAt))
	}
	return lines
}

// saveCursorConfig validates the draft token as a session token before
// anything is written; an invalid draft only shows the error.
func (u *settingsUI) saveCursorConfig(f *accountForm, rt *liveApp) {
	l := u.L()
	f.mcfg.err = ""
	token := strings.TrimSpace(f.mcfg.cursorToken)
	if token == "" {
		f.mcfg.err = tx(l, xCursorTokenRequired)
		return
	}
	if _, usable, err := cursor.CredentialFromToken(token); err != nil || !usable {
		f.mcfg.err = tx(l, xCursorTokenInvalid)
		return
	}
	u.accountAct(f, "", func() string {
		if err := cursor.SaveConfig(platform.RunwayDir(rt.home), token); err != nil {
			return l.Text(l10n.KeyStatusError)
		}
		u.post(func() { f.mcfg.clearFor(store.ProviderCursor) })
		rt.refreshMeter(store.ProviderCursor)
		return ""
	})
}

// saveKimiConfig trims the draft key and requires one before saving.
func (u *settingsUI) saveKimiConfig(f *accountForm, rt *liveApp) {
	l := u.L()
	f.mcfg.err = ""
	key := strings.TrimSpace(f.mcfg.kimiKey)
	if key == "" {
		f.mcfg.err = tx(l, xKimiKeyRequired)
		return
	}
	u.accountAct(f, "", func() string {
		if err := kimi.SaveConfig(platform.RunwayDir(rt.home), key); err != nil {
			return l.Text(l10n.KeyStatusError)
		}
		u.post(func() { f.mcfg.clearFor(store.ProviderKimi) })
		rt.refreshMeter(store.ProviderKimi)
		return ""
	})
}

// saveDeepseekConfig trims the draft key and requires one before saving.
func (u *settingsUI) saveDeepseekConfig(f *accountForm, rt *liveApp) {
	l := u.L()
	f.mcfg.err = ""
	key := strings.TrimSpace(f.mcfg.deepseekKey)
	if key == "" {
		f.mcfg.err = tx(l, xDeepseekKeyRequired)
		return
	}
	u.accountAct(f, "", func() string {
		if err := deepseek.SaveConfig(platform.RunwayDir(rt.home), key); err != nil {
			return l.Text(l10n.KeyStatusError)
		}
		u.post(func() { f.mcfg.clearFor(store.ProviderDeepSeek) })
		rt.refreshMeter(store.ProviderDeepSeek)
		return ""
	})
}

// saveMinimaxConfig trims the draft key and requires one before saving; the
// region is the picker's selection, normalized by the minimax package.
func (u *settingsUI) saveMinimaxConfig(f *accountForm, rt *liveApp) {
	l := u.L()
	f.mcfg.err = ""
	key := strings.TrimSpace(f.mcfg.minimaxKey)
	if key == "" {
		f.mcfg.err = tx(l, xMinimaxKeyRequired)
		return
	}
	region := f.mcfg.minimaxRegion
	u.accountAct(f, "", func() string {
		if err := minimax.SaveConfig(platform.RunwayDir(rt.home), key, region); err != nil {
			return l.Text(l10n.KeyStatusError)
		}
		u.post(func() { f.mcfg.clearFor(store.ProviderMiniMax) })
		rt.refreshMeter(store.ProviderMiniMax)
		return ""
	})
}

// savePixellabConfig trims the draft key and requires one before saving.
func (u *settingsUI) savePixellabConfig(f *accountForm, rt *liveApp) {
	l := u.L()
	f.mcfg.err = ""
	key := strings.TrimSpace(f.mcfg.pixellabKey)
	if key == "" {
		f.mcfg.err = tx(l, xPixellabKeyRequired)
		return
	}
	u.accountAct(f, "", func() string {
		if err := pixellab.SaveConfig(platform.RunwayDir(rt.home), key); err != nil {
			return l.Text(l10n.KeyStatusError)
		}
		u.post(func() { f.mcfg.clearFor(store.ProviderPixelLab) })
		rt.refreshMeter(store.ProviderPixelLab)
		return ""
	})
}

// saveElevenlabsConfig trims the draft key and requires one before saving.
func (u *settingsUI) saveElevenlabsConfig(f *accountForm, rt *liveApp) {
	l := u.L()
	f.mcfg.err = ""
	key := strings.TrimSpace(f.mcfg.elevenlabsKey)
	if key == "" {
		f.mcfg.err = tx(l, xElevenlabsKeyRequired)
		return
	}
	u.accountAct(f, "", func() string {
		if err := elevenlabs.SaveConfig(platform.RunwayDir(rt.home), key); err != nil {
			return l.Text(l10n.KeyStatusError)
		}
		u.post(func() { f.mcfg.clearFor(store.ProviderElevenLabs) })
		rt.refreshMeter(store.ProviderElevenLabs)
		return ""
	})
}

// saveAgyConfig stores the path overrides as entered; empty fields are an
// explicit reset of that side to auto-discovery.
func (u *settingsUI) saveAgyConfig(f *accountForm, rt *liveApp) {
	l := u.L()
	f.mcfg.err = ""
	cfg := agy.Config{Bin: f.mcfg.agyBin, ProfileDir: f.mcfg.agyProfileDir}
	u.accountAct(f, "", func() string {
		if err := agy.SaveConfig(platform.RunwayDir(rt.home), cfg); err != nil {
			return l.Text(l10n.KeyStatusError)
		}
		rt.refreshMeter(store.ProviderAgy)
		return ""
	})
}

// clearCursorConfig asks for confirmation, then removes the saved token.
func (u *settingsUI) clearCursorConfig(f *accountForm, rt *liveApp) {
	u.removeMeterConfigConfirm(f, rt, store.ProviderCursor)
}

// clearKimiConfig asks for confirmation, then removes the saved key.
func (u *settingsUI) clearKimiConfig(f *accountForm, rt *liveApp) {
	u.removeMeterConfigConfirm(f, rt, store.ProviderKimi)
}

// clearDeepseekConfig asks for confirmation, then removes the saved key.
func (u *settingsUI) clearDeepseekConfig(f *accountForm, rt *liveApp) {
	u.removeMeterConfigConfirm(f, rt, store.ProviderDeepSeek)
}

// clearMinimaxConfig asks for confirmation, then removes the saved key.
func (u *settingsUI) clearMinimaxConfig(f *accountForm, rt *liveApp) {
	u.removeMeterConfigConfirm(f, rt, store.ProviderMiniMax)
}

// clearPixellabConfig asks for confirmation, then removes the saved key.
func (u *settingsUI) clearPixellabConfig(f *accountForm, rt *liveApp) {
	u.removeMeterConfigConfirm(f, rt, store.ProviderPixelLab)
}

// clearElevenlabsConfig asks for confirmation, then removes the saved key.
func (u *settingsUI) clearElevenlabsConfig(f *accountForm, rt *liveApp) {
	u.removeMeterConfigConfirm(f, rt, store.ProviderElevenLabs)
}

// resetAgyConfig restores the default Antigravity discovery without asking:
// the overrides hold no secret.
func (u *settingsUI) resetAgyConfig(f *accountForm, rt *liveApp) {
	l := u.L()
	u.accountAct(f, "", func() string {
		if err := rt.removeMeterConfig(store.ProviderAgy); err != nil {
			return l.Text(l10n.KeyStatusError)
		}
		u.post(func() { f.mcfg.clearFor(store.ProviderAgy) })
		rt.refreshMeter(store.ProviderAgy)
		return ""
	})
}

// removeMeterConfigConfirm asks before a credential configuration is removed;
// Quosparr then falls back to automatic discovery.
func (u *settingsUI) removeMeterConfigConfirm(f *accountForm, rt *liveApp, p store.Provider) {
	l := u.L()
	confirmed, err := confirmDialog(l, tx(l, xMeterConfigRemoveConfirmTitle), tx(l, xMeterConfigRemoveConfirmMessage), "", l.Text(l10n.KeyAccountsDelete))
	if err != nil || !confirmed {
		return
	}
	u.accountAct(f, "", func() string {
		if err := rt.removeMeterConfig(p); err != nil {
			return l.Text(l10n.KeyStatusError)
		}
		u.post(func() { f.mcfg.clearFor(p) })
		rt.refreshMeter(p)
		return ""
	})
}

// removeMeterConfig deletes the explicit configuration of a meter provider,
// returning it to automatic discovery.
func (rt *liveApp) removeMeterConfig(p store.Provider) error {
	root := platform.RunwayDir(rt.home)
	switch p {
	case store.ProviderCursor:
		return cursor.RemoveConfig(root)
	case store.ProviderKimi:
		return kimi.RemoveConfig(root)
	case store.ProviderAgy:
		return agy.RemoveConfig(root)
	case store.ProviderDeepSeek:
		return deepseek.RemoveConfig(root)
	case store.ProviderMiniMax:
		return minimax.RemoveConfig(root)
	case store.ProviderPixelLab:
		return pixellab.RemoveConfig(root)
	case store.ProviderElevenLabs:
		return elevenlabs.RemoveConfig(root)
	}
	return errors.New("meter config: unsupported provider")
}

// refreshMeter refetches a provider's quota after a configuration change.
// The module's onChange republishes the snapshot, which redraws the UI.
func (rt *liveApp) refreshMeter(p store.Provider) {
	if mod := rt.meterMods[p]; mod != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = mod.Refresh(ctx)
		}()
	}
}
