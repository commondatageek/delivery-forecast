// Package history builds a per-day series of flow metrics from per-issue
// lifecycle timestamps: the cheap, deterministic, no-Monte-Carlo twin of
// simulate.RunBacktest, meant to be piped into a plotting script.
//
// The package is pure and IO-free and never calls time.Now(); Options.Start
// and Options.End are supplied by the caller. Day boundaries are local
// midnight (util.LocalDay); "as of day D" means "at end of day D" — an event
// whose timestamp truncates to D has happened as of D.
//
// Compute reuses cfd.Normalize to clamp each issue's four lifecycle
// timestamps into a monotonically non-decreasing (Arrival, LeftBacklog,
// Exit, ExitType) tuple truncated to day resolution, exactly as cfd does, so
// the two packages agree on which day a boundary event lands on (see
// history_test.go's cross-check against cfd.BuildGrid). Issues with no
// CreatedAt are dropped and counted in Result.SkippedIssues.
//
// Each emitted DayRow has three tiers of columns:
//
//   - Tier 1, cumulative state as of D: Total (the burnup scope line — it
//     grows over time), Completed, Canceled, Backlog, InProgress, and
//     Remaining (= Backlog + InProgress = Total − Completed − Canceled, not
//     Total − Completed: canceled issues are excluded from "remaining", not
//     silently counted as outstanding work).
//   - Tier 2, daily deltas: *Delta is the count of events landing exactly on
//     day D, i.e. the cumulative column's forward difference. The first
//     emitted row has no prior day to diff against, so its delta equals its
//     own cumulative value.
//   - Tier 2, rolling metrics: trailing windows are inclusive of D and
//     cover the N days before it, i.e. (D−N, D], where N is
//     Options.WindowDays (default 28) except Throughput7d, which is always
//     a 7-day window. The window looks at the full issue set passed to
//     Compute, not at the emitted [Start, End] range, so the first emitted
//     row still gets a meaningful rolling value when history exists before
//     it.
//
// Undefined values (an empty sample — no completions in a throughput
// window, no WIP on a given day — or a division by zero throughput) are
// math.NaN(), never zero: a real zero and a missing value mean opposite
// things on a chart, and NaN propagates cleanly through the downstream
// LittlesLawCT/DaysRemainingAtRate divisions instead of producing +Inf.
// Renderers turn NaN into an empty CSV cell, JSON null, or "-" in text.
//
// Compute never early-exits: it emits one row for every calendar day in
// [Start, End], even once every issue is done.
package history
