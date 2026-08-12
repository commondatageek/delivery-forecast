package issues

import "strings"

// Filter selects a subset of issues. A zero Filter matches everything.
type Filter struct {
	// Teams matches Issue.TeamKey, case-insensitively. Empty means all teams.
	Teams []string
	// Project matches Issue.ProjectName exactly. Empty means all projects.
	Project string
	// Milestone matches Issue.ProjectMilestoneName exactly. Empty means all
	// milestones. Only meaningful together with Project.
	Milestone string
}

// Apply returns the issues matching f, preserving input order.
func (f Filter) Apply(in []Issue) []Issue {
	if len(f.Teams) == 0 && f.Project == "" && f.Milestone == "" {
		return in
	}

	teams := make(map[string]bool, len(f.Teams))
	for _, t := range f.Teams {
		teams[strings.ToUpper(strings.TrimSpace(t))] = true
	}

	out := make([]Issue, 0, len(in))
	for _, it := range in {
		if len(teams) > 0 && !teams[strings.ToUpper(it.TeamKey)] {
			continue
		}
		if f.Project != "" && it.ProjectName != f.Project {
			continue
		}
		if f.Milestone != "" && it.ProjectMilestoneName != f.Milestone {
			continue
		}
		out = append(out, it)
	}
	return out
}
