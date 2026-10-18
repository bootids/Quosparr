package app

import (
	"context"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/codex"
	"github.com/licoy/quosparr/internal/grok"
	"github.com/licoy/quosparr/internal/l10n"
)

type accountChoice struct {
	id, name, detail string
	selected, update bool
}

type accountSelection struct {
	export bool
	rows   []accountChoice
	codex  []codex.ImportCandidate
	grok   []grok.ImportCandidate
	failed bool
}

func (s *accountSelection) selectedIDs() []string {
	var ids []string
	for _, row := range s.rows {
		if row.selected {
			ids = append(ids, row.id)
		}
	}
	return ids
}

func exportAccountSelection(accounts []codex.ManagedAccount) *accountSelection {
	s := &accountSelection{export: true}
	for _, account := range accounts {
		s.rows = append(s.rows, accountChoice{id: account.ID, name: account.ResolvedDisplayName(), detail: account.Email, selected: true})
	}
	return s
}

func codexImportSelection(store *codex.Store, paths []string) *accountSelection {
	preview := (&codex.Importer{Store: store}).PreviewFiles(paths)
	s := &accountSelection{codex: preview.Items, failed: len(preview.Failures) > 0}
	for _, candidate := range preview.Items {
		row := candidate.Preview
		s.rows = append(s.rows, accountChoice{id: row.ID, name: row.DisplayName, detail: row.Detail, selected: true, update: row.Conflict.WillUpdate})
	}
	return s
}

func (u *settingsUI) previewAccountFiles(f *accountForm, rt *liveApp) {
	if f.busy {
		return
	}
	f.busy = true
	kind, l := f.kind, u.L()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: l.Text(l10n.KeyAccountsAddFile), Multiple: true})
		var selection *accountSelection
		if err == nil && len(paths) > 0 {
			if kind == accountKindCodex {
				selection = codexImportSelection(rt.codex, paths)
			} else {
				preview, previewErr := rt.grokMod.PreviewImportFiles(context.Background(), paths)
				err = previewErr
				selection = &accountSelection{grok: preview.Items, failed: len(preview.Failures) > 0}
				for _, candidate := range preview.Items {
					row := candidate.Preview
					selection.rows = append(selection.rows, accountChoice{id: candidate.ID(), name: row.DisplayName, detail: row.Detail, selected: true, update: row.Conflict.WillUpdate})
				}
			}
		}
		u.post(func() {
			f.busy = false
			if err != nil {
				u.accountNotice(f, l.Text(l10n.KeyAccountsImportFailed))
				return
			}
			if selection != nil && len(selection.rows) == 0 {
				u.accountNotice(f, l.Text(l10n.KeyAccountsImportNoCredentials))
				return
			}
			f.selection = selection
		})
	}()
}

func accountSelectionEditor(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) {
	s := f.selection
	if s == nil {
		return
	}
	open := true
	ui.Modal(c.Key("account-selection"), &open, func() {
		accountSelectionBody(c, u, f, rt, s)
	}).Width(520)
	if !open && !f.busy {
		f.closeEditor()
	}
}

func accountSelectionBody(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp, s *accountSelection) {
	l := u.L()
	title, hint, action := l10n.KeyAccountsImportPreviewTitle, l10n.KeyAccountsImportPreviewHint, l10n.KeyAccountsImportSelected
	if s.export {
		title, hint, action = l10n.KeyAccountsExportTitle, l10n.KeyAccountsExportHint, l10n.KeyAccountsExport
	}
	ui.Text(c, l.Text(title)).FontSize(17).FontWeight(600)
	ui.Text(c, l.Text(hint)).FontSize(11).TextColor(c.Theme().TextMuted)
	if f.editorError != "" {
		ui.Text(c, f.editorError).FontSize(12).TextColor(c.Theme().Danger)
	}
	if s.failed {
		ui.Text(c, l.Text(l10n.KeyAccountsImportFailed)).FontSize(11).TextColor(c.Theme().Danger)
	}
	privacy := u.s.Prefs().AccountPrivacyModeEnabled
	ui.Scroll(c.Key("account-selection-rows")).MaxHeight(320).Gap(8).Children(func() {
		for i := range s.rows {
			row := &s.rows[i]
			ui.Checkbox(c.Key("selection-"+row.id), &row.selected, maskAccount(privacy, row.name)).Disabled(f.busy)
			if row.detail != "" {
				ui.Text(c, maskAccount(privacy, row.detail)).FontSize(11).TextColor(c.Theme().TextMuted)
			}
			if !s.export {
				conflict := l10n.KeyAccountsImportWillAdd
				if row.update {
					conflict = l10n.KeyAccountsImportWillUpdate
				}
				ui.Text(c, l.Text(conflict)).FontSize(11).TextColor(c.Theme().TextMuted)
			}
		}
	})
	ui.Row(c).Gap(8).Children(func() {
		if f.busy {
			ui.Spinner(c).Size(14, 14)
		}
		ui.Spacer(c).Grow(1)
		if PillButton(c, nil, l.Text(l10n.KeyCancel), f.busy).Clicked() {
			f.closeEditor()
		}
		if PillButton(c, nil, l.Text(action), f.busy || len(s.selectedIDs()) == 0).Clicked() {
			ids, kind := s.selectedIDs(), f.kind
			u.accountEditAct(f, func() (string, bool) {
				if rt == nil {
					return tx(l, xDetailUnavailable), false
				}
				return rt.commitAccountSelection(l, s, ids, kind)
			})
		}
	})
}

func (rt *liveApp) commitAccountSelection(l l10n.Localizer, s *accountSelection, ids []string, kind int) (string, bool) {
	if s.export {
		if kind == accountKindGrok {
			return rt.exportGrok(l, ids)
		}
		return rt.exportCodex(l, ids)
	}
	count, failed := 0, false
	if kind == accountKindCodex {
		batch := (&codex.Importer{Store: rt.codex}).ImportPreviewSelection(context.Background(), s.codex, ids, false)
		count, failed = len(batch.Succeeded), len(batch.Failures) > 0
		rt.reloadAccounts()
	} else {
		batch, _, err := rt.grokMod.ImportPreviewSelection(context.Background(), s.grok, ids)
		count, failed = batch.SuccessCount(), err != nil || len(batch.Failures) > 0
		rt.grok.reload(true)
	}
	if failed {
		return l.Textf(l10n.KeyAccountsImportSucceeded, count) + " · " + l.Text(l10n.KeyAccountsImportFailed), false
	}
	return l.Textf(l10n.KeyAccountsImportSucceeded, count), count > 0
}
