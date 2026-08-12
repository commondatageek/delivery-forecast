package history

import (
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
)

// NormalizedIssue holds per-issue lifecycle event times clamped to be
// monotonically non-decreasing. All times are truncated to day resolution
// (local midnight). Zero means the event has not occurred.
//
// This type and Normalize originally lived in package cfd; they moved here
// so cfd.BuildGrid could be reimplemented on top of history's day-walk
// without an import cycle (history needs Normalize; cfd now depends on
// history rather than the reverse). cfd re-exports both as aliases.
type NormalizedIssue struct {
	Arrival     time.Time
	LeftBacklog time.Time
	Exit        time.Time
	ExitType    string // "completed" | "canceled" | ""
}

func truncDay(t time.Time) time.Time {
	return util.LocalDay(t)
}

func clampMin(a, floor time.Time) time.Time {
	if a.IsZero() {
		return a
	}
	if a.Before(floor) {
		return floor
	}
	return a
}

// Normalize converts an Issue into a NormalizedIssue with monotonically
// non-decreasing timestamps. Returns false if the issue has no created_at and
// should be dropped.
func Normalize(r Issue) (NormalizedIssue, bool) {
	arrival := truncDay(r.CreatedAt)
	if arrival.IsZero() {
		return NormalizedIssue{}, false
	}

	completed := truncDay(r.CompletedAt)
	canceled := truncDay(r.CanceledAt)
	started := truncDay(r.StartedAt)

	var leftBacklog time.Time
	if !started.IsZero() {
		leftBacklog = clampMin(started, arrival)
	} else if !completed.IsZero() || !canceled.IsZero() {
		// Canceled or completed without ever having started_at set.
		// Use the terminal time so the issue exits the backlog at the moment it exits the system.
		terminal := completed
		if terminal.IsZero() {
			terminal = canceled
		}
		leftBacklog = clampMin(terminal, arrival)
	}

	floor := leftBacklog
	if floor.IsZero() {
		floor = arrival
	}

	var exit time.Time
	var exitType string
	switch {
	case !completed.IsZero():
		exit = clampMin(completed, floor)
		exitType = "completed"
	case !canceled.IsZero():
		exit = clampMin(canceled, floor)
		exitType = "canceled"
	}

	return NormalizedIssue{
		Arrival:     arrival,
		LeftBacklog: leftBacklog,
		Exit:        exit,
		ExitType:    exitType,
	}, true
}
