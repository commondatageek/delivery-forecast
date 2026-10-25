package app

import (
	"fmt"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

// CheckResult is one command's readiness verdict against a loaded issue set.
type CheckResult struct {
	Command  string // "history", "cfd", "aging", "count", "sim"
	Excluded int    // issues the command will silently drop; 0 = ok
	Status   string // "ok" or the exact sentence check prints today
}

// CheckReadiness evaluates a loaded issue set against every forecast
// command's data requirements (see DATA_REQUIREMENTS.md's per-command table),
// reporting "ok" or a specific reason some issues will be silently excluded.
// It never returns an error — a bad fit for one command doesn't stop the
// others from being reported — which is what makes it safe to run against an
// unfamiliar export before trying any real command.
func CheckReadiness(items []issues.Issue) []CheckResult {
	if len(items) == 0 {
		const msg = "no issues in input"
		return []CheckResult{
			{"history", 0, msg}, {"cfd", 0, msg}, {"aging", 0, msg}, {"count", 0, msg}, {"sim", 0, msg},
		}
	}

	var noCreatedAt, noUpdatedAt, noStartedAtCompleted, noAssigneeCompleted int
	for _, it := range items {
		if it.CreatedAt.IsZero() {
			noCreatedAt++
		}
		if it.UpdatedAt.IsZero() {
			noUpdatedAt++
		}
		if !it.IsCompleted() {
			continue
		}
		if it.StartedAt.IsZero() {
			noStartedAtCompleted++
		}
		if it.Assignee == "" {
			noAssigneeCompleted++
		}
	}

	return []CheckResult{
		{"history", noCreatedAt, excludedStatus(noCreatedAt, "issue", "no created_at and will be excluded")},
		{"cfd", noCreatedAt, excludedStatus(noCreatedAt, "issue", "no created_at and will be excluded")},
		{"aging", noStartedAtCompleted, excludedStatus(noStartedAtCompleted, "completed issue", "no started_at and will be excluded from the cycle-time distribution")},
		{"count", noUpdatedAt, excludedStatus(noUpdatedAt, "issue", "no updated_at; a project is hidden unless one of its issues was updated since -updated-since")},
		{"sim", noAssigneeCompleted, excludedStatus(noAssigneeCompleted, "completed issue", "no assignee and will be excluded")},
	}
}

// excludedStatus renders "ok" for n == 0, otherwise a count-and-reason
// sentence, e.g. "12 completed issues have no assignee and will be excluded"
// (n > 1) or "1 completed issue has no assignee and will be excluded" (n == 1).
func excludedStatus(n int, noun, reason string) string {
	if n == 0 {
		return "ok"
	}
	if n == 1 {
		return fmt.Sprintf("1 %s has %s", noun, reason)
	}
	return fmt.Sprintf("%d %ss have %s", n, noun, reason)
}

// Assignees returns the set of non-empty Assignee values in items.
func Assignees(items []issues.Issue) map[string]bool {
	assignees := make(map[string]bool)
	for _, it := range items {
		if it.Assignee != "" {
			assignees[it.Assignee] = true
		}
	}
	return assignees
}

// ExclusionsEngineer is one per-engineer scope in an exclusions file.
type ExclusionsEngineer struct {
	Name    string
	Entries int
}

// ExclusionsReport is what check reports about an exclusions file.
type ExclusionsReport struct {
	ParseErr       error // non-nil: file unreadable/unparseable; other fields zero
	GlobalEntries  int
	Engineers      []ExclusionsEngineer // sorted by name (exc.Scopes() order)
	HasDates       bool
	First, Last    time.Time
	Past, Future   int
	Today          time.Time
	UnmatchedNames []string // engineer scopes with no matching assignee, in Scopes() order
}

// CheckExclusions summarises an exclusions file against the loaded issues'
// assignees. parseErr is the read-or-parse error, if any. today is the
// caller's clock (local midnight). A date counts as "past" when it is today
// or earlier (today's completions are already in the default sample window)
// and "future" when it is after today.
func CheckExclusions(exc simulate.Exclusions, parseErr error, assignees map[string]bool, today time.Time) ExclusionsReport {
	if parseErr != nil {
		return ExclusionsReport{ParseErr: parseErr}
	}

	names := exc.Scopes()
	r := ExclusionsReport{GlobalEntries: len(exc.Global), Today: today}
	for _, n := range names {
		r.Engineers = append(r.Engineers, ExclusionsEngineer{Name: n, Entries: len(exc.Engineers[n])})
	}

	distinct := make(map[int64]time.Time)
	for _, scope := range append([]string{""}, names...) {
		for _, d := range exc.Days(scope) {
			distinct[d.Unix()] = d
		}
	}
	if len(distinct) > 0 {
		r.HasDates = true
		for _, d := range distinct {
			if r.First.IsZero() || d.Before(r.First) {
				r.First = d
			}
			if r.Last.IsZero() || d.After(r.Last) {
				r.Last = d
			}
			if !d.After(today) {
				r.Past++
			}
		}
		r.Future = len(distinct) - r.Past
	}

	for _, n := range names {
		if !assignees[n] {
			r.UnmatchedNames = append(r.UnmatchedNames, n)
		}
	}
	return r
}
