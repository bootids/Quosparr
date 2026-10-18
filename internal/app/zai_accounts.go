package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/zai"
)

// Editor modes of the Z.ai API-key account page.
const (
	modeZaiAdd  = "zai-add"
	modeZaiEdit = "zai-edit"
)

// zaiDraft is the editor state of one Z.ai API-key account. The key is only
// held here between the password field and the store; it is never rendered
// or echoed in a message.
type zaiDraft struct {
	label, key string
	region     string
	scheme     string
	editID     string
}

// renderZaiAccounts is the Z.ai tab: the local API-key library with the
// current account highlighted. The key itself only moves between the store
// and request headers.
func renderZaiAccounts(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) {
	l := u.L()
	dimText(c, l.Text(l10n.KeyZaiAccountsIntro))
	rows, err := rt.zaiRows()
	current := ""
	if rt.zaiStore != nil {
		current, _ = rt.zaiStore.Current()
	}
	accountToolbar(c, u, f, accountCountTitle(l, len(rows)), accountFolder{}, func(m *ui.Menu) {
		if m.Item(tx(l, xAPIAdd)).Chosen() {
			f.zai = zaiDraft{region: zai.RegionIntl, scheme: zai.SchemeRaw}
			f.mode = modeZaiAdd
		}
	}, func() string {
		return ""
	}, func() string {
		rt.refreshZaiMeter()
		return ""
	})
	zaiEditor(c, u, f)
	if err != nil {
		calloutDim(c, l.Text(l10n.KeyStatusError))
	}
	if len(rows) == 0 && err == nil {
		ui.Text(c, l.Text(l10n.KeyAccountsEmpty)).FontSize(12).TextColor(c.Theme().TextMuted)
	}
	accountRows(c, len(rows), func(i int) {
		renderZaiAccountRow(c, u, f, rt, rows[i], current)
	})
}

// renderZaiAccountRow is one stored key: label and fingerprint, region and
// auth scheme badges, the current marker, and the row actions.
func renderZaiAccountRow(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp, row zai.Row, current string) {
	l := u.L()
	t := c.Theme()
	ui.Row(c).Gap(10).Padding(10, 0).AlignItems(ui.Center).Children(func() {
		ProviderIcon(c, store.ProviderZai, 20)
		ui.Column(c).Gap(2).Grow(1).Basis(0).Children(func() {
			name := row.Label
			if name == "" {
				name = row.KeyHint
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).FontSize(fsCallout).FontWeight(600).SingleLine()
				if row.ID == current {
					ui.Text(c, l.Text(l10n.KeyAccountsCurrent)).FontSize(fsCaption).TextColor(t.TextMuted)
				}
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, row.KeyHint).FontSize(fsCaption).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine()
				dimText(c, zaiRegionText(l, row.Region)+" · "+zaiSchemeText(l, row.AuthScheme))
			})
		})
		ui.MenuButton(c, l.Text(l10n.KeyAccountsActions), func(m *ui.Menu) {
			if m.Item(l.Text(l10n.KeyAccountsMakeCurrent)).Disabled(row.ID == current).Chosen() {
				u.accountAct(f, "", func() string { return rt.zaiSetCurrent(l, row.ID) })
			}
			if m.Item(tx(l, xAPIEditMenu)).Chosen() {
				f.zai = zaiDraft{label: row.Label, region: row.Region, scheme: row.AuthScheme, editID: row.ID}
				f.mode = modeZaiEdit
			}
			if m.Item(l.Text(l10n.KeyAccountsDelete)).Chosen() {
				u.accountAct(f, "", func() string { return rt.zaiDelete(l, row.ID) })
			}
		}).Disabled(f.busy || rt == nil)
	})
}

func zaiRegionText(l l10n.Localizer, region string) string {
	if region == zai.RegionCN {
		return l.Text(l10n.KeyZaiRegionCn)
	}
	return l.Text(l10n.KeyZaiRegionIntl)
}

func zaiSchemeText(l l10n.Localizer, scheme string) string {
	if scheme == zai.SchemeBearer {
		return l.Text(l10n.KeyZaiAuthSchemeBearer)
	}
	return l.Text(l10n.KeyZaiAuthSchemeRaw)
}

// zaiEditor is the add/edit modal: label, API key (kept when left empty on
// edit), region and auth scheme.
func zaiEditor(c *ui.Context, u *settingsUI, f *accountForm) {
	if f.mode != modeZaiAdd && f.mode != modeZaiEdit {
		return
	}
	l, open := u.L(), true
	edit := f.mode == modeZaiEdit
	d := &f.zai
	title := tx(l, xAPIAdd)
	if edit {
		title = tx(l, xAPIEdit)
	}
	ui.Modal(c.Key("zai-editor"), &open, func() {
		ui.Text(c, title).FontSize(17).FontWeight(600)
		ui.Field(c, l.Text(l10n.KeyAlias), func() {
			ui.TextInput(c.Key("zai-label"), &d.label).Disabled(f.busy)
		})
		ui.Field(c, l.Text(l10n.KeyZaiApiKeyLabel), func() {
			in := ui.TextInput(c.Key("zai-key"), &d.key).Password().Placeholder(l.Text(l10n.KeyZaiApiKeyPlaceholder)).Disabled(f.busy)
			if edit {
				in.Placeholder(l.Text(l10n.KeyZaiKeyKeptHint))
			}
		})
		if v, ok := choose(c, "zai-region", l.Text(l10n.KeyZaiRegionLabel), d.region, []string{zai.RegionIntl, zai.RegionCN}, func(v string) string { return zaiRegionText(l, v) }, f.busy); ok {
			d.region = v
		}
		if v, ok := choose(c, "zai-scheme", l.Text(l10n.KeyZaiAuthSchemeLabel), d.scheme, []string{zai.SchemeRaw, zai.SchemeBearer}, func(v string) string { return zaiSchemeText(l, v) }, f.busy); ok {
			d.scheme = v
		}
		if f.editorError != "" {
			ui.Text(c, f.editorError).FontSize(12).TextColor(c.Theme().Danger)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if f.busy {
				ui.Spinner(c).Size(14, 14)
			}
			ui.Spacer(c).Grow(1)
			if PillButton(c, nil, l.Text(l10n.KeyCancel), f.busy).Clicked() {
				f.mode, f.zai = "", zaiDraft{}
			}
			empty := !edit && strings.TrimSpace(d.key) == ""
			if PillButton(c, nil, title, f.busy || empty).Clicked() {
				u.submitZaiEditor(f)
			}
		})
	}).Width(520)
	if !open && !f.busy {
		f.mode, f.zai = "", zaiDraft{}
	}
}

// submitZaiEditor saves the draft. An edit with an empty key keeps the
// stored key; failures keep the draft and show the error, never the key.
func (u *settingsUI) submitZaiEditor(f *accountForm) {
	l, mode := u.L(), f.mode
	d := f.zai
	u.accountEditAct(f, func() (string, bool) {
		rt := currentRuntime()
		if rt == nil || rt.zaiStore == nil {
			return l.Text(l10n.KeyStatusError), false
		}
		key := strings.TrimSpace(d.key)
		var err error
		if mode == modeZaiAdd {
			if key == "" {
				return l.Text(l10n.KeyZaiApiKeyLabel), false
			}
			_, err = rt.zaiStore.Add(d.label, key, d.region, d.scheme)
		} else {
			var keyPtr *string
			if key != "" {
				keyPtr = &key
			}
			err = rt.zaiStore.Update(d.editID, d.label, keyPtr, d.region, d.scheme)
		}
		if err != nil {
			return l.Text(l10n.KeyStatusError), false
		}
		rt.refreshZaiMeter()
		return "", true
	})
}

// zaiRows lists the stored accounts; a store that failed to open lists none.
func (rt *liveApp) zaiRows() ([]zai.Row, error) {
	if rt.zaiStore == nil {
		return nil, nil
	}
	return rt.zaiStore.Rows()
}

// refreshZaiMeter refetches the zai quota after an account change. The
// module's onChange republishes the snapshot, which redraws the UI.
func (rt *liveApp) refreshZaiMeter() {
	if rt.zaiMod == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = rt.zaiMod.Refresh(ctx)
	}()
}

func (rt *liveApp) zaiSetCurrent(l l10n.Localizer, id string) string {
	if rt.zaiStore == nil {
		return l.Text(l10n.KeyStatusError)
	}
	if err := rt.zaiStore.SetCurrent(id); err != nil {
		return l.Text(l10n.KeyStatusError)
	}
	rt.refreshZaiMeter()
	return ""
}

// zaiDelete asks for confirmation, then removes the account.
func (rt *liveApp) zaiDelete(l l10n.Localizer, id string) string {
	confirmed, err := confirmDialog(l, l.Text(l10n.KeyAccountsDeleteConfirmTitle), l.Text(l10n.KeyAccountsDeleteConfirmMessage), "", l.Text(l10n.KeyAccountsDelete))
	if err != nil {
		return l.Text(l10n.KeyStatusError)
	}
	if !confirmed {
		return ""
	}
	return rt.removeZai(l, id)
}

func (rt *liveApp) removeZai(l l10n.Localizer, id string) string {
	if rt.zaiStore == nil {
		return l.Text(l10n.KeyStatusError)
	}
	if err := rt.zaiStore.Delete(id); err != nil {
		return l.Text(l10n.KeyStatusError)
	}
	rt.refreshZaiMeter()
	return ""
}
