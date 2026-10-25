package main

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
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
		return printBacktestCSV(res)
	default:
		printBacktestText(res, sampleStartDate, sampleEndDate, req.Today)
	}
	return nil
}

func printBacktestText(res *app.BacktestResult, sampleStart, sampleEnd, today time.Time) {
	rows := res.Rows
	startDate := res.ReplayStart
	fmt.Printf("Backtest: %s (%d issues, %s)\n", res.Scope, res.IssueCount, res.Run.ModeLabel)
	fmt.Printf("Start date: %s  Sample window: %s – %s\n\n",
		startDate.Format("2006-01-02"), sampleStart.Format("2006-01-02"), sampleEnd.Format("2006-01-02"))

	// Format the header and every row through tabwriter in one pass so column
	// widths stay consistent; the projected/today divider is spliced into the
	// already-aligned output afterward, since a line with no tab-separated
	// cells would otherwise make tabwriter start a new column block.
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DATE\tCOMPLETED\tREMAINING\tPROB")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%.1f%%\n",
			r.Date.Format("2006-01-02"), r.Completed, r.Remaining, r.Prob)
	}
	tw.Flush()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	dividerLine := fmt.Sprintf("---- today: %s — rows below are projected (assuming no further completions) ----",
		today.Format("2006-01-02"))
	dividerIdx := -1
	for i, r := range rows {
		if r.Projected {
			dividerIdx = i + 1 // +1 to account for the header line
			break
		}
	}
	if dividerIdx >= 0 {
		lines = append(lines[:dividerIdx], append([]string{dividerLine}, lines[dividerIdx:]...)...)
	}
	fmt.Println(strings.Join(lines, "\n"))
}

func printBacktestCSV(res *app.BacktestResult) error {
	w := csv.NewWriter(os.Stdout)
	if err := w.Write([]string{"date", "completed", "remaining", "probability", "projected"}); err != nil {
		return err
	}
	for _, r := range res.Rows {
		if err := w.Write([]string{
			r.Date.Format("2006-01-02"),
			strconv.Itoa(r.Completed),
			strconv.Itoa(r.Remaining),
			strconv.FormatFloat(r.Prob, 'f', 2, 64),
			strconv.FormatBool(r.Projected),
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
