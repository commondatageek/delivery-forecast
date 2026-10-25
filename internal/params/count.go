package params

import (
	"flag"
	"time"

	"github.com/commondatageek/delivery-forecast/counts"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/linear"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

// Count holds the flags of `forecast count`. Milestones is adapter-only.
type Count struct {
	Input        Input
	Milestones   *bool
	UpdatedSince *string
	Teams        *linear.TeamKeyList
	Config       *string
}

// NewCount registers the `count` flags on fs. The -updated-since default is
// three months before now.
func NewCount(fs *flag.FlagSet, now time.Time) *Count {
	defaultSince := now.AddDate(0, -3, 0).Format("2006-01-02")

	c := &Count{}
	c.Input = AddInput(fs, true)
	c.Milestones = fs.Bool("milestones", false, "add a per-milestone breakdown under each project")
	c.UpdatedSince = fs.String("updated-since", defaultSince, `only include projects with an issue updated on/after this date (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months")`)
	c.Teams = AddTeams(fs, "comma-separated team keys to filter by (e.g. ENG,DESIGN); default: all teams")
	c.Config = AddConfig(fs)
	return c
}

// Resolve turns the parsed flags into a validated request; Issues is left for
// the adapter.
func (c *Count) Resolve(now time.Time) (Resolved[app.CountRequest], error) {
	var r Resolved[app.CountRequest]
	var err error
	r.InputPath, r.Warnings, err = c.Input.Resolve()
	if err != nil {
		return r, err
	}

	since, err := util.ParseFlexibleDate(*c.UpdatedSince, now)
	if err != nil {
		return r, fieldErr("updated-since", "invalid -updated-since %q: %v", *c.UpdatedSince, err)
	}

	r.Request = app.CountRequest{Options: counts.Options{Teams: *c.Teams, Since: since}}
	if err := r.Request.Validate(); err != nil {
		return r, err
	}
	return r, nil
}
