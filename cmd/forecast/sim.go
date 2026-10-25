package main

import (
	"flag"
	"os"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/report"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdSimItems(args []string) error {
	cmd := flag.NewFlagSet("sim items", flag.ExitOnError)
	c := params.NewSimItems(cmd)
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
	if err := loadSimInputs(&req.SimBase, r.InputPath, *c.Input.StdinFormat, *c.Sim.ExclusionsFile); err != nil {
		return err
	}
	dataFile := fingerprintForManifest(*c.Manifest, r.InputPath)
	bar := newProgressBar(*c.Sim.Simulations)
	req.Progress = bar.update

	res, err := app.SimItems(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if err := writeSimManifest(*c.Manifest, "sim items", cmd, c.Sim, dataFile, res.Run, res.ManifestExtra()); err != nil {
		return err
	}

	report.SimItems(os.Stdout, res)
	return nil
}

func cmdSimDays(args []string) error {
	cmd := flag.NewFlagSet("sim days", flag.ExitOnError)
	c := params.NewSimDays(cmd)
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
	if err := loadSimInputs(&req.SimBase, r.InputPath, *c.Input.StdinFormat, *c.Sim.ExclusionsFile); err != nil {
		return err
	}
	// Only the single-value path reports progress; the grouped trajectory runs
	// several simulations back to back and leaves the bar off.
	dataFile := fingerprintForManifest(*c.Manifest, r.InputPath)
	if len(c.Items) == 1 {
		bar := newProgressBar(*c.Sim.Simulations)
		req.Progress = bar.update
	}

	res, err := app.SimDays(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if err := writeSimManifest(*c.Manifest, "sim days", cmd, c.Sim, dataFile, res.Run, res.ManifestExtra()); err != nil {
		return err
	}

	report.SimDays(os.Stdout, res, c.Items)
	return nil
}

func cmdSimProbability(args []string) error {
	cmd := flag.NewFlagSet("sim probability", flag.ExitOnError)
	c := params.NewSimProbability(cmd)
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
	if err := loadSimInputs(&req.SimBase, r.InputPath, *c.Input.StdinFormat, *c.Sim.ExclusionsFile); err != nil {
		return err
	}
	dataFile := fingerprintForManifest(*c.Manifest, r.InputPath)
	bar := newProgressBar(*c.Sim.Simulations)
	req.Progress = bar.update

	res, err := app.SimProbability(req)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}

	if err := writeSimManifest(*c.Manifest, "sim probability", cmd, c.Sim, dataFile, res.Run, res.ManifestExtra()); err != nil {
		return err
	}

	report.SimProbability(os.Stdout, res)
	return nil
}
