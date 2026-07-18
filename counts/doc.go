// Package counts folds outstanding-issue counts into a per-project (and
// per-milestone) report: Compute groups ProjectMilestoneCount rows by
// project, drops projects whose most recent activity (ProjectActivity)
// predates a cutoff, and sorts the result most-recently-updated first.
//
// The package is pure and IO-free. Callers supply their own
// ProjectMilestoneCount/ProjectActivity records (the CLI maps
// sqlite.ProjectMilestoneCount/sqlite.ProjectActivity via
// toProjectMilestoneCounts/toProjectActivity), already restricted to whatever
// issue set the caller considers "outstanding" — Compute itself does no state
// filtering, only the since-cutoff, grouping, and sorting. Since is a plain
// threshold compared with time.Time.Before, not a window start, so there's no
// day-bucketing convention to observe here (contrast package simulate).
package counts
