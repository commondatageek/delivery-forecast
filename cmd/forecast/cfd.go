package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/cfd"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdCFD(args []string) error {
	cmd := flag.NewFlagSet("cfd", flag.ExitOnError)
	c := params.NewCFD(cmd)
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

	res, err := app.RunCFD(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}
	rows, health := res.Rows, res.Health
	opts := req.Options

	out := os.Stdout
	if *c.Out != "" {
		f, err := os.Create(*c.Out)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		out = f
	}

	switch *c.Format {
	case "html":
		return cfd.RenderHTML(out, rows, health, health.TotalIssues, health.SkippedIssues, opts.Start, opts.End)
	case "json":
		return cfd.RenderJSON(out, rows, health)
	default:
		return fmt.Errorf("unknown -format %q (use html or json)", *c.Format)
	}
}
