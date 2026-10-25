package params

import (
	"flag"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/flagvals"
)

// SimItems holds the flags of `forecast sim items`.
type SimItems struct {
	Input       Input
	Sim         *SimFlags
	Days        *int
	TargetStart *string // -target-start-date
	TargetEnd   *string // -target-end-date
	Confidence  flagvals.IntList
	Manifest    *string
	Config      *string
	fs          *flag.FlagSet
}

// NewSimItems registers the `sim items` flags on fs.
func NewSimItems(fs *flag.FlagSet) *SimItems {
	c := &SimItems{fs: fs}
	c.Input = AddInput(fs, true)
	c.Sim = AddSim(fs)
	c.Days = fs.Int("days", 0, "number of days; mutually exclusive with -target-end-date, one must be given")
	c.TargetStart = fs.String("target-start-date", "tomorrow", `start of the target window (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: tomorrow`)
	c.TargetEnd = fs.String("target-end-date", "", `end of the target window, inclusive (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months"); mutually exclusive with -days, one must be given`)
	fs.Var(&c.Confidence, "confidence", "comma-separated confidence levels to output, e.g. 85 means \"85% chance of completing at least N items\" (default: 50,75,85,95)")
	c.Manifest = fs.String("manifest", "", `write a run-provenance JSON manifest to this path ("-" for stdout)`)
	c.Config = AddConfig(fs)
	return c
}

// Resolve turns the parsed flags into a validated request. Issues,
// Exclusions and Progress are left for the adapter to fill in.
func (c *SimItems) Resolve(now time.Time) (Resolved[app.ItemsRequest], error) {
	var r Resolved[app.ItemsRequest]
	var err error
	r.InputPath, r.Warnings, err = c.Input.Resolve()
	if err != nil {
		return r, err
	}
	startDate, endDate, err := c.Sim.parseSampleWindow(now, true)
	if err != nil {
		return r, err
	}
	targetStart, windowDays, targetEnd, err := parseTargetWindow(c.fs, *c.Days, *c.TargetStart, *c.TargetEnd, now)
	if err != nil {
		return r, err
	}
	r.Request = app.ItemsRequest{
		SimBase:     c.Sim.Base(c.fs, startDate, endDate, now),
		TargetStart: targetStart,
		Days:        windowDays,
		TargetEnd:   targetEnd,
		Confidences: []int(c.Confidence),
	}
	if err := r.Request.Validate(); err != nil {
		return r, err
	}
	return r, nil
}
