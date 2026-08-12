package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/commondatageek/delivery-forecast/history"
	"github.com/commondatageek/delivery-forecast/internal/logx"
	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/issues"
)

// toHistoryIssues converts issues.Issue records to history.Issue.
func toHistoryIssues(items []issues.Issue) []history.Issue {
	out := make([]history.Issue, len(items))
	for i, it := range items {
		out[i] = history.Issue{
			CreatedAt:   it.CreatedAt,
			StartedAt:   it.StartedAt,
			CompletedAt: it.CompletedAt,
			CanceledAt:  it.CanceledAt,
		}
	}
	return out
}

func cmdHistory(args []string) error {
	cmd := flag.NewFlagSet("history", flag.ExitOnError)
	input := addInputFlag(cmd)
	inputFormat := addInputFormatFlag(cmd)
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
	if *window <= 0 {
		return fmt.Errorf("-window must be positive")
	}
	if *input == "" {
		return fmt.Errorf("-input is required")
	}

	ctx := context.Background()

	raw, err := loadIssues(ctx, *input, *inputFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	filtered := issues.Filter{Teams: *teams, Project: *project, Milestone: *milestone}.Apply(raw)
	if len(filtered) == 0 {
		switch {
		case *milestone != "":
			return fmt.Errorf("no issues found for project %q milestone %q — check spelling", *project, *milestone)
		case *project != "":
			return fmt.Errorf("no issues found for project %q — check spelling", *project)
		default:
			return fmt.Errorf("no issues found for the given filters — check spelling")
		}
	}

	if msg := blendingTeamsWarning(*teams, distinctTeamKeys(filtered)); msg != "" {
		logx.Warnf("%s", msg)
	}

	now := time.Now()
	endDate, err := util.ParseFlexibleDate(*endStr, now)
	if err != nil {
		return fmt.Errorf("invalid -end %q: %w", *endStr, err)
	}

	historyIssues := toHistoryIssues(filtered)

	var startDate time.Time
	if *startStr != "" {
		startDate, err = util.ParseFlexibleStartDate(*startStr, now)
		if err != nil {
			return fmt.Errorf("invalid -start %q: %w", *startStr, err)
		}
	} else {
		// Differs from sim backtest, which defaults to the earliest
		// started_at: history's Total is a created-to-date burnup line, so
		// created_at is the right anchor here.
		startDate = util.LocalDay(history.EarliestCreatedAt(historyIssues))
		if startDate.IsZero() {
			return fmt.Errorf("no created_at found in issue set; provide -start explicitly")
		}
	}

	if endDate.Before(startDate) {
		return fmt.Errorf("-end must not be before -start (%s)", startDate.Format("2006-01-02"))
	}

	res, err := history.Compute(historyIssues, history.Options{Start: startDate, End: endDate, WindowDays: *window})
	if err != nil {
		return err
	}
	if err := history.AssertInvariants(res.Rows); err != nil {
		return fmt.Errorf("history invariant violated: %w", err)
	}

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
