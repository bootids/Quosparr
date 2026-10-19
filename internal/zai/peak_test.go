package zai

import (
	"testing"
	"time"
)

func TestPeakPricing(t *testing.T) {
	// 2026-10-19 is a Monday; the window is Monday to Friday 14:00-18:00
	// Beijing time (docs.z.ai/devpack/overview).
	cases := []struct {
		when time.Time
		peak bool
		note string
	}{
		{time.Date(2026, 10, 19, 13, 59, 0, 0, cst), false, "minute before the window"},
		{time.Date(2026, 10, 19, 14, 0, 0, 0, cst), true, "window start"},
		{time.Date(2026, 10, 19, 16, 30, 0, 0, cst), true, "mid window"},
		{time.Date(2026, 10, 19, 17, 59, 0, 0, cst), true, "last minute"},
		{time.Date(2026, 10, 19, 18, 0, 0, 0, cst), false, "window end"},
		{time.Date(2026, 10, 19, 9, 0, 0, 0, cst), false, "morning is off-peak"},
		{time.Date(2026, 10, 23, 17, 0, 0, 0, cst), true, "Friday counts"},
		{time.Date(2026, 10, 24, 15, 0, 0, 0, cst), false, "Saturday is off-peak"},
		{time.Date(2026, 10, 25, 15, 0, 0, 0, cst), false, "Sunday is off-peak"},
		{time.Date(2026, 10, 20, 6, 0, 0, 0, time.UTC), true, "06:00 UTC is 14:00 Beijing"},
		{time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC), false, "10:00 UTC is past the window"},
	}
	for _, tc := range cases {
		if got := PeakPricing(tc.when); got != tc.peak {
			t.Errorf("%s (%s): PeakPricing = %v, want %v", tc.when.Format(time.RFC3339), tc.note, got, tc.peak)
		}
	}
}
