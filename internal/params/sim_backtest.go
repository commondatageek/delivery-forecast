package params

import (
	"flag"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

// SimBacktest holds the flags of `forecast sim backtest`.
type SimBacktest struct {
	Input       Input
	Sim         *SimFlags
	Project     *string
	Milestone   *string
	ReplayStart *string // -replay-start-date
	TargetEnd   *string // -target-end-date
	Format      *string
	Config      *string
	fs          *flag.FlagSet
}

// NewSimBacktest registers the `sim backtest` flags on fs.
func NewSimBacktest(fs *flag.FlagSet) *SimBacktest {
	c := &SimBacktest{fs: fs}
	c.Input = AddInput(fs, true)
	c.Sim = AddSim(fs)
	fs.Lookup("simulations").Usage = "number of Monte Carlo simulations to run per backtested day"
	c.Project = fs.String("project", "", "project name to backtest (required)")
	c.Milestone = fs.String("milestone", "", "milestone name within the project (optional)")
	c.ReplayStart = fs.String("replay-start-date", "", `first day to replay from, inclusive (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: earliest started_at across the issue set`)
	c.TargetEnd = fs.String("target-end-date", "", `completion deadline to forecast against (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months"; required)`)
	c.Format = fs.String("format", "text", `output format: "text" or "csv"`)
	c.Config = AddConfig(fs)
	return c
}

// Resolve turns the parsed flags into a validated request. The sample
// window it parsed is available as Request.SampleStart/SampleEnd.
func (c *SimBacktest) Resolve(now time.Time) (Resolved[app.BacktestRequest], error) {
	var r Resolved[app.BacktestRequest]
	var err error
	r.InputPath, r.Warnings, err = c.Input.Resolve()
	if err != nil {
		return r, err
	}
	var targetDate time.Time
	if *c.TargetEnd != "" {
		targetDate, err = util.ParseFlexibleDate(*c.TargetEnd, now)
		if err != nil {
			return r, fieldErr("target-end-date", "invalid -target-end-date: %v", err)
		}
	}
	sampleStart, sampleEnd, err := c.Sim.parseSampleWindow(now, false)
	if err != nil {
		return r, err
	}
	var replayStart time.Time
	if *c.ReplayStart != "" {
		replayStart, err = util.ParseFlexibleStartDate(*c.ReplayStart, now)
		if err != nil {
			return r, fieldErr("replay-start-date", "invalid -replay-start-date: %v", err)
		}
	}
	r.Request = app.BacktestRequest{
		SimBase:     c.Sim.Base(c.fs, sampleStart, sampleEnd, now),
		Project:     *c.Project,
		Milestone:   *c.Milestone,
		ReplayStart: replayStart,
		TargetEnd:   targetDate,
		Today:       util.LocalDay(now),
	}
	if err := r.Request.Validate(); err != nil {
		return r, err
	}
	if *c.Format != "text" && *c.Format != "csv" {
		return r, &app.FieldError{Fields: []string{"format"}, Msg: `-format must be "text" or "csv"`}
	}
	return r, nil
}
