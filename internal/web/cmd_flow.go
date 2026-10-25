package web

import (
	"flag"
	"io"
	"time"

	"github.com/commondatageek/delivery-forecast/aging"
	"github.com/commondatageek/delivery-forecast/cfd"
	"github.com/commondatageek/delivery-forecast/counts"
	"github.com/commondatageek/delivery-forecast/history"
	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
)

func newAgingFlags(fs *flag.FlagSet, _ time.Time) any   { return params.NewAging(fs) }
func newCFDFlags(fs *flag.FlagSet, _ time.Time) any     { return params.NewCFD(fs) }
func newHistoryFlags(fs *flag.FlagSet, _ time.Time) any { return params.NewHistory(fs) }
func newCountFlags(fs *flag.FlagSet, now time.Time) any { return params.NewCount(fs, now) }

func prepareAging(rc *runContext, b any) (*runner, error) {
	c := b.(*params.Aging)
	r, err := c.Resolve(rc.now)
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	req := r.Request
	ds, err := rc.loadInput("input", r.InputPath)
	if err != nil {
		return nil, err
	}
	req.Issues = ds.issues
	return &runner{run: func(events) (*runOutput, error) {
		res, err := app.RunAging(req)
		out := &runOutput{Warnings: res.Warnings}
		if err != nil {
			return out, err
		}
		jsonReport := func(w io.Writer) error { return aging.RenderJSON(w, res.InProgress, res.Meta) }
		raw, err := rawReport(jsonReport)
		if err != nil {
			return out, err
		}
		out.Result = reportDTO{Report: raw}
		out.Exports = map[string]export{
			"text": textExport(func(w io.Writer) error {
				return aging.RenderText(w, res.InProgress, res.Completed, req.ShowCompleted, res.Meta)
			}),
			"html": htmlExport(func(w io.Writer) error {
				return aging.RenderHTML(w, res.InProgress, res.Completed, req.ShowCompleted, res.Meta)
			}),
			"json": rawJSONExport(jsonReport),
		}
		return out, nil
	}}, nil
}

func prepareCFD(rc *runContext, b any) (*runner, error) {
	c := b.(*params.CFD)
	r, err := c.Resolve(rc.now)
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	req := r.Request
	ds, err := rc.loadInput("input", r.InputPath)
	if err != nil {
		return nil, err
	}
	req.Issues = ds.issues
	return &runner{run: func(events) (*runOutput, error) {
		res, err := app.RunCFD(req)
		out := &runOutput{Warnings: res.Warnings}
		if err != nil {
			return out, err
		}
		jsonReport := func(w io.Writer) error { return cfd.RenderJSON(w, res.Rows, res.Health) }
		raw, err := rawReport(jsonReport)
		if err != nil {
			return out, err
		}
		out.Result = reportDTO{Report: raw, Start: date(req.Start), End: date(req.End)}
		out.Exports = map[string]export{
			"html": htmlExport(func(w io.Writer) error {
				return cfd.RenderHTML(w, res.Rows, res.Health, res.Health.TotalIssues, res.Health.SkippedIssues, req.Start, req.End)
			}),
			"json": rawJSONExport(jsonReport),
		}
		return out, nil
	}}, nil
}

func prepareHistory(rc *runContext, b any) (*runner, error) {
	c := b.(*params.History)
	r, err := c.Resolve(rc.now)
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	req := r.Request
	ds, err := rc.loadInput("input", r.InputPath)
	if err != nil {
		return nil, err
	}
	req.Issues = ds.issues
	return &runner{run: func(events) (*runOutput, error) {
		hres, err := app.RunHistory(req)
		out := &runOutput{Warnings: hres.Warnings}
		if err != nil {
			return out, err
		}
		res := hres.Result
		jsonReport := func(w io.Writer) error { return history.RenderJSON(w, res) }
		raw, err := rawReport(jsonReport)
		if err != nil {
			return out, err
		}
		out.Result = historyDTO{Report: raw, Start: date(hres.Start), StartInferred: hres.StartInferred}
		out.Exports = map[string]export{
			"text": textExport(func(w io.Writer) error { return history.RenderText(w, res) }),
			"csv":  csvExport(func(w io.Writer) error { return history.RenderCSV(w, res) }),
			"json": rawJSONExport(jsonReport),
		}
		return out, nil
	}}, nil
}

func prepareCount(rc *runContext, b any) (*runner, error) {
	c := b.(*params.Count)
	r, err := c.Resolve(rc.now)
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	req := r.Request
	ds, err := rc.loadInput("input", r.InputPath)
	if err != nil {
		return nil, err
	}
	req.Issues = ds.issues
	milestones := *c.Milestones
	return &runner{run: func(events) (*runOutput, error) {
		res, err := app.RunCount(req)
		out := &runOutput{Warnings: res.Warnings}
		if err != nil {
			return out, err
		}
		out.Result = newCountDTO(res)
		out.Exports = map[string]export{
			"text": textExport(func(w io.Writer) error {
				if milestones {
					return counts.RenderGrouped(w, res.Projects, res.Total, res.ShowTeams)
				}
				return counts.RenderSummary(w, res.Projects, res.Total, res.ShowTeams)
			}),
			"json": jsonExport(out.Result),
		}
		return out, nil
	}}, nil
}
