package zai

import "time"

// cst is the UTC+8 zone both peak windows of the coding plan are stated in
// (Z.ai publishes them in Singapore Standard Time).
var cst = time.FixedZone("UTC+8", 8*60*60)

// PeakPricing reports whether t falls in the GLM Coding Plan's peak window:
// Monday to Friday 14:00–18:00 UTC+8. Off-peak usage is charged at half the
// credit rate, so during the window the panel highlights the provider name
// (docs.z.ai/devpack/overview). Holidays are not part of the published rule.
func PeakPricing(t time.Time) bool {
	local := t.In(cst)
	switch local.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	return local.Hour() >= 14 && local.Hour() < 18
}
