package report

import (
	"testing"
	"time"
)

func TestDescribeWindow(t *testing.T) {
	start := time.Date(2026, 10, 8, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, 11, 6, 0, 0, 0, 0, time.Local)
	if got, want := describeWindow(start, end, 30), "2026-10-08 to 2026-11-06 (30 days)"; got != want {
		t.Errorf("describeWindow = %q, want %q", got, want)
	}
}
