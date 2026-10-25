package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/history"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdHistory(args []string) error {
	cmd := flag.NewFlagSet("history", flag.ExitOnError)
	c := params.NewHistory(cmd)
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

	hres, err := app.RunHistory(req)
	printWarnings(hres.Warnings)
	if err != nil {
		return err
	}
	res := hres.Result

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
	case "json":
		return history.RenderJSON(out, res)
	case "text":
		return history.RenderText(out, res)
	default:
		return history.RenderCSV(out, res)
	}
}
