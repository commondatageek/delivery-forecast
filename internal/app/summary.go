package app

import (
	"sort"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
)

// DatasetSummary describes a loaded issue set for the web UI's pickers.
type DatasetSummary struct {
	Issues          int
	Teams           []string         // DistinctTeamKeys
	Projects        []ProjectSummary // sorted by name; "" project omitted
	Assignees       []string         // sorted, non-empty
	EarliestCreated time.Time        // zero if none
	LatestUpdated   time.Time        // zero if none
	Readiness       []CheckResult    // CheckReadiness
}

// ProjectSummary is one project and its milestone names (sorted, "" omitted).
type ProjectSummary struct {
	Name       string
	Milestones []string
}

// Summarize computes a DatasetSummary. Pure.
func Summarize(items []issues.Issue) DatasetSummary {
	s := DatasetSummary{
		Issues:    len(items),
		Teams:     DistinctTeamKeys(items),
		Readiness: CheckReadiness(items),
	}

	milestones := make(map[string]map[string]bool)
	for _, it := range items {
		if it.ProjectName != "" {
			if milestones[it.ProjectName] == nil {
				milestones[it.ProjectName] = make(map[string]bool)
			}
			if it.MilestoneName != "" {
				milestones[it.ProjectName][it.MilestoneName] = true
			}
		}
		if !it.CreatedAt.IsZero() && (s.EarliestCreated.IsZero() || it.CreatedAt.Before(s.EarliestCreated)) {
			s.EarliestCreated = it.CreatedAt
		}
		if it.UpdatedAt.After(s.LatestUpdated) {
			s.LatestUpdated = it.UpdatedAt
		}
	}

	for name, ms := range milestones {
		p := ProjectSummary{Name: name}
		for m := range ms {
			p.Milestones = append(p.Milestones, m)
		}
		sort.Strings(p.Milestones)
		s.Projects = append(s.Projects, p)
	}
	sort.Slice(s.Projects, func(i, j int) bool { return s.Projects[i].Name < s.Projects[j].Name })

	for a := range Assignees(items) {
		s.Assignees = append(s.Assignees, a)
	}
	sort.Strings(s.Assignees)
	return s
}
