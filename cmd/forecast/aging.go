package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/commondatageek/delivery-forecast/aging"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdAging(args []string) error {
	cmd := flag.NewFlagSet("aging", flag.ExitOnError)
	dbFile := addDBFlag(cmd)
	inputFile := addInputFlag(cmd)
	stdinFormat := addStdinFormatFlag(cmd)
	sampleStartStr := cmd.String("sample-start", "-3 months", `start of completed-issue window (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months")`)
	sampleEndStr := cmd.String("sample-end", "today", `end of completed-issue window (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months")`)
	format := cmd.String("format", "text", "output format: text, json, html")
	minCycleTimeStr := cmd.String("min-cycle-time", "", "exclude completed issues with cycle time below this duration (e.g. 5m, 1h, 1d)")
	percentile := cmd.Int("percentile", 85, "percentile of the cycle-time distribution to anchor the report to (1-100)")
	showCompleted := cmd.Bool("show-completed", false, "text/html: also list the completed issues that make up the percentile distribution sample")
	teams := addTeamsFlag(cmd, "comma-separated team keys to filter by (e.g. DATA,PLT); default: all teams")
	configFile := addConfigFlag(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *configFile); err != nil {
		return err
	}

	inputPath, err := resolveInput(cmd, inputFile, dbFile)
	if err != nil {
		return err
	}

	var minCycleTime time.Duration
	if *minCycleTimeStr != "" {
		d, err := util.ParseFlexibleDuration(*minCycleTimeStr)
		if err != nil {
			return fmt.Errorf("invalid -min-cycle-time %q: %w", *minCycleTimeStr, err)
		}
		minCycleTime = d
	}

	now := nowFunc()
	today := util.LocalDay(now)

	sampleEnd, err := util.ParseFlexibleDate(*sampleEndStr, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-end %q: %w", *sampleEndStr, err)
	}

	sampleStart, err := util.ParseFlexibleStartDate(*sampleStartStr, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-start %q: %w", *sampleStartStr, err)
	}

	req := app.AgingRequest{
		Options:       aging.Options{Teams: *teams, SampleStart: sampleStart, SampleEnd: sampleEnd, MinCycleTime: minCycleTime, Percentile: *percentile},
		ShowCompleted: *showCompleted,
		Today:         today,
	}
	if err := req.Validate(); err != nil {
		return err
	}

	req.Issues, err = loadIssues(context.Background(), inputPath, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	res, err := app.RunAging(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}
	meta, inProgressItems, completedItems := res.Meta, res.InProgress, res.Completed

	switch *format {
	case "text":
		return aging.RenderText(os.Stdout, inProgressItems, completedItems, *showCompleted, meta)
	case "json":
		return aging.RenderJSON(os.Stdout, inProgressItems, meta)
	case "html":
		return aging.RenderHTML(os.Stdout, inProgressItems, completedItems, *showCompleted, meta)
	default:
		return fmt.Errorf("unknown -format %q (use text, json, or html)", *format)
	}
}
