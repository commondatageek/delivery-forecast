package params

import (
	"flag"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/flagvals"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

// SimDays holds the flags of `forecast sim days`.
type SimDays struct {
	Input       Input
	Sim         *SimFlags
	Items       flagvals.IntList
	TargetStart *string // -target-start-date
	Confidence  flagvals.IntList
	Manifest    *string
	Config      *string
	fs          *flag.FlagSet
}

// NewSimDays registers the `sim days` flags on fs.
func NewSimDays(fs *flag.FlagSet) *SimDays {
	c := &SimDays{fs: fs}
	c.Input = AddInput(fs, true)
	c.Sim = AddSim(fs)
	fs.Var(&c.Items, "items", "number of items to complete (required); comma-separated for a grouped trajectory report (e.g. 13,12,9)")
	c.TargetStart = fs.String("target-start-date", "tomorrow", `forecast start date used to compute calendar dates (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: tomorrow`)
	fs.Var(&c.Confidence, "confidence", "comma-separated confidence levels to output, e.g. 85 means \"85% chance of finishing within N days\" (default: 50,75,85,95)")
	c.Manifest = fs.String("manifest", "", `write a run-provenance JSON manifest to this path ("-" for stdout)`)
	c.Config = AddConfig(fs)
	return c
}

// Resolve turns the parsed flags into a validated request.
func (c *SimDays) Resolve(now time.Time) (Resolved[app.DaysRequest], error) {
	var r Resolved[app.DaysRequest]
	var err error
	r.InputPath, r.Warnings, err = c.Input.Resolve()
	if err != nil {
		return r, err
	}
	startDate, endDate, err := c.Sim.parseSampleWindow(now, true)
	if err != nil {
		return r, err
	}
	targetStartDate, err := util.ParseFlexibleStartDate(*c.TargetStart, now)
	if err != nil {
		return r, fieldErr("target-start-date", "invalid -target-start-date: %v", err)
	}
	r.Request = app.DaysRequest{
		SimBase:     c.Sim.Base(c.fs, startDate, endDate, now),
		TargetStart: targetStartDate,
		Items:       []int(c.Items),
		Confidences: []int(c.Confidence),
	}
	if err := r.Request.Validate(); err != nil {
		return r, err
	}
	return r, nil
}
