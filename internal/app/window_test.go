package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResolveTargetWindow(t *testing.T) {
	start := time.Date(2025, 3, 2, 0, 0, 0, 0, time.Local)
	endAt := func(y int, m time.Month, d, hour int) time.Time {
		return time.Date(y, m, d, hour, 0, 0, 0, time.Local)
	}
	cases := []struct {
		name       string
		start      time.Time
		days       int
		end        time.Time
		wantEnd    string
		wantDays   int
		wantErr    string
		wantFields []string
	}{
		{"days only", start, 10, time.Time{}, "2025-03-11", 10, "", nil},
		{"days one", start, 1, time.Time{}, "2025-03-02", 1, "", nil},
		{"end only", start, 0, endAt(2025, 3, 11, 0), "2025-03-11", 10, "", nil},
		{"end carries a time of day", start, 0, endAt(2025, 3, 11, 15), "2025-03-11", 10, "", nil},
		{"explicit start with days", time.Date(2025, 4, 1, 0, 0, 0, 0, time.Local), 30, time.Time{}, "2025-04-30", 30, "", nil},
		{"both", start, 5, endAt(2025, 3, 11, 0), "", 0, "-days and -target-end-date are mutually exclusive", []string{"days", "target-end-date"}},
		{"neither", start, 0, time.Time{}, "", 0, "one of -days or -target-end-date must be provided", []string{"days", "target-end-date"}},
		{"explicit zero days is unset (§3 item 2)", start, 0, time.Time{}, "", 0, "one of -days or -target-end-date must be provided", []string{"days", "target-end-date"}},
		{"negative days", start, -3, time.Time{}, "", 0, "-days must be positive", []string{"days"}},
		{"end before start", start, 0, endAt(2025, 3, 1, 0), "", 0, "-target-end-date must be after -target-start-date", []string{"target-end-date"}},
		{"end equals start", start, 0, endAt(2025, 3, 2, 0), "", 0, "-target-end-date must be after -target-start-date", []string{"target-end-date"}},
		{"end same day as start but later time", start, 0, endAt(2025, 3, 2, 15), "", 0, "-target-end-date must be after -target-start-date", []string{"target-end-date"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			end, n, err := resolveTargetWindow(c.start, c.days, c.end)
			if c.wantErr != "" {
				var fe *FieldError
				if !errors.As(err, &fe) {
					t.Fatalf("error = %v, want *FieldError", err)
				}
				if !strings.Contains(fe.Msg, c.wantErr) || fe.Error() != fe.Msg {
					t.Errorf("Msg = %q, want one containing %q", fe.Msg, c.wantErr)
				}
				if !reflect.DeepEqual(fe.Fields, c.wantFields) {
					t.Errorf("Fields = %v, want %v", fe.Fields, c.wantFields)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveTargetWindow: %v", err)
			}
			if got := end.Format("2006-01-02"); got != c.wantEnd {
				t.Errorf("end = %s, want %s", got, c.wantEnd)
			}
			if n != c.wantDays {
				t.Errorf("effectiveDays = %d, want %d", n, c.wantDays)
			}
		})
	}
}
