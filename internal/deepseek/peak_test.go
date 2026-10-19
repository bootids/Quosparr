package deepseek

import (
	"testing"
	"time"
)

func TestPeakPricing(t *testing.T) {
	// 2026-10-19 is a Monday; the peak windows are Monday to Friday
	// 09:00-12:00 and 14:00-18:00 Beijing time, the rest of the week is
	// off-peak (api-docs.deepseek.com/quick_start/pricing).
	cases := []struct {
		when time.Time
		peak bool
		note string
	}{
		{time.Date(2026, 10, 19, 8, 59, 0, 0, cst), false, "minute before the morning window"},
		{time.Date(2026, 10, 19, 9, 0, 0, 0, cst), true, "morning window start"},
		{time.Date(2026, 10, 19, 11, 59, 0, 0, cst), true, "morning window last minute"},
		{time.Date(2026, 10, 19, 12, 0, 0, 0, cst), false, "midday dip is off-peak"},
		{time.Date(2026, 10, 19, 13, 59, 0, 0, cst), false, "minute before the afternoon window"},
		{time.Date(2026, 10, 19, 14, 0, 0, 0, cst), true, "afternoon window start"},
		{time.Date(2026, 10, 19, 17, 59, 0, 0, cst), true, "afternoon window last minute"},
		{time.Date(2026, 10, 19, 18, 0, 0, 0, cst), false, "window end"},
		{time.Date(2026, 10, 19, 23, 30, 0, 0, cst), false, "late night is off-peak"},
		{time.Date(2026, 10, 23, 9, 30, 0, 0, cst), true, "Friday counts"},
		{time.Date(2026, 10, 24, 10, 0, 0, 0, cst), false, "Saturday is off-peak"},
		{time.Date(2026, 10, 25, 10, 0, 0, 0, cst), false, "Sunday is off-peak"},
		{time.Date(2026, 10, 20, 1, 30, 0, 0, time.UTC), true, "01:30 UTC is inside the Beijing morning window"},
		{time.Date(2026, 10, 20, 5, 0, 0, 0, time.UTC), false, "05:00 UTC falls in the midday dip"},
	}
	for _, tc := range cases {
		if got := PeakPricing(tc.when); got != tc.peak {
			t.Errorf("%s (%s): PeakPricing = %v, want %v", tc.when.Format(time.RFC3339), tc.note, got, tc.peak)
		}
	}
}
