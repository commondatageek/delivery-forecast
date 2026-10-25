package app

import (
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
)

// resolveTargetWindow resolves a forecast horizon from a start date and
// exactly one of days (> 0) / end (non-zero). end is inclusive and is
// truncated to its local day first ("now" carries a time of day).
// effectiveDays is the calendar-day count of [start, end].
func resolveTargetWindow(start time.Time, days int, end time.Time) (resolvedEnd time.Time, effectiveDays int, err error) {
	if days != 0 && !end.IsZero() {
		return time.Time{}, 0, &FieldError{Fields: []string{"days", "target-end-date"}, Msg: "-days and -target-end-date are mutually exclusive"}
	}
	if days == 0 && end.IsZero() {
		return time.Time{}, 0, &FieldError{Fields: []string{"days", "target-end-date"}, Msg: "one of -days or -target-end-date must be provided"}
	}
	if days < 0 {
		return time.Time{}, 0, &FieldError{Fields: []string{"days"}, Msg: "-days must be positive"}
	}
	if days > 0 {
		return start.AddDate(0, 0, days-1), days, nil
	}

	// "now" carries a time of day; the window is whole calendar days.
	end = util.LocalDay(end)
	if !end.After(start) {
		return time.Time{}, 0, &FieldError{Fields: []string{"target-end-date"}, Msg: "-target-end-date must be after -target-start-date"}
	}
	return end, util.DayIndex(end, start) + 1, nil
}
