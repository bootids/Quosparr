package runway

import (
	"github.com/licoy/quosparr/internal/alerts"
	"github.com/licoy/quosparr/internal/codexapi"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/store"
)

func toAlertWindow(w codexapi.RateWindow) alerts.Window {
	out := alerts.Window{UsedPercent: w.UsedPercent}
	if !w.ResetsAt.IsZero() {
		t := w.ResetsAt
		out.ResetsAt = &t
	}
	return out
}

func quotaAlertInput(q codexapi.QuotaSnapshot) alerts.QuotaInput {
	in := alerts.QuotaInput{Primary: toAlertWindow(q.Primary)}
	if q.Secondary != nil {
		w := toAlertWindow(*q.Secondary)
		in.Secondary = &w
	}
	for _, a := range q.AdditionalWindows {
		in.Additional = append(in.Additional, alerts.NamedWindow{Name: a.Name, Window: toAlertWindow(a.Window)})
	}
	return in
}

func creditAlertInput(c codexapi.ResetCreditsSnapshot) []alerts.ResetCredit {
	out := make([]alerts.ResetCredit, 0, len(c.Credits))
	for _, cr := range c.Credits {
		ac := alerts.ResetCredit{Status: cr.Status, RemainingSeconds: cr.RemainingSeconds}
		if cr.ID != "" {
			id := cr.ID
			ac.ID = &id
		}
		if !cr.ExpiresAt.IsZero() {
			t := cr.ExpiresAt
			ac.ExpiresAt = &t
		}
		out = append(out, ac)
	}
	return out
}

// remoteAlertCandidates builds the alert candidates of a decoded remote
// status: quota thresholds and expiring reset credits, gated by the same
// preferences as the local Service. Rate-limit-reset detection stays local:
// the remote file has no reset-today reading to re-evaluate.
func remoteAlertCandidates(st export.StatusSnapshot, prefs store.Preferences) []alerts.Alert {
	var out []alerts.Alert
	if prefs.QuotaAlertsEnabled {
		if q := importStatusQuota(st.Quota); q != nil {
			out = append(out, alerts.QuotaAlerts(quotaAlertInput(*q))...)
		}
	}
	if !prefs.ResetCreditAlertsEnabled || st.Panel == nil {
		return out
	}
	if st.Panel.Codex != nil && st.Panel.Codex.ResetCredits != nil {
		if c := importPanelResetCredits(st.Panel.Codex.ResetCredits); c != nil {
			out = append(out, alerts.ResetCreditAlerts(creditAlertInput(*c))...)
		}
	}
	for _, block := range []*export.ProviderPanelBlock{st.Panel.Grok, st.Panel.Claude} {
		if block == nil || block.ResetCredits == nil {
			continue
		}
		c := importPanelResetCredits(block.ResetCredits)
		if c != nil {
			out = append(out, alerts.ResetCreditAlerts(creditAlertInput(*c))...)
		}
	}
	return out
}
