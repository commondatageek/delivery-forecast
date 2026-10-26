package app

import (
	"github.com/commondatageek/delivery-forecast/counts"
	"github.com/commondatageek/delivery-forecast/issues"
)

// CountRequest asks for the outstanding-work report.
type CountRequest struct {
	Issues         []issues.Issue
	counts.Options // Teams, Since
}

// CountResult is the outcome of RunCount.
type CountResult struct {
	Warnings  []string
	Projects  []counts.Project
	Total     int
	ShowTeams bool // no -teams given and the loaded set has > 1 team
}

// Validate exists so every request has one; count has nothing to check.
func (r CountRequest) Validate() error { return nil }

// RunCount folds the non-terminal issues into the per-project report.
func RunCount(req CountRequest) (*CountResult, error) {
	res := &CountResult{}
	if err := req.Validate(); err != nil {
		return res, err
	}
	opts := req.Options

	allTeams := DistinctTeamKeys(req.Issues)
	multiTeam := len(allTeams) > 1

	if msg := BlendingTeamsWarning(opts.Teams, allTeams); msg != "" {
		res.Warnings = append(res.Warnings, msg)
	}

	filtered := issues.Filter{Teams: opts.Teams}.Apply(req.Issues)

	pmCounts, activity := counts.Aggregate(toCountsIssues(filtered))
	if len(pmCounts) == 0 {
		res.Warnings = append(res.Warnings, "no outstanding (non-terminal) issues found for the given filters")
	}

	res.Projects, res.Total = counts.Compute(pmCounts, activity, opts.Since)
	res.ShowTeams = len(opts.Teams) == 0 && multiTeam
	return res, nil
}
