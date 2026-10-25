package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/commondatageek/delivery-forecast/history"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdHistory(args []string) error {
	cmd := flag.NewFlagSet("history", flag.ExitOnError)
	input := addInputFlag(cmd)
	stdinFormat := addStdinFormatFlag(cmd)
	project := cmd.String("project", "", "exact project name to scope to; default: all projects")
	milestone := cmd.String("milestone", "", "exact milestone name within -project; default: all milestones")
	teams := addTeamsFlag(cmd, "comma-separated team keys to filter by (e.g. ENG,DATA); default: all teams")
	startStr := cmd.String("start", "", `first day emitted, inclusive (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: earliest created_at in scope`)
	endStr := cmd.String("end", "today", `last day emitted, inclusive (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months")`)
	window := cmd.Int("window", 28, "trailing window in days for rolling metrics")
	format := cmd.String("format", "csv", `output format: "csv", "json", or "text"`)
	outPath := cmd.String("out", "", "write output to this file instead of stdout")
	configFile := addConfigFlag(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *configFile); err != nil {
		return err
	}

	if *format != "csv" && *format != "json" && *format != "text" {
		return fmt.Errorf(`-format must be "csv", "json", or "text"`)
	}
	if *input == "" {
		return fmt.Errorf("-input is required")
	}

	now := nowFunc()
	endDate, err := util.ParseFlexibleDate(*endStr, now)
	if err != nil {
		return fmt.Errorf("invalid -end %q: %w", *endStr, err)
	}

	var startDate time.Time
	if *startStr != "" {
		startDate, err = util.ParseFlexibleStartDate(*startStr, now)
		if err != nil {
			return fmt.Errorf("invalid -start %q: %w", *startStr, err)
		}
	}

	req := app.HistoryRequest{
		Teams:      *teams,
		Project:    *project,
		Milestone:  *milestone,
		Start:      startDate,
		End:        endDate,
		WindowDays: *window,
	}
	if err := req.Validate(); err != nil {
		return err
	}

	req.Issues, err = loadIssues(context.Background(), *input, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	hres, err := app.RunHistory(req)
	printWarnings(hres.Warnings)
	if err != nil {
		return err
	}
	res := hres.Result

	out := os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		out = f
	}

	switch *format {
	case "json":
		return history.RenderJSON(out, res)
	case "text":
		return history.RenderText(out, res)
	default:
		return history.RenderCSV(out, res)
	}
}
