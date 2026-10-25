package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/counts"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdCount(args []string) error {
	defaultSince := nowFunc().AddDate(0, -3, 0).Format("2006-01-02")

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

	since, err := util.ParseFlexibleDate(*updatedSince, nowFunc())
	if err != nil {
		return fmt.Errorf("invalid -updated-since %q: %w", *updatedSince, err)
	}

	req := app.CountRequest{Options: counts.Options{Teams: *teams, Since: since}}
	if err := req.Validate(); err != nil {
		return err
	}

	req.Issues, err = loadIssues(context.Background(), inputPath, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	res, err := app.RunCount(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if *milestones {
		return counts.RenderGrouped(os.Stdout, res.Projects, res.Total, res.ShowTeams)
	}
	return counts.RenderSummary(os.Stdout, res.Projects, res.Total, res.ShowTeams)
}
