package params

import (
	"flag"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/linear"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

// History holds the flags of `forecast history`. It has no deprecated -db
// alias. Format and Out are adapter-only.
type History struct {
	Input     Input
	Project   *string
	Milestone *string
	Teams     *linear.TeamKeyList
	Start     *string
	End       *string
	Window    *int
	Format    *string
	Out       *string
	Config    *string
}

// NewHistory registers the `history` flags on fs.
func NewHistory(fs *flag.FlagSet) *History {
	c := &History{}
	c.Input = AddInput(fs, false)
	c.Project = fs.String("project", "", "exact project name to scope to; default: all projects")
	c.Milestone = fs.String("milestone", "", "exact milestone name within -project; default: all milestones")
	c.Teams = AddTeams(fs, "comma-separated team keys to filter by (e.g. ENG,DATA); default: all teams")
	c.Start = fs.String("start", "", `first day emitted, inclusive (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: earliest created_at in scope`)
	c.End = fs.String("end", "today", `last day emitted, inclusive (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months")`)
	c.Window = fs.Int("window", 28, "trailing window in days for rolling metrics")
	c.Format = fs.String("format", "csv", `output format: "csv", "json", or "text"`)
	c.Out = fs.String("out", "", "write output to this file instead of stdout")
	c.Config = AddConfig(fs)
	return c
}

// Resolve turns the parsed flags into a validated request; Issues is left for
// the adapter.
func (c *History) Resolve(now time.Time) (Resolved[app.HistoryRequest], error) {
	var r Resolved[app.HistoryRequest]
	if f := *c.Format; f != "csv" && f != "json" && f != "text" {
		return r, &app.FieldError{Fields: []string{"format"}, Msg: `-format must be "csv", "json", or "text"`}
	}
	var err error
	r.InputPath, r.Warnings, err = c.Input.Resolve()
	if err != nil {
		return r, err
	}

	endDate, err := util.ParseFlexibleDate(*c.End, now)
	if err != nil {
		return r, fieldErr("end", "invalid -end %q: %v", *c.End, err)
	}
	var startDate time.Time
	if *c.Start != "" {
		startDate, err = util.ParseFlexibleStartDate(*c.Start, now)
		if err != nil {
			return r, fieldErr("start", "invalid -start %q: %v", *c.Start, err)
		}
	}

	r.Request = app.HistoryRequest{
		Teams:      *c.Teams,
		Project:    *c.Project,
		Milestone:  *c.Milestone,
		Start:      startDate,
		End:        endDate,
		WindowDays: *c.Window,
	}
	if err := r.Request.Validate(); err != nil {
		return r, err
	}
	return r, nil
}
