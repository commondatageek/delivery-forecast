package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"forecasting/internal/aging"
	"forecasting/internal/linear"
	"forecasting/internal/sqlite"
	"forecasting/internal/util"
)

func main() {
	dbFile := flag.String("db", "", "path to SQLite database")
	sampleStartStr := flag.String("sample-start", "", "Start of completed-issue window (YYYY-MM-DD, default: today minus 3 months)")
	sampleEndStr := flag.String("sample-end", "", "End of completed-issue window (YYYY-MM-DD, default: today)")
	format := flag.String("format", "text", "Output format: text, json, html")
	minCycleTimeStr := flag.String("min-cycle-time", "", "Exclude completed issues with cycle time below this duration from the percentile distribution (e.g. 5m, 1h, 1d)")
	var teams linear.KeyList
	flag.Var(&teams, "teams", "Comma-separated team keys to filter by (e.g. DATA,PLT); default: all teams")
	configFile := flag.String("config", "", "path to a YAML config file supplying flag values (CLI flags override)")
	flag.Parse()

	if err := util.ApplyConfig(flag.CommandLine, *configFile); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *dbFile == "" {
		fmt.Fprintln(os.Stderr, "error: -db is required")
		os.Exit(1)
	}

	var minCycleTime time.Duration
	if *minCycleTimeStr != "" {
		d, err := util.ParseFlexibleDuration(*minCycleTimeStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid -min-cycle-time %q: %v\n", *minCycleTimeStr, err)
			os.Exit(1)
		}
		minCycleTime = d
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)

	sampleEnd := today
	if *sampleEndStr != "" {
		t, err := util.ParseDate(*sampleEndStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid -sample-end %q: %v\n", *sampleEndStr, err)
			os.Exit(1)
		}
		sampleEnd = t
	}

	sampleStart := today.AddDate(0, -3, 0)
	if *sampleStartStr != "" {
		t, err := util.ParseDate(*sampleStartStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid -sample-start %q: %v\n", *sampleStartStr, err)
			os.Exit(1)
		}
		sampleStart = t
	}

	if !sampleStart.Before(sampleEnd) {
		fmt.Fprintln(os.Stderr, "error: -sample-start must be before -sample-end")
		os.Exit(1)
	}

	store, err := sqlite.Open(*dbFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx := context.Background()

	completed, err := store.CompletedBetween(ctx, sampleStart, sampleEnd, nil, teams)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: query completed: %v\n", err)
		os.Exit(1)
	}

	active, err := store.InProgress(ctx, teams)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: query in-progress: %v\n", err)
		os.Exit(1)
	}

	cycleTimes := aging.CycleTimes(completed, minCycleTime)
	sort.Float64s(cycleTimes)

	inProgress := aging.InProgressItems(active, today)
	aging.RankItems(inProgress, cycleTimes)

	sort.Slice(inProgress, func(i, j int) bool {
		return inProgress[i].AgeDays > inProgress[j].AgeDays
	})

	p85 := util.PercentileValue(cycleTimes, 85)

	if len(cycleTimes) == 0 {
		fmt.Fprintln(os.Stderr, "warning: no completed issues found in the sample window; percentiles will be 0")
	}

	switch *format {
	case "text":
		if err := aging.RenderText(os.Stdout, inProgress, cycleTimes, p85, sampleStart, sampleEnd); err != nil {
			fmt.Fprintf(os.Stderr, "error: render text: %v\n", err)
			os.Exit(1)
		}
	case "json":
		if err := aging.RenderJSON(os.Stdout, inProgress); err != nil {
			fmt.Fprintf(os.Stderr, "error: encode JSON: %v\n", err)
			os.Exit(1)
		}
	case "html":
		if err := aging.RenderHTML(os.Stdout, inProgress, p85, sampleStart, sampleEnd, len(cycleTimes)); err != nil {
			fmt.Fprintf(os.Stderr, "error: render HTML: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "error: unknown -format %q (use text, json, or html)\n", *format)
		os.Exit(1)
	}
}
