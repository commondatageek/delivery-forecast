package params

import (
	"flag"
	"time"

	"github.com/commondatageek/delivery-forecast/cfd"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/linear"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

// CFD holds the flags of `forecast cfd`. Format and Out are adapter-only.
type CFD struct {
	Input  Input
	Start  *string
	End    *string
	Format *string
	Out    *string
	Teams  *linear.TeamKeyList
	Config *string
}

// NewCFD registers the `cfd` flags on fs.
func NewCFD(fs *flag.FlagSet) *CFD {
	c := &CFD{}
	c.Input = AddInput(fs, true)
	c.Start = fs.String("start", "-3 months", `start date, inclusive (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months")`)
	c.End = fs.String("end", "today", `end date, inclusive (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months")`)
	c.Format = fs.String("format", "html", "output format: html, json")
	c.Out = fs.String("out", "", "write output to this file instead of stdout")
	c.Teams = AddTeams(fs, "comma-separated team keys to filter by (e.g. ENG,DATA); default: all teams")
	c.Config = AddConfig(fs)
	return c
}

// Resolve turns the parsed flags into a validated request; Issues is left for
// the adapter.
func (c *CFD) Resolve(now time.Time) (Resolved[app.CFDRequest], error) {
	var r Resolved[app.CFDRequest]
	var err error
	r.InputPath, r.Warnings, err = c.Input.Resolve()
	if err != nil {
		return r, err
	}

	windowEnd, err := util.ParseFlexibleDate(*c.End, now)
	if err != nil {
		return r, fieldErr("end", "invalid -end %q: %v", *c.End, err)
	}
	windowStart, err := util.ParseFlexibleStartDate(*c.Start, now)
	if err != nil {
		return r, fieldErr("start", "invalid -start %q: %v", *c.Start, err)
	}

	r.Request = app.CFDRequest{Options: cfd.Options{Teams: *c.Teams, Start: windowStart, End: windowEnd}}
	if err := r.Request.Validate(); err != nil {
		return r, err
	}
	return r, nil
}
