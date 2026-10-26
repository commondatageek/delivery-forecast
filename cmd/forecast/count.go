package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/counts"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdCount(args []string) error {
	cmd := flag.NewFlagSet("count", flag.ExitOnError)
	c := params.NewCount(cmd, nowFunc())
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *c.Config); err != nil {
		return err
	}

	r, err := c.Resolve(nowFunc())
	printWarnings(r.Warnings)
	if err != nil {
		return err
	}
	req := r.Request

	req.Issues, err = loadIssues(context.Background(), r.InputPath, *c.Input.StdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	res, err := app.RunCount(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if *c.Milestones {
		return counts.RenderGrouped(os.Stdout, res.Projects, res.Total, res.ShowTeams)
	}
	return counts.RenderSummary(os.Stdout, res.Projects, res.Total, res.ShowTeams)
}
