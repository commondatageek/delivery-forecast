package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/commondatageek/delivery-forecast/counts"
	"github.com/commondatageek/delivery-forecast/internal/logx"
	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/issues"
)

// toCountsIssues converts issues.Issue records to counts.Issue.
func toCountsIssues(items []issues.Issue) []counts.Issue {
	out := make([]counts.Issue, len(items))
	for i, it := range items {
		out[i] = counts.Issue{
			TeamKey:              it.TeamKey,
			TeamName:             it.TeamName,
			ProjectName:          it.ProjectName,
			ProjectMilestoneName: it.ProjectMilestoneName,
			StateType:            it.StateType,
			UpdatedAt:            it.UpdatedAt,
		}
	}
	return out
}

func cmdCount(args []string) error {
	defaultSince := time.Now().AddDate(0, -3, 0).Format("2006-01-02")

	cmd := flag.NewFlagSet("count", flag.ExitOnError)
	dbFile := addDBFlag(cmd)
	inputFile := addInputFlag(cmd)
	stdinFormat := addStdinFormatFlag(cmd)
	milestones := cmd.Bool("milestones", false, "add a per-milestone breakdown under each project")
	updatedSince := cmd.String("updated-since", defaultSince, `only include projects with an issue updated on/after this date (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months")`)
	teams := addTeamsFlag(cmd, "comma-separated team keys to filter by (e.g. ENG,DESIGN); default: all teams")
	configFile := addConfigFlag(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *configFile); err != nil {
		return err
	}

	inputPath, err := resolveInput(cmd, inputFile, dbFile)
	if err != nil {
		return err
	}

	since, err := util.ParseFlexibleDate(*updatedSince, time.Now())
	if err != nil {
		return fmt.Errorf("invalid -updated-since %q: %w", *updatedSince, err)
	}

	opts := counts.Options{Teams: *teams, Since: since}

	projects, total, multiTeam, err := loadCountProjects(inputPath, *stdinFormat, opts)
	if err != nil {
		return err
	}

	showTeams := len(opts.Teams) == 0 && multiTeam

	if *milestones {
		return counts.RenderGrouped(os.Stdout, projects, total, showTeams)
	}
	return counts.RenderSummary(os.Stdout, projects, total, showTeams)
}

// loadCountProjects loads issues from path (format naming the stdin format
// when path is "-") and returns the folded project list. It also reports
// whether the loaded issue set holds more than one team.
func loadCountProjects(path, format string, opts counts.Options) ([]counts.Project, int, bool, error) {
	raw, err := loadIssues(context.Background(), path, format)
	if err != nil {
		return nil, 0, false, fmt.Errorf("load issues: %w", err)
	}

	allTeams := distinctTeamKeys(raw)
	multiTeam := len(allTeams) > 1

	if msg := blendingTeamsWarning(opts.Teams, allTeams); msg != "" {
		logx.Warnf("%s", msg)
	}

	filtered := issues.Filter{Teams: opts.Teams}.Apply(raw)

	pmCounts, activity := counts.Aggregate(toCountsIssues(filtered))
	if len(pmCounts) == 0 {
		logx.Warnf("no outstanding (non-terminal) issues found for the given filters")
	}

	projects, total := counts.Compute(pmCounts, activity, opts.Since)
	return projects, total, multiTeam, nil
}
