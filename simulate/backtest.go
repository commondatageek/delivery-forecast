package simulate

import (
	"time"

	"github.com/commondatageek/delivery-forecast/history"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

// BacktestItem is the neutral per-issue record the backtest needs: just the
// timestamps. The cmd layer converts source-specific records (e.g. issues.Issue)
// to BacktestItem before calling RunBacktest.
type BacktestItem struct {
	CreatedAt   time.Time
	StartedAt   time.Time
	CompletedAt time.Time
}

// BacktestRow is one day's entry in the backtest output.
type BacktestRow struct {
	Date      time.Time
	Completed int
	Remaining int
	Prob      float64
}

// CountAsOf counts how many items in the fixed set were completed by midnight
// of d, and how many had been created by that point but were not yet complete.
//
// RunBacktest no longer calls this (it now walks history.Compute's rows
// instead, which day-truncate and clamp timestamps before counting — see D4
// in HISTORY_PLAN.md). CountAsOf keeps its original raw-timestamp semantics
// on purpose: it's the baseline history_test.go's cross-check test compares
// against to document that truncation's effect on boundary days, and
// reimplementing it as a thin wrapper over history would collapse the very
// divergence that test exists to demonstrate.
//
// Deprecated: CountAsOf is retained only as that test baseline. Its
// raw-timestamp comparison disagrees with every other day-grain count in this
// module on boundary days. New code — inside this module or outside it —
// should use history.Compute and read DayRow.Completed / DayRow.Remaining.
func CountAsOf(items []BacktestItem, d time.Time) (completed, remaining int) {
	for _, it := range items {
		completedByD := !it.CompletedAt.IsZero() && !it.CompletedAt.After(d)
		notYetCreated := !it.CreatedAt.IsZero() && it.CreatedAt.After(d)
		switch {
		case completedByD:
			completed++
		case notYetCreated:
			// neither column
		default:
			remaining++
		}
	}
	return
}

// EarliestStartedAt returns the minimum non-zero StartedAt across all items,
// or the zero time if none have one.
func EarliestStartedAt(items []BacktestItem) time.Time {
	var earliest time.Time
	for _, it := range items {
		if it.StartedAt.IsZero() {
			continue
		}
		if earliest.IsZero() || it.StartedAt.Before(earliest) {
			earliest = it.StartedAt
		}
	}
	return earliest
}

// AllCreatedBy reports whether every item had been created by d.
func AllCreatedBy(items []BacktestItem, d time.Time) bool {
	for _, it := range items {
		if !it.CreatedAt.IsZero() && it.CreatedAt.After(d) {
			return false
		}
	}
	return true
}

// RunBacktest replays probability forecasts day-by-day from startDate through
// targetDate (inclusive) using the fixed items set and sample pool. On each day
// it counts completed/remaining items and runs a Monte Carlo forecast for the
// remaining window. The loop exits early once all items are complete and have
// been created.
//
// The day-by-day completed/remaining counts come from history.Compute (the
// same day-walk engine forecast history and cfd.BuildGrid use), not a
// bespoke loop, so all three agree on which day a boundary event lands on.
// This is D4 in HISTORY_PLAN.md: history day-truncates and clamps timestamps
// before counting, where the old inline loop compared raw timestamps, so a
// completion that lands mid-day can now count a calendar day earlier than it
// used to. items carries no CanceledAt (the backtested issue set already
// excludes canceled/duplicate issues via SQL), so history's Canceled tier is
// always zero here and Remaining reduces to Total−Completed, matching the
// old behavior exactly modulo that truncation.
//
// p.Calendar, when set, is anchored at startDate; on each replayed day r the
// forecast horizon is [r, targetDate], so the calendar is rebased to r and any
// exclusions inside that range apply as they would have on that day (D9).
func RunBacktest(pool *SamplePool, items []BacktestItem, startDate, targetDate time.Time, p Params) []BacktestRow {
	historyItems := make([]history.Issue, len(items))
	for i, it := range items {
		historyItems[i] = history.Issue{CreatedAt: it.CreatedAt, StartedAt: it.StartedAt, CompletedAt: it.CompletedAt}
	}
	// startDate/targetDate are validated by the caller (cmd/forecast/backtest.go
	// requires targetDate after startDate) before RunBacktest is ever called,
	// so history.Compute's Start/End preconditions always hold here.
	res, err := history.Compute(historyItems, history.Options{Start: startDate, End: targetDate})
	if err != nil {
		return nil
	}

	var rows []BacktestRow
	for _, r := range res.Rows {
		daysToTarget := util.DayIndex(targetDate, r.Date) + 1

		var prob float64
		if r.Remaining == 0 {
			prob = 100.0
		} else {
			dist := ItemsInDays(pool, Params{
				Mode:          p.Mode,
				Engineers:     p.Engineers,
				EngineerNames: p.EngineerNames,
				Calendar:      p.Calendar.Rebase(r.Date),
				Days:          daysToTarget,
				Simulations:   p.Simulations,
				Workers:       p.Workers,
				Seed:          p.Seed,
			})
			prob = ProbabilityAtLeast(dist, r.Remaining)
		}
		rows = append(rows, BacktestRow{r.Date, r.Completed, r.Remaining, prob})

		if r.Remaining == 0 && AllCreatedBy(items, r.Date) {
			break
		}
	}
	return rows
}
