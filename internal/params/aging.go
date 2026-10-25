package params

import (
	"flag"
	"time"

	"github.com/commondatageek/delivery-forecast/aging"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/linear"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

// Aging holds the flags of `forecast aging`. Format is checked at render
// time, as the CLI always has.
type Aging struct {
	Input         Input
	SampleStart   *string
	SampleEnd     *string
	Format        *string
	MinCycleTime  *string
	Percentile    *int
	ShowCompleted *bool
	Teams         *linear.TeamKeyList
	Config        *string
}

// NewAging registers the `aging` flags on fs.
func NewAging(fs *flag.FlagSet) *Aging {
	c := &Aging{}
	c.Input = AddInput(fs, true)
	c.SampleStart = fs.String("sample-start", "-3 months", `start of completed-issue window (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months")`)
	c.SampleEnd = fs.String("sample-end", "today", `end of completed-issue window (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months")`)
	c.Format = fs.String("format", "text", "output format: text, json, html")
	c.MinCycleTime = fs.String("min-cycle-time", "", "exclude completed issues with cycle time below this duration (e.g. 5m, 1h, 1d)")
	c.Percentile = fs.Int("percentile", 85, "percentile of the cycle-time distribution to anchor the report to (1-100)")
	c.ShowCompleted = fs.Bool("show-completed", false, "text/html: also list the completed issues that make up the percentile distribution sample")
	c.Teams = AddTeams(fs, "comma-separated team keys to filter by (e.g. DATA,PLT); default: all teams")
	c.Config = AddConfig(fs)
	return c
}

// Resolve turns the parsed flags into a validated request; Issues is left for
// the adapter.
func (c *Aging) Resolve(now time.Time) (Resolved[app.AgingRequest], error) {
	var r Resolved[app.AgingRequest]
	var err error
	r.InputPath, r.Warnings, err = c.Input.Resolve()
	if err != nil {
		return r, err
	}

	var minCycleTime time.Duration
	if *c.MinCycleTime != "" {
		d, err := util.ParseFlexibleDuration(*c.MinCycleTime)
		if err != nil {
			return r, fieldErr("min-cycle-time", "invalid -min-cycle-time %q: %v", *c.MinCycleTime, err)
		}
		minCycleTime = d
	}

	today := util.LocalDay(now)

	sampleEnd, err := util.ParseFlexibleDate(*c.SampleEnd, now)
	if err != nil {
		return r, fieldErr("sample-end", "invalid -sample-end %q: %v", *c.SampleEnd, err)
	}
	sampleStart, err := util.ParseFlexibleStartDate(*c.SampleStart, now)
	if err != nil {
		return r, fieldErr("sample-start", "invalid -sample-start %q: %v", *c.SampleStart, err)
	}

	r.Request = app.AgingRequest{
		Options:       aging.Options{Teams: *c.Teams, SampleStart: sampleStart, SampleEnd: sampleEnd, MinCycleTime: minCycleTime, Percentile: *c.Percentile},
		ShowCompleted: *c.ShowCompleted,
		Today:         today,
	}
	if err := r.Request.Validate(); err != nil {
		return r, err
	}
	return r, nil
}
