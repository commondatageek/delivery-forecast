package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/flagvals"
	"github.com/commondatageek/delivery-forecast/internal/report"
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
	var confidences flagvals.IntList
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
	dataFile := fingerprintForManifest(*manifestFile, inputPath)
	bar := newProgressBar(*sf.Simulations)
	req.Progress = bar.update

	res, err := app.SimItems(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if err := writeSimManifest(*manifestFile, "sim items", cmd, sf, dataFile, res.Run, map[string]any{
		"effective_confidence_levels": res.Confidences,
		"target_start_date":           res.TargetStart.Format("2006-01-02"),
		"target_end_date":             res.TargetEnd.Format("2006-01-02"),
		"effective_days":              res.EffectiveDays,
	}); err != nil {
		return err
	}

	report.SimItems(os.Stdout, res)
	return nil
}

func cmdSimDays(args []string) error {
	cmd := flag.NewFlagSet("sim days", flag.ExitOnError)
	dbFile := addDBFlag(cmd)
	inputFile := addInputFlag(cmd)
	stdinFormat := addStdinFormatFlag(cmd)
	sf := addSimFlags(cmd)
	var items flagvals.IntList
	cmd.Var(&items, "items", "number of items to complete (required); comma-separated for a grouped trajectory report (e.g. 13,12,9)")
	targetStartStr := cmd.String("target-start-date", "tomorrow", `forecast start date used to compute calendar dates (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: tomorrow`)
	var confidences flagvals.IntList
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
	dataFile := fingerprintForManifest(*manifestFile, inputPath)
	if len(items) == 1 {
		bar := newProgressBar(*sf.Simulations)
		req.Progress = bar.update
	}

	res, err := app.SimDays(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if err := writeSimManifest(*manifestFile, "sim days", cmd, sf, dataFile, res.Run, map[string]any{
		"effective_confidence_levels": res.Confidences,
		"target_start_date":           res.TargetStart.Format("2006-01-02"),
	}); err != nil {
		return err
	}

	report.SimDays(os.Stdout, res, items)
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
	dataFile := fingerprintForManifest(*manifestFile, inputPath)
	bar := newProgressBar(*sf.Simulations)
	req.Progress = bar.update

	res, err := app.SimProbability(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if err := writeSimManifest(*manifestFile, "sim probability", cmd, sf, dataFile, res.Run, map[string]any{
		"target_start_date": res.TargetStart.Format("2006-01-02"),
		"target_end_date":   res.TargetEnd.Format("2006-01-02"),
		"effective_days":    res.EffectiveDays,
	}); err != nil {
		return err
	}

	report.SimProbability(os.Stdout, res)
	return nil
}
