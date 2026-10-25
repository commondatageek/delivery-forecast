package app

import (
	"fmt"
	"sort"
	"time"

	"github.com/commondatageek/delivery-forecast/aging"
	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/issues"
)

// AgingRequest asks for the WIP-age / cycle-time report.
type AgingRequest struct {
	Issues        []issues.Issue
	aging.Options // Teams, SampleStart, SampleEnd, MinCycleTime, Percentile
	ShowCompleted bool
	Today         time.Time // local midnight
}

// AgingResult is the outcome of RunAging.
type AgingResult struct {
	Warnings   []string
	Meta       aging.Meta
	InProgress []aging.Item // sorted by AgeDays desc
	Completed  []aging.Item // nil unless ShowCompleted; sorted by AgeDays desc
	CycleTimes []float64    // sorted asc
}

// completedBetween selects items completed in [start, end) (start inclusive,
// end exclusive), using Issue.IsCompleted rather than a raw state_type
// check so file sources that omit state_type still work (D7).
//
// Unlike sim's sample pool, this deliberately does not require a non-empty
// assignee: aging never filters or groups by assignee, so an unassigned
// completed issue still belongs in the cycle-time distribution. The old
// SQL-backed CompletedBetween required one unconditionally (a query it
// shares with sim, where the pool genuinely is per-engineer and an
// unassigned issue has nowhere to go), which silently dropped unassigned
// completed issues from aging's distribution — see DATA_REQUIREMENTS.md
// footnote 1. This fixes that for every source, not just file ones.
func completedBetween(items []issues.Issue, start, end time.Time) []issues.Issue {
	var out []issues.Issue
	for _, it := range items {
		if !it.IsCompleted() || it.CompletedAt.IsZero() {
			continue
		}
		if it.CompletedAt.Before(start) || !it.CompletedAt.Before(end) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// inProgress selects items currently in progress, using Issue.IsInProgress
// rather than a raw state_type check (D7). aging.InProgressItems already
// skips issues with a zero StartedAt, matching the old query's
// `started_at IS NOT NULL`.
func inProgress(items []issues.Issue) []issues.Issue {
	var out []issues.Issue
	for _, it := range items {
		if it.IsInProgress() {
			out = append(out, it)
		}
	}
	return out
}

// Validate checks everything about the request that doesn't need Issues.
func (r AgingRequest) Validate() error {
	if r.Percentile < 1 || r.Percentile > 100 {
		return &FieldError{Fields: []string{"percentile"}, Msg: fmt.Sprintf("-percentile must be between 1 and 100, got %d", r.Percentile)}
	}
	if !r.SampleStart.Before(r.SampleEnd) {
		return &FieldError{Fields: []string{"sample-start", "sample-end"}, Msg: "-sample-start must be before -sample-end"}
	}
	return nil
}

// RunAging ranks the in-progress issues against the historical cycle-time
// distribution of the issues completed in the sample window.
func RunAging(req AgingRequest) (*AgingResult, error) {
	res := &AgingResult{}
	if err := req.Validate(); err != nil {
		return res, err
	}
	opts := req.Options

	if msg := BlendingTeamsWarning(opts.Teams, DistinctTeamKeys(req.Issues)); msg != "" {
		res.Warnings = append(res.Warnings, msg)
	}

	filtered := issues.Filter{Teams: opts.Teams}.Apply(req.Issues)

	agingCompleted := toAgingIssues(completedBetween(filtered, opts.SampleStart, opts.SampleEnd))

	cycleTimes := aging.CycleTimes(agingCompleted, opts.MinCycleTime)
	sort.Float64s(cycleTimes)

	threshold := util.PercentileValue(cycleTimes, float64(opts.Percentile))

	res.Meta = aging.Meta{
		Percentile:     opts.Percentile,
		Threshold:      threshold,
		SampleStart:    opts.SampleStart,
		SampleEnd:      opts.SampleEnd,
		CompletedCount: len(cycleTimes),
	}

	inProgressItems := aging.InProgressItems(toAgingIssues(inProgress(filtered)), req.Today)
	aging.RankItems(inProgressItems, cycleTimes, threshold)

	sort.Slice(inProgressItems, func(i, j int) bool {
		return inProgressItems[i].AgeDays > inProgressItems[j].AgeDays
	})

	var completedItems []aging.Item
	if req.ShowCompleted {
		completedItems = aging.CompletedItems(agingCompleted, opts.MinCycleTime)
		aging.RankItems(completedItems, cycleTimes, threshold)

		sort.Slice(completedItems, func(i, j int) bool {
			return completedItems[i].AgeDays > completedItems[j].AgeDays
		})
	}

	if len(cycleTimes) == 0 {
		res.Warnings = append(res.Warnings, "no completed issues found in the sample window; percentiles will be 0 and multipliers blank")
	}

	res.InProgress, res.Completed, res.CycleTimes = inProgressItems, completedItems, cycleTimes
	return res, nil
}
