package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/commondatageek/delivery-forecast/aging"
	"github.com/commondatageek/delivery-forecast/internal/logx"
	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/issues"
)

// toAgingIssues converts issues.Issue records to aging.Issue.
func toAgingIssues(items []issues.Issue) []aging.Issue {
	out := make([]aging.Issue, len(items))
	for i, it := range items {
		out[i] = aging.Issue{
			Identifier:  it.Identifier,
			Title:       it.Title,
			Assignee:    it.Assignee,
			ProjectName: it.ProjectName,
			StateType:   it.StateType,
			StateName:   it.StateName,
			StartedAt:   it.StartedAt,
			CompletedAt: it.CompletedAt,
		}
	}
	return out
}

// completedBetween selects items completed in [start, end) (start inclusive,
// end exclusive), using Issue.IsCompleted rather than a raw state_type
// check so file sources that omit state_type still work (D7).
//
// Unlike sim's sample pool, this deliberately does not require a non-empty
// assignee: aging never filters or groups by assignee, so an unassigned
// completed issue still belongs in the cycle-time distribution. The old
// SQL-backed CompletedBetween required one unconditionally (a query it
// shares with sim, where the pool genuinely is per-engineer and an
// unassigned issue has nowhere to go), which silently dropped unassigned
// completed issues from aging's distribution — see DATA_REQUIREMENTS.md
// footnote 1. This fixes that for every source, not just file ones.
func completedBetween(items []issues.Issue, start, end time.Time) []issues.Issue {
	var out []issues.Issue
	for _, it := range items {
		if !it.IsCompleted() || it.CompletedAt.IsZero() {
			continue
		}
		if it.CompletedAt.Before(start) || !it.CompletedAt.Before(end) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// inProgress selects items currently in progress, using Issue.IsInProgress
// rather than a raw state_type check (D7). aging.InProgressItems already
// skips issues with a zero StartedAt, matching the old query's
// `started_at IS NOT NULL`.
func inProgress(items []issues.Issue) []issues.Issue {
	var out []issues.Issue
	for _, it := range items {
		if it.IsInProgress() {
			out = append(out, it)
		}
	}
	return out
}

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

	if *percentile < 1 || *percentile > 100 {
		return fmt.Errorf("-percentile must be between 1 and 100, got %d", *percentile)
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

	now := time.Now()
	today := util.LocalDay(now)

	sampleEnd, err := util.ParseFlexibleDate(*sampleEndStr, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-end %q: %w", *sampleEndStr, err)
	}

	sampleStart, err := util.ParseFlexibleStartDate(*sampleStartStr, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-start %q: %w", *sampleStartStr, err)
	}

	if !sampleStart.Before(sampleEnd) {
		return fmt.Errorf("-sample-start must be before -sample-end")
	}

	opts := aging.Options{Teams: *teams, SampleStart: sampleStart, SampleEnd: sampleEnd, MinCycleTime: minCycleTime, Percentile: *percentile}

	raw, err := loadIssues(context.Background(), inputPath, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	if msg := blendingTeamsWarning(opts.Teams, distinctTeamKeys(raw)); msg != "" {
		logx.Warnf("%s", msg)
	}

	filtered := issues.Filter{Teams: opts.Teams}.Apply(raw)

	agingCompleted := toAgingIssues(completedBetween(filtered, opts.SampleStart, opts.SampleEnd))

	cycleTimes := aging.CycleTimes(agingCompleted, opts.MinCycleTime)
	sort.Float64s(cycleTimes)

	threshold := util.PercentileValue(cycleTimes, float64(opts.Percentile))

	meta := aging.Meta{
		Percentile:     opts.Percentile,
		Threshold:      threshold,
		SampleStart:    opts.SampleStart,
		SampleEnd:      opts.SampleEnd,
		CompletedCount: len(cycleTimes),
	}

	inProgressItems := aging.InProgressItems(toAgingIssues(inProgress(filtered)), today)
	aging.RankItems(inProgressItems, cycleTimes, threshold)

	sort.Slice(inProgressItems, func(i, j int) bool {
		return inProgressItems[i].AgeDays > inProgressItems[j].AgeDays
	})

	var completedItems []aging.Item
	if *showCompleted {
		completedItems = aging.CompletedItems(agingCompleted, opts.MinCycleTime)
		aging.RankItems(completedItems, cycleTimes, threshold)

		sort.Slice(completedItems, func(i, j int) bool {
			return completedItems[i].AgeDays > completedItems[j].AgeDays
		})
	}

	if len(cycleTimes) == 0 {
		logx.Warnf("no completed issues found in the sample window; percentiles will be 0 and multipliers blank")
	}

	switch *format {
	case "text":
		color := isatty.IsTerminal(os.Stdout.Fd()) && os.Getenv("NO_COLOR") == ""
		return aging.RenderText(os.Stdout, inProgressItems, completedItems, *showCompleted, meta, color)
	case "json":
		return aging.RenderJSON(os.Stdout, inProgressItems, meta)
	case "html":
		return aging.RenderHTML(os.Stdout, inProgressItems, completedItems, *showCompleted, meta)
	default:
		return fmt.Errorf("unknown -format %q (use text, json, or html)", *format)
	}
}
