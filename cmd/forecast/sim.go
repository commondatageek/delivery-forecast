package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/simulate"
)

func cmdSimItems(args []string) error {
	cmd := flag.NewFlagSet("sim items", flag.ExitOnError)
	dbFile := addDBFlag(cmd)
	inputFile := addInputFlag(cmd)
	stdinFormat := addStdinFormatFlag(cmd)
	sf := addSimFlags(cmd)
	days := cmd.Int("days", 30, "number of days")
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

	mode, err := simulate.ResolveMode(isFlagSet(cmd, "engineers"), *sf.WholeTeam)
	if err != nil {
		return err
	}

	now := time.Now()
	startDate, err := util.ParseFlexibleStartDate(*sf.SampleStart, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-start date: %w", err)
	}
	endDate, err := util.ParseFlexibleDate(*sf.SampleEnd, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-end date: %w", err)
	}

	if len(confidences) == 0 {
		confidences = intList{50, 75, 85, 95}
	}
	for _, c := range confidences {
		if c <= 0 || c > 100 {
			return fmt.Errorf("-confidence: values must be in (0, 100], got %d", c)
		}
	}

	all, err := loadIssues(context.Background(), inputPath, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	loaded, err := loadPool(all, *sf.ExclusionsFile, sf.TypicalEngineers, startDate, endDate, *sf.WholeTeam)
	if err != nil {
		return err
	}
	pool := loaded.Pool
	if err := simulate.ValidatePool(pool, mode, false); err != nil {
		return err
	}
	seed := resolveSeed(cmd, *sf.RandomSeed, now)

	if err := writeManifest(*manifestFile, manifestInputs{
		Subcommand: "sim items", Cmd: cmd, Mode: mode, TypicalEngineers: sf.TypicalEngineers,
		Engineers: *sf.Engineers, WholeTeam: *sf.WholeTeam, Seed: seed,
		SampleStart: startDate, SampleEnd: endDate,
		DBPath: inputPath, ExclusionsPath: *sf.ExclusionsFile,
		Exclusions: loaded.Exclusions, Pool: pool, Issues: loaded.Issues, Skipped: loaded.Skipped,
		Extra: map[string]any{"effective_confidence_levels": []int(confidences)},
	}); err != nil {
		return err
	}

	bar := newProgressBar(*sf.Simulations)
	dist := simulate.ItemsInDays(pool, simulate.Params{
		Mode:        mode,
		Engineers:   *sf.Engineers,
		Days:        *days,
		Simulations: *sf.Simulations,
		Workers:     *sf.Goroutines,
		Seed:        seed,
		Progress:    bar.update,
	})
	fmt.Printf("%s, %d days -> how many items?\n\n", simulate.ModeLabel(mode, *sf.Engineers, nil), *days)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Confidence\tItems")
	for _, c := range confidences {
		n := simulate.ItemsAtConfidence(dist, float64(c))
		fmt.Fprintf(w, "%d%%\tat least %d\n", c, n)
	}
	w.Flush()
	return nil
}

// printTrajectoryReport prints the grouped trajectory report for `sim days
// -items g1,g2,...`: one row per group plus a Total row, with per-confidence
// Days/Date columns. All thresholds are simulated with the same seed (see
// simulate.ComputeTrajectoryTable) so the report's invariants hold.
func printTrajectoryReport(pool *simulate.SamplePool, mode simulate.Mode, engineers int, seed int64, simulations, goroutines int, groups, confidences []int, targetStartDate time.Time) {
	cum := make([]int, len(groups))
	total := 0
	for g, n := range groups {
		total += n
		cum[g] = total
	}

	dists := make([][]int, len(groups))
	for g, threshold := range cum {
		dists[g] = simulate.DaysToComplete(pool, simulate.Params{
			Mode:        mode,
			Engineers:   engineers,
			Items:       threshold,
			Simulations: simulations,
			Workers:     goroutines,
			Seed:        seed,
		})
	}
	cells, totals := simulate.ComputeTrajectoryTable(dists, confidences)

	fmt.Printf("%s, starting %s -> grouped trajectory\n\n", simulate.ModeLabel(mode, engineers, nil), targetStartDate.Format("2006-01-02"))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	confRow := []string{"", ""}
	header := []string{"Group", "Items"}
	for _, c := range confidences {
		confRow = append(confRow, fmt.Sprintf("%d%%", c), "")
		header = append(header, "Days", "Date")
	}
	fmt.Fprintln(w, strings.Join(confRow, "\t"))
	fmt.Fprintln(w, strings.Join(header, "\t"))

	for g := range groups {
		row := []string{fmt.Sprintf("Group %d", g+1), fmt.Sprintf("%d", groups[g])}
		for pi := range confidences {
			cell := cells[g][pi]
			date := targetStartDate.AddDate(0, 0, cell.CumulativeDays)
			row = append(row, fmt.Sprintf("%d", cell.MarginalDays), date.Format("2006-01-02 (Mon)"))
		}
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	totalRow := []string{"Total", fmt.Sprintf("%d", total)}
	for pi := range confidences {
		days := totals[pi]
		date := targetStartDate.AddDate(0, 0, days)
		totalRow = append(totalRow, fmt.Sprintf("%d", days), date.Format("2006-01-02 (Mon)"))
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
	targetStartStr := cmd.String("target-start-date", "today", `forecast start date used to compute calendar dates (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months")`)
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

	mode, err := simulate.ResolveMode(isFlagSet(cmd, "engineers"), *sf.WholeTeam)
	if err != nil {
		return err
	}

	now := time.Now()
	startDate, err := util.ParseFlexibleStartDate(*sf.SampleStart, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-start date: %w", err)
	}
	endDate, err := util.ParseFlexibleDate(*sf.SampleEnd, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-end date: %w", err)
	}

	if len(items) == 0 {
		return fmt.Errorf("-items is required")
	}
	for _, n := range items {
		if n <= 0 {
			return fmt.Errorf("-items: group sizes must be positive, got %d", n)
		}
	}

	all, err := loadIssues(context.Background(), inputPath, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	loaded, err := loadPool(all, *sf.ExclusionsFile, sf.TypicalEngineers, startDate, endDate, *sf.WholeTeam)
	if err != nil {
		return err
	}
	pool := loaded.Pool
	if err := simulate.ValidatePool(pool, mode, true); err != nil {
		return err
	}
	seed := resolveSeed(cmd, *sf.RandomSeed, now)

	targetStartDate, err := util.ParseFlexibleStartDate(*targetStartStr, now)
	if err != nil {
		return fmt.Errorf("invalid -target-start-date: %w", err)
	}

	if len(confidences) == 0 {
		confidences = intList{50, 75, 85, 95}
	}

	if err := writeManifest(*manifestFile, manifestInputs{
		Subcommand: "sim days", Cmd: cmd, Mode: mode, TypicalEngineers: sf.TypicalEngineers,
		Engineers: *sf.Engineers, WholeTeam: *sf.WholeTeam, Seed: seed,
		SampleStart: startDate, SampleEnd: endDate,
		DBPath: inputPath, ExclusionsPath: *sf.ExclusionsFile,
		Exclusions: loaded.Exclusions, Pool: pool, Issues: loaded.Issues, Skipped: loaded.Skipped,
		Extra: map[string]any{"effective_confidence_levels": []int(confidences)},
	}); err != nil {
		return err
	}

	if len(items) > 1 {
		printTrajectoryReport(pool, mode, *sf.Engineers, seed, *sf.Simulations, *sf.Goroutines, items, confidences, targetStartDate)
		return nil
	}

	bar := newProgressBar(*sf.Simulations)
	dist := simulate.DaysToComplete(pool, simulate.Params{
		Mode:        mode,
		Engineers:   *sf.Engineers,
		Items:       items[0],
		Simulations: *sf.Simulations,
		Workers:     *sf.Goroutines,
		Seed:        seed,
		Progress:    bar.update,
	})
	fmt.Printf("%s, %d items -> how many days?\n\n", simulate.ModeLabel(mode, *sf.Engineers, nil), items[0])

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Confidence\tDays\tDate")
	for _, c := range confidences {
		days := simulate.PercentileValue(dist, float64(c))
		date := targetStartDate.AddDate(0, 0, days)
		fmt.Fprintf(w, "%d%%\t%d\t%s\n", c, days, date.Format("2006-01-02 Mon"))
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
	targetEndStr := cmd.String("target-end-date", "", `end of the target window (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months"); mutually exclusive with -days, one must be given`)
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

	mode, err := simulate.ResolveMode(isFlagSet(cmd, "engineers"), *sf.WholeTeam)
	if err != nil {
		return err
	}

	daysSet := isFlagSet(cmd, "days")
	targetEndSet := isFlagSet(cmd, "target-end-date")
	if daysSet && targetEndSet {
		return fmt.Errorf("-days and -target-end-date are mutually exclusive")
	}
	if !daysSet && !targetEndSet {
		return fmt.Errorf("one of -days or -target-end-date must be provided")
	}

	now := time.Now()
	startDate, err := util.ParseFlexibleStartDate(*sf.SampleStart, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-start date: %w", err)
	}
	endDate, err := util.ParseFlexibleDate(*sf.SampleEnd, now)
	if err != nil {
		return fmt.Errorf("invalid -sample-end date: %w", err)
	}

	effectiveDays := *days
	var targetStart, targetEnd time.Time
	if targetEndSet {
		targetStart, err = util.ParseFlexibleStartDate(*targetStartStr, now)
		if err != nil {
			return fmt.Errorf("invalid -target-start-date: %w", err)
		}
		targetEnd, err = util.ParseFlexibleDate(*targetEndStr, now)
		if err != nil {
			return fmt.Errorf("invalid -target-end-date: %w", err)
		}
		if !targetEnd.After(targetStart) {
			return fmt.Errorf("-target-end-date must be after -target-start-date")
		}
		effectiveDays = util.DayIndex(targetEnd, targetStart) + 1
	}

	all, err := loadIssues(context.Background(), inputPath, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	loaded, err := loadPool(all, *sf.ExclusionsFile, sf.TypicalEngineers, startDate, endDate, *sf.WholeTeam)
	if err != nil {
		return err
	}
	pool := loaded.Pool
	if err := simulate.ValidatePool(pool, mode, false); err != nil {
		return err
	}
	seed := resolveSeed(cmd, *sf.RandomSeed, now)

	manifestExtra := map[string]any{}
	if targetEndSet {
		manifestExtra["target_start_date"] = targetStart.Format("2006-01-02")
		manifestExtra["target_end_date"] = targetEnd.Format("2006-01-02")
		manifestExtra["effective_days"] = effectiveDays
	}
	if err := writeManifest(*manifestFile, manifestInputs{
		Subcommand: "sim probability", Cmd: cmd, Mode: mode, TypicalEngineers: sf.TypicalEngineers,
		Engineers: *sf.Engineers, WholeTeam: *sf.WholeTeam, Seed: seed,
		SampleStart: startDate, SampleEnd: endDate,
		DBPath: inputPath, ExclusionsPath: *sf.ExclusionsFile,
		Exclusions: loaded.Exclusions, Pool: pool, Issues: loaded.Issues, Skipped: loaded.Skipped,
		Extra: manifestExtra,
	}); err != nil {
		return err
	}

	bar := newProgressBar(*sf.Simulations)
	dist := simulate.ItemsInDays(pool, simulate.Params{
		Mode:        mode,
		Engineers:   *sf.Engineers,
		Days:        effectiveDays,
		Simulations: *sf.Simulations,
		Workers:     *sf.Goroutines,
		Seed:        seed,
		Progress:    bar.update,
	})
	modeDescription := simulate.ModeLabel(mode, *sf.Engineers, nil)

	var windowDescription string
	if targetEndSet {
		windowDescription = fmt.Sprintf("%s to %s (%d days)", targetStart.Format("2006-01-02"), targetEnd.Format("2006-01-02"), effectiveDays)
	} else {
		windowDescription = fmt.Sprintf("%d days", effectiveDays)
	}

	if *items >= 0 {
		p := simulate.ProbabilityAtLeast(dist, *items)
		fmt.Printf("%s, %s, %d items -> probability of completion?\n", modeDescription, windowDescription, *items)
		fmt.Printf("  %.1f%%  (i.e. you can commit to %d items here at ~%.0f%% confidence)\n", p, *items, p)
	} else {
		fmt.Printf("%s, %s -> probability of completing N items\n", modeDescription, windowDescription)
		fmt.Printf("  (each row's %% is also that many items' confidence level; see `sim items -confidence`)\n")
		for n := 1; ; n++ {
			p := simulate.ProbabilityAtLeast(dist, n)
			fmt.Printf("  %d items: %.1f%%\n", n, p)
			if p == 0 {
				break
			}
		}
	}
	return nil
}
