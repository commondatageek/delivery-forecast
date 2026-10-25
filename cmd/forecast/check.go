package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/report"
	"github.com/commondatageek/delivery-forecast/internal/source"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdCheck(args []string) error {
	cmd := flag.NewFlagSet("check", flag.ExitOnError)
	c := params.NewCheck(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *c.Config); err != nil {
		return err
	}

	r, err := c.Resolve()
	printWarnings(r.Warnings)
	if err != nil {
		return err
	}

	raw, err := loadIssues(context.Background(), r.InputPath, *c.Input.StdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	report.Check(os.Stdout, len(raw), r.InputPath, app.CheckReadiness(raw))

	if r.Request.Exclusions != "" {
		// A file that can't be read is a finding about the file, not a failure
		// of check itself, so it prints as an "invalid" line like a parse error.
		exc, err := source.ReadExclusions(r.Request.Exclusions)
		report.Exclusions(os.Stdout, r.Request.Exclusions, app.CheckExclusions(exc, err, app.Assignees(raw), util.LocalDay(nowFunc())))
	}
	return nil
}
