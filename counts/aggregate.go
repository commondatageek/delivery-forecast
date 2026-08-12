package counts

import "time"

// Issue is the neutral per-issue input Aggregate reads: just the grouping
// keys, the raw state_type, and updated_at. The caller maps its source's
// fields onto it (the CLI maps issues.Issue).
//
// Unlike aging/sim, there is no timestamp-based fallback for "is this
// terminal" here — count's source data carries no completed_at/canceled_at
// at all, only state_type — so a file source that omits state_type entirely
// will count every issue as outstanding, including ones a spreadsheet
// exporter (or a human) would call done. This is the sharpest edge of D7's
// "state_type is optional" story; document it, don't paper over it.
type Issue struct {
	TeamKey              string
	TeamName             string
	ProjectName          string
	ProjectMilestoneName string
	StateType            string
	UpdatedAt            time.Time
}

// isTerminalState reports whether stateType is one of the three terminal
// states Aggregate excludes from the not-completed counts: completed,
// canceled, or duplicate. Mirrors NotCompletedCounts' SQL predicate
// (`state_type NOT IN ('completed', 'canceled', 'duplicate')`) exactly.
func isTerminalState(stateType string) bool {
	return stateType == "completed" || stateType == "canceled" || stateType == "duplicate"
}

// Aggregate computes not-completed issue counts (grouped by team, project,
// and milestone) and per-project last-updated timestamps (across ALL
// issues, terminal included) directly from a flat issue list — the
// in-memory equivalent of the two SQL queries (NotCompletedCounts,
// ProjectLastUpdated) Compute previously read via sqlite.Store, so any
// source works identically. Feed both return values into Compute.
func Aggregate(all []Issue) ([]ProjectMilestoneCount, []ProjectActivity) {
	type actKey struct{ team, teamName, project string }
	lastUpdated := make(map[actKey]time.Time)
	var actOrder []actKey
	for _, it := range all {
		k := actKey{it.TeamKey, it.TeamName, it.ProjectName}
		if _, ok := lastUpdated[k]; !ok {
			actOrder = append(actOrder, k)
		}
		if it.UpdatedAt.After(lastUpdated[k]) {
			lastUpdated[k] = it.UpdatedAt
		}
	}
	activity := make([]ProjectActivity, len(actOrder))
	for i, k := range actOrder {
		activity[i] = ProjectActivity{TeamKey: k.team, TeamName: k.teamName, ProjectName: k.project, LastUpdated: lastUpdated[k]}
	}

	type countKey struct{ team, teamName, project, milestone string }
	counted := make(map[countKey]int)
	var countOrder []countKey
	for _, it := range all {
		if isTerminalState(it.StateType) {
			continue
		}
		k := countKey{it.TeamKey, it.TeamName, it.ProjectName, it.ProjectMilestoneName}
		if _, ok := counted[k]; !ok {
			countOrder = append(countOrder, k)
		}
		counted[k]++
	}
	pmCounts := make([]ProjectMilestoneCount, len(countOrder))
	for i, k := range countOrder {
		pmCounts[i] = ProjectMilestoneCount{
			TeamKey: k.team, TeamName: k.teamName,
			ProjectName: k.project, MilestoneName: k.milestone,
			Count: counted[k],
		}
	}

	return pmCounts, activity
}
