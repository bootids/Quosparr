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
	"github.com/licoy/quosparr/internal/kimi"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/meter"
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
	default:
		return ""
	}
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
