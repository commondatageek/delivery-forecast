package web

import (
	"flag"
	"io"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/report"
	"github.com/commondatageek/delivery-forecast/internal/source"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func newCheckFlags(fs *flag.FlagSet, _ time.Time) any { return params.NewCheck(fs) }

func prepareCheck(rc *runContext, b any) (*runner, error) {
	c := b.(*params.Check)
	r, err := c.Resolve()
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	ds, err := rc.loadInput("input", r.InputPath)
	if err != nil {
		return nil, err
	}
	excPath := r.Request.Exclusions
	if excPath != "" {
		// A bad path parameter is a 400; an unreadable or invalid file is a
		// finding, as in the CLI.
		if _, err := rc.s.resolvePath("exclusions", excPath); err != nil {
			return nil, err
		}
	}
	return &runner{run: func(events) (*runOutput, error) {
		out := &runOutput{}
		readiness := app.CheckReadiness(ds.issues)
		var excReport *app.ExclusionsReport
		if excPath != "" {
			exc, err := source.ReadExclusions(excPath)
			rep := app.CheckExclusions(exc, err, app.Assignees(ds.issues), util.LocalDay(rc.now))
			excReport = &rep
		}
		var excDTO *exclusionsDTO
		if excReport != nil {
			excDTO = newExclusionsDTO(excPath, *excReport)
		}
		out.Result = newCheckDTO(r.InputPath, len(ds.issues), readiness, excDTO)
		out.Exports = map[string]export{
			"text": textExport(func(w io.Writer) error {
				report.Check(w, len(ds.issues), r.InputPath, readiness)
				if excReport != nil {
					report.Exclusions(w, excPath, *excReport)
				}
				return nil
			}),
			"json": jsonExport(out.Result),
		}
		return out, nil
	}}, nil
}
