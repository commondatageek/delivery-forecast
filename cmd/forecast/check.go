package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/report"
	"github.com/commondatageek/delivery-forecast/internal/source"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdCheck(args []string) error {
	cmd := flag.NewFlagSet("check", flag.ExitOnError)
	input := addInputFlag(cmd)
	exclusions := cmd.String("exclusions", "", "path to an exclusions JSON file to validate against the input's assignees")
	stdinFormat := addStdinFormatFlag(cmd)
	configFile := addConfigFlag(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *configFile); err != nil {
		return err
	}

	if *input == "" {
		return fmt.Errorf("-input is required")
	}

	raw, err := loadIssues(context.Background(), *input, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	report.Check(os.Stdout, len(raw), *input, app.CheckReadiness(raw))

	if *exclusions != "" {
		// A file that can't be read is a finding about the file, not a failure
		// of check itself, so it prints as an "invalid" line like a parse error.
		exc, err := source.ReadExclusions(*exclusions)
		report.Exclusions(os.Stdout, *exclusions, app.CheckExclusions(exc, err, app.Assignees(raw), util.LocalDay(nowFunc())))
	}
	return nil
}
