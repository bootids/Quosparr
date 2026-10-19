package deepseek

import "time"

// cst is Beijing time; DeepSeek states its peak windows in UTC, which maps to
// the same 09:00–18:00 stretch of the Beijing day.
var cst = time.FixedZone("UTC+8", 8*60*60)

// PeakPricing reports whether t falls in DeepSeek's peak billing window:
// Monday to Friday 09:00–12:00 and 14:00–18:00 Beijing time (01:00–04:00 and
// 06:00–10:00 UTC), when API usage costs twice the off-peak rate. Chinese
// public holidays are off-peak upstream but are not tracked here, so the
// hint also lights up on those weekdays.
func PeakPricing(t time.Time) bool {
	local := t.In(cst)
	switch local.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	h := local.Hour()
	return (h >= 9 && h < 12) || (h >= 14 && h < 18)
}
