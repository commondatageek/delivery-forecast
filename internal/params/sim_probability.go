package params

import (
	"flag"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// SimProbability holds the flags of `forecast sim probability`.
type SimProbability struct {
	Input       Input
	Sim         *SimFlags
	Days        *int
	TargetStart *string // -target-start-date
	TargetEnd   *string // -target-end-date
	Items       *int
	Manifest    *string
	Config      *string
	fs          *flag.FlagSet
}

// NewSimProbability registers the `sim probability` flags on fs.
func NewSimProbability(fs *flag.FlagSet) *SimProbability {
	c := &SimProbability{fs: fs}
	c.Input = AddInput(fs, true)
	c.Sim = AddSim(fs)
	c.Days = fs.Int("days", 0, "number of days; mutually exclusive with -target-end-date, one must be given")
	c.TargetStart = fs.String("target-start-date", "tomorrow", `start of the target window (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: tomorrow`)
	c.TargetEnd = fs.String("target-end-date", "", `end of the target window, inclusive (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months"); mutually exclusive with -days, one must be given`)
	c.Items = fs.Int("items", -1, "number of items to complete (omit to show full distribution)")
	c.Manifest = fs.String("manifest", "", `write a run-provenance JSON manifest to this path ("-" for stdout)`)
	c.Config = AddConfig(fs)
	return c
}

// Resolve turns the parsed flags into a validated request.
func (c *SimProbability) Resolve(now time.Time) (Resolved[app.ProbabilityRequest], error) {
	var r Resolved[app.ProbabilityRequest]
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
	r.Request = app.ProbabilityRequest{
		SimBase:     c.Sim.Base(c.fs, startDate, endDate, now),
		TargetStart: targetStart,
		Days:        windowDays,
		TargetEnd:   targetEnd,
		Items:       *c.Items,
	}
	if err := r.Request.Validate(); err != nil {
		return r, err
	}
	return r, nil
}
