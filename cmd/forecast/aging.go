package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/aging"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdAging(args []string) error {
	cmd := flag.NewFlagSet("aging", flag.ExitOnError)
	c := params.NewAging(cmd)
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

	res, err := app.RunAging(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}
	meta, inProgressItems, completedItems := res.Meta, res.InProgress, res.Completed

	switch *c.Format {
	case "text":
		return aging.RenderText(os.Stdout, inProgressItems, completedItems, *c.ShowCompleted, meta)
	case "json":
		return aging.RenderJSON(os.Stdout, inProgressItems, meta)
	case "html":
		return aging.RenderHTML(os.Stdout, inProgressItems, completedItems, *c.ShowCompleted, meta)
	default:
		return fmt.Errorf("unknown -format %q (use text, json, or html)", *c.Format)
	}
}
