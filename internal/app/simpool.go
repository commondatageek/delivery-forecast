package app

import (
	"fmt"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

// SimBase is the request block shared by every sim subcommand: the flags
// addSimFlags registers, resolved to typed values.
type SimBase struct {
	Issues           []issues.Issue
	Exclusions       simulate.Exclusions // already loaded; empty = none
	TypicalEngineers []string            // empty = every assignee
	Engineers        int                 // > 0 iff -engineers given
	EngineerNames    []string            // len == Engineers when names were given
	WholeTeam        bool
	SampleStart      time.Time
	SampleEnd        time.Time
	Simulations      int
	Workers          int
	Seed             int64
	Progress         func(done, total int) // optional
}

// SimRun is what a sim run resolved and built: everything a report header or
// a run manifest needs besides the subcommand's own results.
type SimRun struct {
	Mode            simulate.Mode
	ModeLabel       string
	Seed            int64
	SampleStart     time.Time
	SampleEnd       time.Time
	Pool            *simulate.SamplePool
	PoolIssues      []issues.Issue  // the completed issues that fed the pool
	Skipped         int             // dropped by simulate.FilterInvalid
	Assignees       map[string]bool // engineers with >= 1 completion in the window
	Exclusions      simulate.Exclusions
	SampleCalendar  *simulate.Calendar // anchored at SampleStart
	HorizonCalendar *simulate.Calendar // anchored at the target start; nil for backtest
}

// mode resolves which sampling strategy the request asked for, blaming the
// -engineers / -whole-team pair when neither or both were given.
func (b SimBase) mode() (simulate.Mode, error) {
	mode, err := simulate.ResolveMode(b.Engineers > 0, b.WholeTeam)
	if err != nil {
		return 0, &FieldError{Fields: []string{"engineers", "whole-team"}, Msg: err.Error()}
	}
	return mode, nil
}

// completedForPool selects completed issues assigned to one of engineers (or
// any assignee, if engineers is empty) whose completed_at falls in
// [start, end) — start inclusive, end exclusive. Mirrors the old
// CompletedBetween SQL query (state_type = 'completed', now Issue.
// IsCompleted so file sources without state_type still work per D7), except
// matching is now case-sensitive-exact against the given engineer names, same
// as the SQL IN clause was.
//
// Unlike aging's equivalent, this deliberately keeps CompletedBetween's
// unconditional non-empty-assignee requirement: the pool is per-engineer, so
// an unassigned issue has nowhere to go. See completedBetween in aging.go for
// the contrasting case.
func completedForPool(items []issues.Issue, start, end time.Time, engineers []string) []issues.Issue {
	want := make(map[string]bool, len(engineers))
	for _, e := range engineers {
		want[e] = true
	}
	var out []issues.Issue
	for _, it := range items {
		if !it.IsCompleted() || it.CompletedAt.IsZero() || it.Assignee == "" {
			continue
		}
		if it.CompletedAt.Before(start) || !it.CompletedAt.Before(end) {
			continue
		}
		if len(want) > 0 && !want[it.Assignee] {
			continue
		}
		out = append(out, it)
	}
	return out
}

// unmatchedExclusionNames returns, sorted, the engineer names in exc that are
// neither an assignee in the sample pool nor a named -engineers slot. A name
// may legitimately match only one side (a new hire has no history; a departed
// engineer has history but no slot), so this is only ever a warning.
func unmatchedExclusionNames(exc simulate.Exclusions, assignees map[string]bool, slotNames []string) []string {
	slots := make(map[string]bool, len(slotNames))
	for _, n := range slotNames {
		slots[n] = true
	}
	var out []string
	for _, name := range exc.Scopes() {
		if !assignees[name] && !slots[name] {
			out = append(out, name)
		}
	}
	return out
}

// preparePool selects the completed issues, builds the sample pool, and
// validates it for mode. requireProgress is forwarded to
// simulate.ValidatePool. Warnings are returned in the order the CLI used to
// log them.
func preparePool(b SimBase, mode simulate.Mode, requireProgress bool) (SimRun, []string, error) {
	var warnings []string

	completed := completedForPool(b.Issues, b.SampleStart, b.SampleEnd, b.TypicalEngineers)

	engineerSeen := make(map[string]bool, len(completed))
	for _, it := range completed {
		engineerSeen[it.Assignee] = true
	}
	for _, name := range b.TypicalEngineers {
		if !engineerSeen[name] {
			warnings = append(warnings, fmt.Sprintf("-typical-engineers engineer %q not found in data", name))
		}
	}

	records, skipped := simulate.FilterInvalid(issuesToCompletions(completed))
	if skipped > 0 {
		warnings = append(warnings, fmt.Sprintf("skipped %d completed issue(s) with no assignee or completion date", skipped))
	}

	cal := simulate.NewCalendar(b.Exclusions, b.SampleStart)
	pool := simulate.BuildPool(records, cal, b.SampleStart, b.SampleEnd, b.WholeTeam)

	if b.WholeTeam {
		if len(b.Exclusions.Scopes()) > 0 {
			warnings = append(warnings, "exclusions: per-engineer entries are ignored in -whole-team mode")
		}
	} else {
		for _, n := range unmatchedExclusionNames(b.Exclusions, engineerSeen, b.EngineerNames) {
			warnings = append(warnings, fmt.Sprintf("exclusions: engineer %q matches no assignee in the sample data and no -engineers name", n))
		}
	}

	run := SimRun{
		Mode:           mode,
		ModeLabel:      simulate.ModeLabel(mode, b.Engineers, b.EngineerNames),
		Seed:           b.Seed,
		SampleStart:    b.SampleStart,
		SampleEnd:      b.SampleEnd,
		Pool:           pool,
		PoolIssues:     completed,
		Skipped:        skipped,
		Assignees:      engineerSeen,
		Exclusions:     b.Exclusions,
		SampleCalendar: cal,
	}
	if err := simulate.ValidatePool(pool, mode, requireProgress); err != nil {
		return run, warnings, err
	}
	return run, warnings, nil
}

// PreparePoolForBacktest is a temporary export of preparePool for
// cmd/forecast's backtest, which moves into this package in Phase 3.
// Removed in Phase 3.
func PreparePoolForBacktest(b SimBase) (SimRun, []string, error) {
	mode, err := b.mode()
	if err != nil {
		return SimRun{}, nil, err
	}
	return preparePool(b, mode, false)
}
