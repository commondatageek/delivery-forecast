// Package counts folds outstanding-issue counts into a per-project (and
// per-milestone) report: Compute groups ProjectMilestoneCount rows by
// project, drops projects whose most recent activity (ProjectActivity)
// predates a cutoff, and sorts the result most-recently-updated first.
//
// The package is pure and IO-free. Aggregate builds Compute's two inputs
// directly from a flat []Issue — the in-memory equivalent of the two SQL
// aggregate queries (NotCompletedCounts, ProjectLastUpdated) the CLI used to
// read from sqlite.Store, so any source (SQLite, CSV, JSON) produces
// identical counts. Compute itself does no state filtering (that's
// Aggregate's job, via Issue.StateType — count's source data carries no
// completed_at/canceled_at to fall back on, unlike aging/sim, so an omitted
// state_type undercounts nothing as terminal; see Issue's doc comment), only
// the since-cutoff, grouping, and sorting. Since is a plain threshold
// compared with time.Time.Before, not a window start, so there's no
// day-bucketing convention to observe here (contrast package simulate).
package counts
