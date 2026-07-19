// Package simulate is a pure, IO-free Monte Carlo forecasting engine: given a
// history of daily completion counts, it answers "how many items in D days?"
// (ItemsInDays), "how many days for I items?" (DaysToComplete), "what's the
// probability of I items in D days?" (ProbabilityAtLeast), and replays those
// forecasts day-by-day against actual history (RunBacktest).
//
// The package does no file, network, or database access and never calls
// time.Now — callers own that boundary entirely. Build a SamplePool from your
// own Completion records via BuildPool, then feed it to ItemsInDays,
// DaysToComplete, or RunBacktest via Params.
//
// Day bucketing: BuildPool bins Completions into per-engineer daily counts
// over the half-open window [startDate, endDate) — startDate inclusive,
// endDate exclusive — and preserves zero-completion days as zero samples
// (dropping them would bias every forecast upward). startDate should be
// local midnight: BuildPool/DaysBetween bucket by whole days via a
// start-anchored day index, and a startDate carrying a time-of-day offset can
// round a same-day completion into the wrong (or an out-of-range) bucket.
// endDate may carry a time-of-day component (e.g. "now"); the day it falls on
// then gets one partial, inclusive slot instead of being excluded.
package simulate
