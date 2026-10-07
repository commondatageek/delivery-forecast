package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

// issuesToBacktestItems converts issues.Issue records to simulate.BacktestItem.
func issuesToBacktestItems(items []issues.Issue) []simulate.BacktestItem {
	out := make([]simulate.BacktestItem, len(items))
	for i, it := range items {
		out[i] = simulate.BacktestItem{
			CreatedAt:   it.CreatedAt,
			StartedAt:   it.StartedAt,
			CompletedAt: it.CompletedAt,
		}
	}
	return out
}

// backtestIssueSet selects the fixed set of issues sim backtest replays
// against: exact project match (required), exact milestone match (optional,
// only within that project), excluding canceled issues — Issue.IsCanceled
// also covers "duplicate" (D7), matching the old ProjectMilestoneIssues
// query's `state_type NOT IN ('canceled', 'duplicate')`. No team filtering,
// same as before.
func backtestIssueSet(all []issues.Issue, project, milestone string) []issues.Issue {
	scoped := issues.Filter{Project: project, Milestone: milestone}.Apply(all)
	var out []issues.Issue
	for _, it := range scoped {
		if it.IsCanceled() {
			continue
		}
		out = append(out, it)
	}
	return out
}

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
	if *project == "" {
		return fmt.Errorf("-project is required")
	}
	if *targetEndStr == "" {
		return fmt.Errorf("-target-end-date is required")
	}
	if *format != "text" && *format != "csv" {
		return fmt.Errorf(`-format must be "text" or "csv"`)
	}

	now := time.Now()
	targetDate, err := util.ParseFlexibleDate(*targetEndStr, now)
	if err != nil {
		return fmt.Errorf("invalid -target-end-date: %w", err)
	}

	sampleStartDate, err := util.ParseFlexibleStartDate(*sf.SampleStart, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-start: %w", err)
	}
	sampleEndDate, err := util.ParseFlexibleDate(*sf.SampleEnd, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-end: %w", err)
	}

	mode, err := simulate.ResolveMode(isFlagSet(cmd, "engineers"), *sf.WholeTeam)
	if err != nil {
		return err
	}

	// Load the issue set once: it feeds both the sample pool and the fixed
	// backtested set below.
	all, err := loadIssues(context.Background(), inputPath, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	// Build the fixed sample pool once; reused for every backtested day.
	pd, err := loadPool(all, *sf.ExclusionsFile, sf.TypicalEngineers, sampleStartDate, sampleEndDate, *sf.WholeTeam)
	if err != nil {
		return err
	}
	warnExclusionMismatches(pd.Exclusions, pd, sf)
	if err := simulate.ValidatePool(pd.Pool, mode, false); err != nil {
		return err
	}
	seed := resolveSeed(cmd, *sf.RandomSeed, now)

	scoped := backtestIssueSet(all, *project, *milestone)
	if len(scoped) == 0 {
		if *milestone != "" {
			return fmt.Errorf("no issues found for project %q milestone %q — check spelling", *project, *milestone)
		}
		return fmt.Errorf("no issues found for project %q — check spelling", *project)
	}

	btItems := issuesToBacktestItems(scoped)

	// Resolve start date: explicit flag wins; otherwise infer from the earliest
	// started_at across the issue set.
	var startDate time.Time
	if *replayStartStr != "" {
		startDate, err = util.ParseFlexibleStartDate(*replayStartStr, now)
		if err != nil {
			return fmt.Errorf("invalid -replay-start-date: %w", err)
		}
	} else {
		startDate = simulate.EarliestStartedAt(btItems)
		if startDate.IsZero() {
			return fmt.Errorf("no started_at found in issue set; provide -replay-start-date explicitly")
		}
		startDate = util.LocalDay(startDate)
	}

	if !targetDate.After(startDate) {
		return fmt.Errorf("-target-end-date must be after -replay-start-date (inferred: %s)", startDate.Format("2006-01-02"))
	}

	rows := simulate.RunBacktest(pd.Pool, btItems, startDate, targetDate, simulate.Params{
		Mode:          mode,
		Engineers:     sf.Engineers.Count(),
		EngineerNames: sf.Engineers.Names(),
		Simulations:   *sf.Simulations,
		Workers:       *sf.Goroutines,
		Seed:          seed,
	})

	today := util.LocalDay(now)

	switch *format {
	case "csv":
		return printBacktestCSV(rows, today)
	default:
		label := simulate.ModeLabel(mode, sf.Engineers.Count(), sf.Engineers.Names())
		scope := *project
		if *milestone != "" {
			scope += " / " + *milestone
		}
		printBacktestText(rows, scope, label, len(scoped), startDate, sampleStartDate, sampleEndDate, today)
	}
	return nil
}

func printBacktestText(rows []simulate.BacktestRow, scope, label string, total int, startDate, sampleStart, sampleEnd, today time.Time) {
	fmt.Printf("Backtest: %s (%d issues, %s)\n", scope, total, label)
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
		if r.Date.After(today) {
			dividerIdx = i + 1 // +1 to account for the header line
			break
		}
	}
	if dividerIdx >= 0 {
		lines = append(lines[:dividerIdx], append([]string{dividerLine}, lines[dividerIdx:]...)...)
	}
	fmt.Println(strings.Join(lines, "\n"))
}

func printBacktestCSV(rows []simulate.BacktestRow, today time.Time) error {
	w := csv.NewWriter(os.Stdout)
	if err := w.Write([]string{"date", "completed", "remaining", "probability", "projected"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			r.Date.Format("2006-01-02"),
			strconv.Itoa(r.Completed),
			strconv.Itoa(r.Remaining),
			strconv.FormatFloat(r.Prob, 'f', 2, 64),
			strconv.FormatBool(r.Date.After(today)),
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
