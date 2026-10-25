package app

import (
	"fmt"
	"time"

	"github.com/commondatageek/delivery-forecast/history"
	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/issues"
)

// HistoryRequest asks for the per-day flow metrics.
type HistoryRequest struct {
	Issues     []issues.Issue
	Teams      []string
	Project    string
	Milestone  string
	Start      time.Time // zero = earliest created_at in scope (local day)
	End        time.Time
	WindowDays int // the CLI passes its -window value; 0 is rejected by Validate
}

// HistoryResult is the outcome of RunHistory.
type HistoryResult struct {
	Warnings      []string
	Result        history.Result
	Start         time.Time
	StartInferred bool
}

// Validate checks everything about the request that doesn't need Issues.
func (r HistoryRequest) Validate() error {
	if r.WindowDays <= 0 {
		return &FieldError{Fields: []string{"window"}, Msg: "-window must be positive"}
	}
	return nil
}

// RunHistory computes the per-day flow metrics for the scoped issues.
func RunHistory(req HistoryRequest) (*HistoryResult, error) {
	res := &HistoryResult{}
	if err := req.Validate(); err != nil {
		return res, err
	}

	filtered := issues.Filter{Teams: req.Teams, Project: req.Project, Milestone: req.Milestone}.Apply(req.Issues)
	if len(filtered) == 0 {
		switch {
		case req.Milestone != "":
			return res, &FieldError{
				Fields: []string{"project", "milestone"},
				Msg:    fmt.Sprintf("no issues found for project %q milestone %q — check spelling", req.Project, req.Milestone),
			}
		case req.Project != "":
			return res, &FieldError{
				Fields: []string{"project"},
				Msg:    fmt.Sprintf("no issues found for project %q — check spelling", req.Project),
			}
		default:
			return res, &FieldError{Fields: []string{"teams"}, Msg: "no issues found for the given filters — check spelling"}
		}
	}

	// Unlike aging/cfd/count, the blending warning here is computed on the
	// filtered set, not the full one.
	if msg := BlendingTeamsWarning(req.Teams, DistinctTeamKeys(filtered)); msg != "" {
		res.Warnings = append(res.Warnings, msg)
	}

	historyIssues := toHistoryIssues(filtered)

	startDate := req.Start
	if startDate.IsZero() {
		// Differs from sim backtest, which defaults to the earliest
		// started_at: history's Total is a created-to-date burnup line, so
		// created_at is the right anchor here.
		startDate = util.LocalDay(history.EarliestCreatedAt(historyIssues))
		if startDate.IsZero() {
			return res, &FieldError{Fields: []string{"start"}, Msg: "no created_at found in issue set; provide -start explicitly"}
		}
		res.StartInferred = true
	}
	res.Start = startDate

	if req.End.Before(startDate) {
		return res, &FieldError{
			Fields: []string{"end", "start"},
			Msg:    fmt.Sprintf("-end must not be before -start (%s)", startDate.Format("2006-01-02")),
		}
	}

	result, err := history.Compute(historyIssues, history.Options{Start: startDate, End: req.End, WindowDays: req.WindowDays})
	if err != nil {
		return res, err
	}
	if err := history.AssertInvariants(result.Rows); err != nil {
		return res, fmt.Errorf("history invariant violated: %w", err)
	}
	res.Result = result
	return res, nil
}
