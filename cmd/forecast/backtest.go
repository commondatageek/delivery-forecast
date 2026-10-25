package main

import (
	"flag"
	"os"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/report"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdSimBacktest(args []string) error {
	cmd := flag.NewFlagSet("sim backtest", flag.ExitOnError)
	c := params.NewSimBacktest(cmd)
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

	// Load the issue set once: it feeds both the sample pool and the fixed
	// backtested set.
	if err := loadSimInputs(&req.SimBase, r.InputPath, *c.Input.StdinFormat, *c.Sim.ExclusionsFile); err != nil {
		return err
	}

	res, err := app.SimBacktest(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	switch *c.Format {
	case "csv":
		return report.BacktestCSV(os.Stdout, res)
	default:
		report.BacktestText(os.Stdout, res, req.SampleStart, req.SampleEnd, req.Today)
	}
	return nil
}
