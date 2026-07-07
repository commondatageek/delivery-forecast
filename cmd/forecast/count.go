package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"forecasting/internal/counts"
	"forecasting/internal/linear"
	"forecasting/internal/sqlite"
	"forecasting/internal/util"
)

func cmdCount(args []string) error {
	defaultSince := time.Now().AddDate(0, -3, 0).Format("2006-01-02")

	cmd := flag.NewFlagSet("count", flag.ExitOnError)
	dbFile := cmd.String("db", "", "path to SQLite database")
	milestones := cmd.Bool("milestones", false, "add a per-milestone breakdown under each project")
	updatedSince := cmd.String("updated-since", defaultSince, "only include projects with an issue updated on/after this date (YYYY-MM-DD)")
	var teams linear.KeyList
	cmd.Var(&teams, "teams", "comma-separated team keys to filter by (e.g. ENG,DESIGN); default: all teams")
	configFile := cmd.String("config", "", "path to a YAML config file supplying flag values (CLI flags override)")
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *configFile); err != nil {
		return err
	}

	if *dbFile == "" {
		return fmt.Errorf("-db is required")
	}

	since, err := time.Parse("2006-01-02", *updatedSince)
	if err != nil {
		return fmt.Errorf("invalid -updated-since %q (want YYYY-MM-DD): %w", *updatedSince, err)
	}

	projects, total, multiTeam, err := loadCountProjects(*dbFile, teams, since)
	if err != nil {
		return err
	}

	showTeams := len(teams) == 0 && multiTeam

	if *milestones {
		return counts.RenderGrouped(os.Stdout, projects, total, showTeams)
	}
	return counts.RenderSummary(os.Stdout, projects, total, showTeams)
}

// loadCountProjects reads the not-completed issue counts from the store and
// returns the folded project list. It also reports whether the database holds
// more than one team.
func loadCountProjects(dbPath string, teamKeys []string, since time.Time) ([]counts.Project, int, bool, error) {
	store, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, 0, false, fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	ctx := context.Background()

	allTeams, err := store.DistinctTeamKeys(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	multiTeam := len(allTeams) > 1

	countRows, err := store.NotCompletedCounts(ctx, teamKeys)
	if err != nil {
		return nil, 0, false, err
	}

	activity, err := store.ProjectLastUpdated(ctx, teamKeys)
	if err != nil {
		return nil, 0, false, err
	}

	projects, total := counts.Compute(countRows, activity, since)
	return projects, total, multiTeam, nil
}
