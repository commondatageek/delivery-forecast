package app

import (
	"fmt"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

// BacktestRequest asks how a project's (or milestone's) day-by-day completion
// probability would have looked, replayed from ReplayStart to TargetEnd.
type BacktestRequest struct {
	SimBase
	Project     string    // required, exact match
	Milestone   string    // optional, exact match within Project
	ReplayStart time.Time // zero = earliest started_at across the issue set, as a local day
	TargetEnd   time.Time // required
	Today       time.Time // local midnight; rows after it are Projected
}

// BacktestRow is one replayed day.
type BacktestRow struct {
	Date      time.Time
	Completed int
	Remaining int
	Prob      float64 // percent
	Projected bool    // Date.After(Today)
}

// BacktestResult is the outcome of SimBacktest.
type BacktestResult struct {
	Warnings            []string
	Run                 SimRun // HorizonCalendar is nil
	Scope               string // "Project" or "Project / Milestone"
	IssueCount          int    // len(backtested set)
	ReplayStart         time.Time
	ReplayStartInferred bool
	Rows                []BacktestRow
}

// backtestIssueSet selects the fixed set of issues sim backtest replays
// against: exact project match (required), exact milestone match (optional,
// only within that project), excluding canceled issues — Issue.IsCanceled
// also covers "duplicate" (D7), matching the old ProjectMilestoneIssues
// query's `state_type NOT IN ('canceled', 'duplicate')`. No team filtering,
// same as before.
func backtestIssueSet(all []issues.Issue, project, milestone string) []issues.Issue {
	scoped := issues.Filter{Project: project, Milestone: milestone}.Apply(all)
	var out []issues.Issue
	for _, it := range scoped {
		if it.IsCanceled() {
			continue
		}
		out = append(out, it)
	}
	return out
}

// Validate checks everything about the request that doesn't need Issues.
func (r BacktestRequest) Validate() error {
	if r.Project == "" {
		return &FieldError{Fields: []string{"project"}, Msg: "-project is required"}
	}
	if r.TargetEnd.IsZero() {
		return &FieldError{Fields: []string{"target-end-date"}, Msg: "-target-end-date is required"}
	}
	if _, err := r.mode(); err != nil {
		return err
	}
	if err := r.validateWindow(); err != nil {
		return err
	}
	if r.Today.IsZero() {
		return &FieldError{Fields: []string{"today"}, Msg: "today must be set"}
	}
	return nil
}

// SimBacktest replays probability forecasts day by day for a project or
// milestone against a fixed sample pool.
func SimBacktest(req BacktestRequest) (*BacktestResult, error) {
	res := &BacktestResult{}
	if err := req.Validate(); err != nil {
		return res, err
	}
	mode, err := req.mode()
	if err != nil {
		return res, err
	}

	// Build the fixed sample pool once; reused for every backtested day.
	run, warns, err := preparePool(req.SimBase, mode, false)
	res.Warnings, res.Run = warns, run
	if err != nil {
		return res, err
	}

	scoped := backtestIssueSet(req.Issues, req.Project, req.Milestone)
	if len(scoped) == 0 {
		if req.Milestone != "" {
			return res, &FieldError{
				Fields: []string{"project", "milestone"},
				Msg:    fmt.Sprintf("no issues found for project %q milestone %q — check spelling", req.Project, req.Milestone),
			}
		}
		return res, &FieldError{
			Fields: []string{"project"},
			Msg:    fmt.Sprintf("no issues found for project %q — check spelling", req.Project),
		}
	}
	res.IssueCount = len(scoped)
	res.Scope = req.Project
	if req.Milestone != "" {
		res.Scope += " / " + req.Milestone
	}

	btItems := issuesToBacktestItems(scoped)

	// Resolve start date: explicit value wins; otherwise infer from the
	// earliest started_at across the issue set.
	startDate := req.ReplayStart
	if startDate.IsZero() {
		startDate = simulate.EarliestStartedAt(btItems)
		if startDate.IsZero() {
			return res, &FieldError{Fields: []string{"replay-start-date"}, Msg: "no started_at found in issue set; provide -replay-start-date explicitly"}
		}
		startDate = util.LocalDay(startDate)
		res.ReplayStartInferred = true
	}
	res.ReplayStart = startDate

	if !req.TargetEnd.After(startDate) {
		return res, &FieldError{
			Fields: []string{"target-end-date", "replay-start-date"},
			Msg:    fmt.Sprintf("-target-end-date must be after -replay-start-date (inferred: %s)", startDate.Format("2006-01-02")),
		}
	}

	rows := simulate.RunBacktest(run.Pool, btItems, startDate, req.TargetEnd, simulate.Params{
		Mode:          mode,
		Engineers:     req.Engineers,
		EngineerNames: req.EngineerNames,
		Calendar:      simulate.NewCalendar(req.Exclusions, startDate),
		Simulations:   req.Simulations,
		Workers:       req.Workers,
		Seed:          req.Seed,
		Progress:      req.Progress,
	})
	for _, r := range rows {
		res.Rows = append(res.Rows, BacktestRow{
			Date:      r.Date,
			Completed: r.Completed,
			Remaining: r.Remaining,
			Prob:      r.Prob,
			Projected: r.Date.After(req.Today),
		})
	}
	return res, nil
}
