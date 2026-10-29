package web

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/report"
	"github.com/commondatageek/delivery-forecast/internal/source"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func newCheckFlags(fs *flag.FlagSet, _ time.Time) any  { return params.NewCheck(fs) }
func newImportFlags(fs *flag.FlagSet, _ time.Time) any { return params.NewImport(fs) }

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

func prepareImport(rc *runContext, b any) (*runner, error) {
	c := b.(*params.Import)
	r, err := c.Resolve()
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	req := r.Request
	if req.Example != "" {
		// meta.go hides -example; this is defense in depth.
		return nil, &app.FieldError{Fields: []string{"example"}, Msg: "-example is CLI-only; download a template with forecast import -example"}
	}
	absFrom, err := rc.s.resolvePath("from", req.From)
	if err != nil {
		return nil, err
	}
	absMap, err := rc.s.resolvePath("map", req.Map)
	if err != nil {
		return nil, err
	}
	var absOut string
	if !req.DryRun {
		if absOut, err = rc.s.resolvePath("out", req.Out); err != nil {
			return nil, err
		}
	}
	// Errors and the report quote the paths as typed, like the CLI's.
	typed := strings.NewReplacer(absFrom, req.From, absMap, req.Map)
	if absOut != "" {
		typed = strings.NewReplacer(absFrom, req.From, absMap, req.Map, absOut, req.Out)
	}
	return &runner{run: func(events) (*runOutput, error) {
		out := &runOutput{}
		// Not the request context: a closed tab must not abort a write midway.
		res, err := source.Import(context.Background(), absFrom, absMap, absOut, req.DryRun)
		if res.Wrote {
			rc.s.datasets.invalidate(absOut)
		}
		if err != nil {
			var fe *app.FieldError
			if errors.As(err, &fe) {
				return out, &app.FieldError{Fields: fe.Fields, Msg: typed.Replace(fe.Msg)}
			}
			return out, errors.New(typed.Replace(err.Error()))
		}
		readiness := app.CheckReadiness(res.Issues)
		out.Result = newImportDTO(res, readiness)
		out.Exports = map[string]export{
			"text": textExport(func(w io.Writer) error {
				report.Import(w, req.From, req.Out, req.DryRun, res.Deleted, res.Report, readiness)
				return nil
			}),
			"json": jsonExport(out.Result),
		}
		return out, nil
	}}, nil
}
