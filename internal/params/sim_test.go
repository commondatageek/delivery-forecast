package params

import (
	"flag"
	"strings"
	"testing"
	"time"
)

func TestParseTargetWindowFlags(t *testing.T) {
	now := time.Date(2025, 3, 1, 14, 30, 0, 0, time.Local)
	cases := []struct {
		name     string
		args     []string
		wantDays int
		wantEnd  string // "" = zero
		wantErr  string
	}{
		{"days only", []string{"-days", "10"}, 10, "", ""},
		{"explicit zero days is unset", []string{"-days", "0"}, 0, "", ""},
		{"end only", []string{"-target-end-date", "2025-03-11"}, 0, "2025-03-11", ""},
		{"both pass through", []string{"-days", "5", "-target-end-date", "2025-03-11"}, 5, "2025-03-11", ""},
		{"neither", nil, 0, "", ""},
		{"bad start", []string{"-target-start-date", "soon", "-days", "3"}, 0, "", "invalid -target-start-date"},
		{"bad end", []string{"-target-end-date", "later"}, 0, "", "invalid -target-end-date"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := flag.NewFlagSet("t", flag.ContinueOnError)
			days := fs.Int("days", 0, "")
			startStr := fs.String("target-start-date", "tomorrow", "")
			endStr := fs.String("target-end-date", "", "")
			if err := fs.Parse(c.args); err != nil {
				t.Fatal(err)
			}
			start, gotDays, end, err := parseTargetWindow(fs, *days, *startStr, *endStr, now)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTargetWindow: %v", err)
			}
			if got := start.Format("2006-01-02"); got != "2025-03-02" {
				t.Errorf("start = %s, want 2025-03-02", got)
			}
			if gotDays != c.wantDays {
				t.Errorf("days = %d, want %d", gotDays, c.wantDays)
			}
			if c.wantEnd == "" && !end.IsZero() || c.wantEnd != "" && end.Format("2006-01-02") != c.wantEnd {
				t.Errorf("end = %v, want %q", end, c.wantEnd)
			}
		})
	}
}
