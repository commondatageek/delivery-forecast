package app

import (
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
)

// day returns local midnight on the given date.
func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func TestToHistoryIssues(t *testing.T) {
	created := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	started := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	completed := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC)
	canceled := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC)

	in := []issues.Issue{
		{
			Identifier: "ENG-1", TeamKey: "ENG", // non-timestamp fields must not leak through
			CreatedAt: created, StartedAt: started, CompletedAt: completed, CanceledAt: canceled,
		},
	}
	out := toHistoryIssues(in)
	if len(out) != 1 {
		t.Fatalf("got %d issues, want 1", len(out))
	}
	got := out[0]
	if !got.CreatedAt.Equal(created) || !got.StartedAt.Equal(started) ||
		!got.CompletedAt.Equal(completed) || !got.CanceledAt.Equal(canceled) {
		t.Errorf("got %+v", got)
	}
}

func TestToCountsIssues_TerminalFromStatusOrTimestamps(t *testing.T) {
	got := toCountsIssues([]issues.Issue{
		{Identifier: "E-1", Status: issues.StatusCanceled},
		{Identifier: "E-2", Status: issues.StatusStarted, CompletedAt: day(2025, 1, 2)},
		{Identifier: "E-3", Status: issues.StatusBacklog},
	})
	for i, want := range []bool{true, true, false} {
		if got[i].Terminal != want {
			t.Errorf("item %d: Terminal = %v, want %v", i, got[i].Terminal, want)
		}
	}
}
