package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/report"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdSimBacktest(args []string) error {
	cmd := flag.NewFlagSet("sim backtest", flag.ExitOnError)
	dbFile := addDBFlag(cmd)
	inputFile := addInputFlag(cmd)
	stdinFormat := addStdinFormatFlag(cmd)
	sf := addSimFlags(cmd)
	cmd.Lookup("simulations").Usage = "number of Monte Carlo simulations to run per backtested day"
	project := cmd.String("project", "", "project name to backtest (required)")
	milestone := cmd.String("milestone", "", "milestone name within the project (optional)")
	replayStartStr := cmd.String("replay-start-date", "", `first day to replay from, inclusive (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: earliest started_at across the issue set`)
	targetEndStr := cmd.String("target-end-date", "", `completion deadline to forecast against (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months"; required)`)
	format := cmd.String("format", "text", `output format: "text" or "csv"`)
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
	var targetDate time.Time
	if *targetEndStr != "" {
		targetDate, err = util.ParseFlexibleDate(*targetEndStr, now)
		if err != nil {
			return fmt.Errorf("invalid -target-end-date: %w", err)
		}
	}

	sampleStartDate, err := util.ParseFlexibleStartDate(*sf.SampleStart, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-start: %w", err)
	}
	sampleEndDate, err := util.ParseFlexibleDate(*sf.SampleEnd, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-end: %w", err)
	}

	var replayStart time.Time
	if *replayStartStr != "" {
		replayStart, err = util.ParseFlexibleStartDate(*replayStartStr, now)
		if err != nil {
			return fmt.Errorf("invalid -replay-start-date: %w", err)
		}
	}

	req := app.BacktestRequest{
		SimBase:     newSimBase(cmd, sf, sampleStartDate, sampleEndDate, now),
		Project:     *project,
		Milestone:   *milestone,
		ReplayStart: replayStart,
		TargetEnd:   targetDate,
		Today:       util.LocalDay(now),
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if *format != "text" && *format != "csv" {
		return fmt.Errorf(`-format must be "text" or "csv"`)
	}

	// Load the issue set once: it feeds both the sample pool and the fixed
	// backtested set.
	if err := loadSimInputs(&req.SimBase, inputPath, *stdinFormat, *sf.ExclusionsFile); err != nil {
		return err
	}

	res, err := app.SimBacktest(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	switch *format {
	case "csv":
		return report.BacktestCSV(os.Stdout, res)
	default:
		report.BacktestText(os.Stdout, res, sampleStartDate, sampleEndDate, req.Today)
	}
	return nil
}
