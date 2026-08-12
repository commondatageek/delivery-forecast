package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/commondatageek/delivery-forecast/cfd"
	"github.com/commondatageek/delivery-forecast/internal/logx"
	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/issues"
)

// toCFDIssues converts issues.Issue records to cfd.Issue.
func toCFDIssues(items []issues.Issue) []cfd.Issue {
	out := make([]cfd.Issue, len(items))
	for i, it := range items {
		out[i] = cfd.Issue{
			CreatedAt:   it.CreatedAt,
			StartedAt:   it.StartedAt,
			CompletedAt: it.CompletedAt,
			CanceledAt:  it.CanceledAt,
			StateType:   it.StateType,
		}
	}
	return out
}

func cmdCFD(args []string) error {
	cmd := flag.NewFlagSet("cfd", flag.ExitOnError)
	dbFile := addDBFlag(cmd)
	inputFile := addInputFlag(cmd)
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

	now := time.Now()

	windowEnd, err := util.ParseFlexibleDate(*endStr, now)
	if err != nil {
		return fmt.Errorf("invalid -end %q: %w", *endStr, err)
	}

	windowStart, err := util.ParseFlexibleStartDate(*startStr, now)
	if err != nil {
		return fmt.Errorf("invalid -start %q: %w", *startStr, err)
	}

	if !windowStart.Before(windowEnd) {
		return fmt.Errorf("-start must be before -end")
	}

	opts := cfd.Options{Teams: *teams, Start: windowStart, End: windowEnd}

	raw, err := loadIssues(context.Background(), inputPath)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	if msg := blendingTeamsWarning(opts.Teams, distinctTeamKeys(raw)); msg != "" {
		logx.Warnf("%s", msg)
	}

	filtered := issues.Filter{Teams: opts.Teams}.Apply(raw)
	if len(filtered) == 0 {
		logx.Warnf("no issues found for the given team filter")
	}

	var normalized []cfd.NormalizedIssue
	skipped := 0
	for _, r := range toCFDIssues(filtered) {
		ni, ok := cfd.Normalize(r)
		if !ok {
			skipped++
			continue
		}
		normalized = append(normalized, ni)
	}

	rows := cfd.BuildGrid(normalized, opts.Start, opts.End)

	if err := cfd.AssertInvariants(rows); err != nil {
		return fmt.Errorf("CFD invariant violated: %w", err)
	}

	health := cfd.ComputeHealth(rows, normalized, opts.Start, opts.End)
	health.TotalIssues = len(filtered)
	health.SkippedIssues = skipped

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
		return cfd.RenderHTML(out, rows, health, len(filtered), skipped, opts.Start, opts.End)
	case "json":
		return cfd.RenderJSON(out, rows, health)
	default:
		return fmt.Errorf("unknown -format %q (use html or json)", *format)
	}
}
