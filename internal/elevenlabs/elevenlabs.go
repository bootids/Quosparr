// Package elevenlabs reads the subscription quota of ElevenLabs from
// https://api.elevenlabs.io/v1/user/subscription with an API key resolved
// from ELEVENLABS_API_KEY or the CLI api-key file.
//
// The package is pure business logic: no UI, no localization, no logging and
// no global state. HTTP goes through a caller supplied Doer so proxy and TLS
// policy stay with the caller. The key travels in the xi-api-key header (not
// a Bearer token) and is redacted from every error message.
package elevenlabs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/meter"
)

const (
	baseURL   = "https://api.elevenlabs.io"
	userAgent = "Quosparr"
)

// flexFloat is a JSON number or numeric string; ok reports presence.
type flexFloat struct {
	v  float64
	ok bool
}

// UnmarshalJSON accepts a JSON number or a string holding a decimal number;
// anything else decodes as absent.
func (f *flexFloat) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" {
		return nil
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	f.v, f.ok = v, true
	return nil
}

// subscriptionPayload is the /v1/user/subscription response shape. Only the
// quota fields are read; billing, status and extension flags are ignored.
// Static voice pools use pointers so a null key is distinguishable from a
// missing one (both mean absent here).
type subscriptionPayload struct {
	Tier                        string     `json:"tier"`
	CharacterCount              flexFloat  `json:"character_count"`
	CharacterLimit              flexFloat  `json:"character_limit"`
	NextCharacterCountResetUnix flexFloat  `json:"next_character_count_reset_unix"`
	CharacterRefreshPeriod      string     `json:"character_refresh_period"`
	VoiceSlotsUsed              *flexFloat `json:"voice_slots_used"`
	VoiceLimit                  *flexFloat `json:"voice_limit"`
	ProfessionalVoiceSlotsUsed  *flexFloat `json:"professional_voice_slots_used"`
	ProfessionalVoiceLimit      *flexFloat `json:"professional_voice_limit"`
	VoiceAddEditCounter         *flexFloat `json:"voice_add_edit_counter"`
	MaxVoiceAddEdits            *flexFloat `json:"max_voice_add_edits"`
}

// Fetch reads the ElevenLabs subscription endpoint and returns the
// normalized state. An empty key is notConfigured; failure states carry a
// redacted message and never the key or the response body.
func Fetch(ctx context.Context, client meter.Doer, key string, now time.Time) meter.State {
	return fetch(ctx, client, baseURL, key, now)
}

// fetch is Fetch against an explicit base URL so tests can target a local
// server.
func fetch(ctx context.Context, client meter.Doer, base, key string, now time.Time) meter.State {
	if key == "" {
		return meter.NotConfigured()
	}
	headers := map[string]string{"xi-api-key": key}
	body, _, err := meter.GetJSON(ctx, client, base+"/v1/user/subscription", userAgent, headers)
	if err != nil {
		return meter.Failed(fetchStatus(err), meter.Redact(err.Error(), key))
	}
	state, perr := normalize(body)
	if perr != nil {
		return meter.Failed(meter.StatusParse, meter.Redact(perr.Error(), key))
	}
	state.FetchedAt = now
	return state
}

// fetchStatus classifies a GetJSON failure; a non-FetchError defect defaults
// to parse so no unclassified status can leak.
func fetchStatus(err error) meter.Status {
	if fe, ok := err.(*meter.FetchError); ok {
		return fe.Status
	}
	return meter.StatusParse
}

// normalize turns the subscription response into limit rows: the characters
// pool plus one row per populated static voice pool. A static pool is
// skipped only when both its used and limit keys are null or missing. The
// monthly refresh period has no known window length, so WindowMinutes stays
// 0 for every row.
func normalize(body []byte) (meter.State, error) {
	var payload subscriptionPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return meter.State{}, fmt.Errorf("elevenlabs: decode subscription: %w", err)
	}
	state := meter.State{Status: meter.StatusOK, Plan: payload.Tier}
	state.Limits = append(state.Limits, charactersLimit(payload))
	if l, ok := staticLimit("voice_slots", "Voice slots", "voices",
		payload.VoiceSlotsUsed, payload.VoiceLimit); ok {
		state.Limits = append(state.Limits, l)
	}
	if l, ok := staticLimit("professional_voice_slots", "Professional voices", "voices",
		payload.ProfessionalVoiceSlotsUsed, payload.ProfessionalVoiceLimit); ok {
		state.Limits = append(state.Limits, l)
	}
	if l, ok := staticLimit("voice_add_edits", "Voice edits", "edits",
		payload.VoiceAddEditCounter, payload.MaxVoiceAddEdits); ok {
		state.Limits = append(state.Limits, l)
	}
	return state, nil
}

// charactersLimit builds the characters row; character_refresh_period only
// labels the window (monthly), it never sets a window length.
func charactersLimit(p subscriptionPayload) meter.Limit {
	l := meter.Limit{
		ID:            "characters",
		Title:         "Characters",
		Unit:          "characters",
		WindowMinutes: 0,
		Used:          flexValue(p.CharacterCount),
		LimitValue:    flexValue(p.CharacterLimit),
	}
	l.Remaining = l.LimitValue - l.Used
	if l.LimitValue > 0 && !math.IsNaN(l.Used) {
		l.Percent = l.Used / l.LimitValue * 100
		l.HasPercent = true
	}
	if p.NextCharacterCountResetUnix.ok {
		l.ResetsAt = epochTime(p.NextCharacterCountResetUnix.v)
		l.HasReset = true
	}
	return l
}

// staticLimit builds one static voice pool row; ok is false when both the
// used and limit keys are absent (null or missing) and the pool is skipped.
func staticLimit(id, title, unit string, used, limit *flexFloat) (meter.Limit, bool) {
	if (used == nil || !used.ok) && (limit == nil || !limit.ok) {
		return meter.Limit{}, false
	}
	l := meter.Limit{
		ID:         id,
		Title:      title,
		Unit:       unit,
		Used:       flexPtrValue(used),
		LimitValue: flexPtrValue(limit),
	}
	l.Remaining = l.LimitValue - l.Used
	if l.LimitValue > 0 && !math.IsNaN(l.Used) {
		l.Percent = l.Used / l.LimitValue * 100
		l.HasPercent = true
	}
	return l, true
}

// flexValue maps an absent flexFloat to NaN.
func flexValue(f flexFloat) float64 {
	if !f.ok {
		return math.NaN()
	}
	return f.v
}

// flexPtrValue maps a nil or absent pointer to NaN.
func flexPtrValue(f *flexFloat) float64 {
	if f == nil || !f.ok {
		return math.NaN()
	}
	return f.v
}

// epochTime interprets a raw timestamp: values >= 1e12 are milliseconds,
// anything smaller is seconds.
func epochTime(v float64) time.Time {
	if v >= 1e12 {
		return time.UnixMilli(int64(v)).UTC()
	}
	return time.Unix(int64(v), 0).UTC()
}
