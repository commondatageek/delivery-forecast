package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/cfd"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdCFD(args []string) error {
	cmd := flag.NewFlagSet("cfd", flag.ExitOnError)
	dbFile := addDBFlag(cmd)
	inputFile := addInputFlag(cmd)
	stdinFormat := addStdinFormatFlag(cmd)
	startStr := cmd.String("start", "-3 months", `start date, inclusive (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months")`)
	endStr := cmd.String("end", "today", `end date, inclusive (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months")`)
	format := cmd.String("format", "html", "output format: html, json")
	outPath := cmd.String("out", "", "write output to this file instead of stdout")
	teams := addTeamsFlag(cmd, "comma-separated team keys to filter by (e.g. ENG,DATA); default: all teams")
	configFile := addConfigFlag(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *configFile); err != nil {
		return err
	}

	inputPath, err := resolveInput(cmd, inputFile, dbFile)
	if err != nil {
		return err
	}

	now := nowFunc()

	windowEnd, err := util.ParseFlexibleDate(*endStr, now)
	if err != nil {
		return fmt.Errorf("invalid -end %q: %w", *endStr, err)
	}

	windowStart, err := util.ParseFlexibleStartDate(*startStr, now)
	if err != nil {
		return fmt.Errorf("invalid -start %q: %w", *startStr, err)
	}

	req := app.CFDRequest{Options: cfd.Options{Teams: *teams, Start: windowStart, End: windowEnd}}
	if err := req.Validate(); err != nil {
		return err
	}

	req.Issues, err = loadIssues(context.Background(), inputPath, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	res, err := app.RunCFD(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}
	rows, health := res.Rows, res.Health
	opts := req.Options

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
	case "html":
		return cfd.RenderHTML(out, rows, health, health.TotalIssues, health.SkippedIssues, opts.Start, opts.End)
	case "json":
		return cfd.RenderJSON(out, rows, health)
	default:
		return fmt.Errorf("unknown -format %q (use html or json)", *format)
	}
}
