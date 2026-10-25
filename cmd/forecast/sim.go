package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdSimItems(args []string) error {
	cmd := flag.NewFlagSet("sim items", flag.ExitOnError)
	dbFile := addDBFlag(cmd)
	inputFile := addInputFlag(cmd)
	stdinFormat := addStdinFormatFlag(cmd)
	sf := addSimFlags(cmd)
	days := cmd.Int("days", 0, "number of days; mutually exclusive with -target-end-date, one must be given")
	targetStartStr := cmd.String("target-start-date", "tomorrow", `start of the target window (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: tomorrow`)
	targetEndStr := cmd.String("target-end-date", "", `end of the target window, inclusive (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months"); mutually exclusive with -days, one must be given`)
	var confidences intList
	cmd.Var(&confidences, "confidence", "comma-separated confidence levels to output, e.g. 85 means \"85% chance of completing at least N items\" (default: 50,75,85,95)")
	manifestFile := cmd.String("manifest", "", `write a run-provenance JSON manifest to this path ("-" for stdout)`)
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
	startDate, err := util.ParseFlexibleStartDate(*sf.SampleStart, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-start date: %w", err)
	}
	endDate, err := util.ParseFlexibleDate(*sf.SampleEnd, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-end date: %w", err)
	}

	targetStart, windowDays, targetEnd, err := parseTargetWindowFlags(cmd, *days, *targetStartStr, *targetEndStr, now)
	if err != nil {
		return err
	}

	req := app.ItemsRequest{
		SimBase:     newSimBase(cmd, sf, startDate, endDate, now),
		TargetStart: targetStart,
		Days:        windowDays,
		TargetEnd:   targetEnd,
		Confidences: []int(confidences),
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if err := loadSimInputs(&req.SimBase, inputPath, *stdinFormat, *sf.ExclusionsFile); err != nil {
		return err
	}
	bar := newProgressBar(*sf.Simulations)
	req.Progress = bar.update

	res, err := app.SimItems(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if err := writeSimManifest(*manifestFile, "sim items", cmd, sf, inputPath, res.Run, map[string]any{
		"effective_confidence_levels": res.Confidences,
		"target_start_date":           res.TargetStart.Format("2006-01-02"),
		"target_end_date":             res.TargetEnd.Format("2006-01-02"),
		"effective_days":              res.EffectiveDays,
	}); err != nil {
		return err
	}

	fmt.Printf("%s, %s -> how many items?\n\n", res.Run.ModeLabel, describeWindow(res.TargetStart, res.TargetEnd, res.EffectiveDays))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Confidence\tItems")
	for _, row := range res.Rows {
		fmt.Fprintf(w, "%d%%\tat least %d\n", row.Confidence, row.Items)
	}
	w.Flush()
	return nil
}

// printTrajectoryReport prints the grouped trajectory report for `sim days
// -items g1,g2,...`: one row per group plus a Total row, with per-confidence
// Days/Date columns. All thresholds are simulated with the same seed (see
// the trajectory table in package simulate) so the report's invariants hold.
func printTrajectoryReport(res *app.DaysResult) {
	fmt.Printf("%s, starting %s -> grouped trajectory\n\n", res.Run.ModeLabel, res.TargetStart.Format("2006-01-02"))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	confRow := []string{"", ""}
	header := []string{"Group", "Items"}
	for _, c := range res.Confidences {
		confRow = append(confRow, fmt.Sprintf("%d%%", c), "")
		header = append(header, "Days", "Date")
	}
	fmt.Fprintln(w, strings.Join(confRow, "\t"))
	fmt.Fprintln(w, strings.Join(header, "\t"))

	for g, group := range res.Groups {
		row := []string{fmt.Sprintf("Group %d", g+1), fmt.Sprintf("%d", group.Items)}
		for _, cell := range group.Cells {
			row = append(row, fmt.Sprintf("%d", cell.MarginalDays), cell.Date.Format("2006-01-02 (Mon)"))
		}
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	totalRow := []string{"Total", fmt.Sprintf("%d", res.Total.Items)}
	for _, cell := range res.Total.Cells {
		totalRow = append(totalRow, fmt.Sprintf("%d", cell.MarginalDays), cell.Date.Format("2006-01-02 (Mon)"))
	}
	fmt.Fprintln(w, strings.Join(totalRow, "\t"))

	w.Flush()
}

func cmdSimDays(args []string) error {
	cmd := flag.NewFlagSet("sim days", flag.ExitOnError)
	dbFile := addDBFlag(cmd)
	inputFile := addInputFlag(cmd)
	stdinFormat := addStdinFormatFlag(cmd)
	sf := addSimFlags(cmd)
	var items intList
	cmd.Var(&items, "items", "number of items to complete (required); comma-separated for a grouped trajectory report (e.g. 13,12,9)")
	targetStartStr := cmd.String("target-start-date", "tomorrow", `forecast start date used to compute calendar dates (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: tomorrow`)
	var confidences intList
	cmd.Var(&confidences, "confidence", "comma-separated confidence levels to output, e.g. 85 means \"85% chance of finishing within N days\" (default: 50,75,85,95)")
	manifestFile := cmd.String("manifest", "", `write a run-provenance JSON manifest to this path ("-" for stdout)`)
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
	startDate, err := util.ParseFlexibleStartDate(*sf.SampleStart, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-start date: %w", err)
	}
	endDate, err := util.ParseFlexibleDate(*sf.SampleEnd, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-end date: %w", err)
	}
	targetStartDate, err := util.ParseFlexibleStartDate(*targetStartStr, now)
	if err != nil {
		return fmt.Errorf("invalid -target-start-date: %w", err)
	}

	req := app.DaysRequest{
		SimBase:     newSimBase(cmd, sf, startDate, endDate, now),
		TargetStart: targetStartDate,
		Items:       []int(items),
		Confidences: []int(confidences),
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if err := loadSimInputs(&req.SimBase, inputPath, *stdinFormat, *sf.ExclusionsFile); err != nil {
		return err
	}
	// Only the single-value path reports progress; the grouped trajectory runs
	// several simulations back to back and leaves the bar off.
	if len(items) == 1 {
		bar := newProgressBar(*sf.Simulations)
		req.Progress = bar.update
	}

	res, err := app.SimDays(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if err := writeSimManifest(*manifestFile, "sim days", cmd, sf, inputPath, res.Run, map[string]any{
		"effective_confidence_levels": res.Confidences,
		"target_start_date":           res.TargetStart.Format("2006-01-02"),
	}); err != nil {
		return err
	}

	if len(items) > 1 {
		printTrajectoryReport(res)
		return nil
	}

	fmt.Printf("%s, %d items -> how many days?\n\n", res.Run.ModeLabel, items[0])

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Confidence\tDays\tDate")
	for _, row := range res.Single {
		fmt.Fprintf(w, "%d%%\t%d\t%s\n", row.Confidence, row.Days, row.Date.Format("2006-01-02 Mon"))
	}
	w.Flush()
	return nil
}

func cmdSimProbability(args []string) error {
	cmd := flag.NewFlagSet("sim probability", flag.ExitOnError)
	dbFile := addDBFlag(cmd)
	inputFile := addInputFlag(cmd)
	stdinFormat := addStdinFormatFlag(cmd)
	sf := addSimFlags(cmd)
	days := cmd.Int("days", 0, "number of days; mutually exclusive with -target-end-date, one must be given")
	targetStartStr := cmd.String("target-start-date", "tomorrow", `start of the target window (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: tomorrow`)
	targetEndStr := cmd.String("target-end-date", "", `end of the target window, inclusive (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months"); mutually exclusive with -days, one must be given`)
	items := cmd.Int("items", -1, "number of items to complete (omit to show full distribution)")
	manifestFile := cmd.String("manifest", "", `write a run-provenance JSON manifest to this path ("-" for stdout)`)
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
	startDate, err := util.ParseFlexibleStartDate(*sf.SampleStart, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-start date: %w", err)
	}
	endDate, err := util.ParseFlexibleDate(*sf.SampleEnd, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-end date: %w", err)
	}

	targetStart, windowDays, targetEnd, err := parseTargetWindowFlags(cmd, *days, *targetStartStr, *targetEndStr, now)
	if err != nil {
		return err
	}

	req := app.ProbabilityRequest{
		SimBase:     newSimBase(cmd, sf, startDate, endDate, now),
		TargetStart: targetStart,
		Days:        windowDays,
		TargetEnd:   targetEnd,
		Items:       *items,
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if err := loadSimInputs(&req.SimBase, inputPath, *stdinFormat, *sf.ExclusionsFile); err != nil {
		return err
	}
	bar := newProgressBar(*sf.Simulations)
	req.Progress = bar.update

	res, err := app.SimProbability(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if err := writeSimManifest(*manifestFile, "sim probability", cmd, sf, inputPath, res.Run, map[string]any{
		"target_start_date": res.TargetStart.Format("2006-01-02"),
		"target_end_date":   res.TargetEnd.Format("2006-01-02"),
		"effective_days":    res.EffectiveDays,
	}); err != nil {
		return err
	}

	windowDescription := describeWindow(res.TargetStart, res.TargetEnd, res.EffectiveDays)

	if res.Items >= 0 {
		fmt.Printf("%s, %s, %d items -> probability of completion?\n", res.Run.ModeLabel, windowDescription, res.Items)
		fmt.Printf("  %.1f%%  (i.e. you can commit to %d items here at ~%.0f%% confidence)\n", res.Probability, res.Items, res.Probability)
	} else {
		fmt.Printf("%s, %s -> probability of completing N items\n", res.Run.ModeLabel, windowDescription)
		fmt.Printf("  (each row's %% is also that many items' confidence level; see `sim items -confidence`)\n")
		for _, row := range res.Table {
			fmt.Printf("  %d items: %.1f%%\n", row.Items, row.Probability)
		}
	}
	return nil
}
