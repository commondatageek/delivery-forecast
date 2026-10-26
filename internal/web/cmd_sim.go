package web

import (
	"flag"
	"io"
	"os"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/report"
	"github.com/commondatageek/delivery-forecast/internal/source"
)

func newSimItemsFlags(fs *flag.FlagSet, _ time.Time) any       { return params.NewSimItems(fs) }
func newSimDaysFlags(fs *flag.FlagSet, _ time.Time) any        { return params.NewSimDays(fs) }
func newSimProbabilityFlags(fs *flag.FlagSet, _ time.Time) any { return params.NewSimProbability(fs) }
func newSimBacktestFlags(fs *flag.FlagSet, _ time.Time) any    { return params.NewSimBacktest(fs) }

// loadSim loads the issue set and exclusions a sim request needs into b.
func (rc *runContext) loadSim(b *app.SimBase, inputPath, exclusionsPath string) (*dataset, error) {
	ds, err := rc.loadInput("input", inputPath)
	if err != nil {
		return nil, err
	}
	if exclusionsPath != "" {
		if _, err := rc.s.resolvePath("exclusions", exclusionsPath); err != nil {
			return nil, err
		}
	}
	exc, err := source.LoadExclusions(exclusionsPath)
	if err != nil {
		return nil, &app.FieldError{Fields: []string{"exclusions"}, Msg: err.Error()}
	}
	b.Issues, b.Exclusions = ds.issues, exc
	return ds, nil
}

// manifestExport builds the lazily-encoded manifest download for a sim run.
func (rc *runContext) manifestExport(subcommand string, sf *params.SimFlags, run app.SimRun, extra map[string]any, ds *dataset) export {
	wd, _ := os.Getwd()
	mc := params.ManifestContext{
		Invocation:  app.Invocation{Subcommand: subcommand, Args: params.CommandLine(subcommand, rc.fs), WorkingDir: wd},
		Binary:      rc.s.opts.Binary,
		Data:        ds.file,
		GeneratedAt: rc.now,
	}
	fs := rc.fs
	return jsonExport(nil).withWrite(func(w io.Writer) error {
		return encodeIndented(w, app.NewManifest(params.SimManifest(subcommand, fs, sf, run, extra, mc)))
	})
}

func (e export) withWrite(w func(io.Writer) error) export {
	e.Write = w
	return e
}

func prepareSimItems(rc *runContext, b any) (*runner, error) {
	c := b.(*params.SimItems)
	r, err := c.Resolve(rc.now)
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	req := r.Request
	ds, err := rc.loadSim(&req.SimBase, r.InputPath, *c.Sim.ExclusionsFile)
	if err != nil {
		return nil, err
	}
	return &runner{run: func(ev events) (*runOutput, error) {
		req.Progress = ev.Progress
		res, err := app.SimItems(req)
		out := &runOutput{Warnings: res.Warnings}
		if err != nil {
			return out, err
		}
		out.Result = newItemsDTO(res)
		out.Exports = map[string]export{
			"text":     textExport(func(w io.Writer) error { report.SimItems(w, res); return nil }),
			"json":     jsonExport(out.Result),
			"manifest": rc.manifestExport("sim items", c.Sim, res.Run, res.ManifestExtra(), ds),
		}
		return out, nil
	}}, nil
}

func prepareSimDays(rc *runContext, b any) (*runner, error) {
	c := b.(*params.SimDays)
	r, err := c.Resolve(rc.now)
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	req := r.Request
	ds, err := rc.loadSim(&req.SimBase, r.InputPath, *c.Sim.ExclusionsFile)
	if err != nil {
		return nil, err
	}
	return &runner{run: func(ev events) (*runOutput, error) {
		// Only the single-value path reports progress; the grouped trajectory
		// runs several simulations back to back.
		if len(req.Items) == 1 {
			req.Progress = ev.Progress
		}
		res, err := app.SimDays(req)
		out := &runOutput{Warnings: res.Warnings}
		if err != nil {
			return out, err
		}
		out.Result = newDaysDTO(res, req.Items)
		out.Exports = map[string]export{
			"text":     textExport(func(w io.Writer) error { report.SimDays(w, res, req.Items); return nil }),
			"json":     jsonExport(out.Result),
			"manifest": rc.manifestExport("sim days", c.Sim, res.Run, res.ManifestExtra(), ds),
		}
		return out, nil
	}}, nil
}

func prepareSimProbability(rc *runContext, b any) (*runner, error) {
	c := b.(*params.SimProbability)
	r, err := c.Resolve(rc.now)
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	req := r.Request
	ds, err := rc.loadSim(&req.SimBase, r.InputPath, *c.Sim.ExclusionsFile)
	if err != nil {
		return nil, err
	}
	return &runner{run: func(ev events) (*runOutput, error) {
		req.Progress = ev.Progress
		res, err := app.SimProbability(req)
		out := &runOutput{Warnings: res.Warnings}
		if err != nil {
			return out, err
		}
		out.Result = newProbabilityDTO(res)
		out.Exports = map[string]export{
			"text":     textExport(func(w io.Writer) error { report.SimProbability(w, res); return nil }),
			"json":     jsonExport(out.Result),
			"manifest": rc.manifestExport("sim probability", c.Sim, res.Run, res.ManifestExtra(), ds),
		}
		return out, nil
	}}, nil
}

func prepareSimBacktest(rc *runContext, b any) (*runner, error) {
	c := b.(*params.SimBacktest)
	r, err := c.Resolve(rc.now)
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	req := r.Request
	if _, err := rc.loadSim(&req.SimBase, r.InputPath, *c.Sim.ExclusionsFile); err != nil {
		return nil, err
	}
	return &runner{run: func(ev events) (*runOutput, error) {
		req.Progress = ev.Progress
		res, err := app.SimBacktest(req)
		out := &runOutput{Warnings: res.Warnings}
		if err != nil {
			return out, err
		}
		out.Result = newBacktestDTO(res, req.Today)
		out.Exports = map[string]export{
			"text": textExport(func(w io.Writer) error {
				report.BacktestText(w, res, req.SampleStart, req.SampleEnd, req.Today)
				return nil
			}),
			"csv":  csvExport(func(w io.Writer) error { return report.BacktestCSV(w, res) }),
			"json": jsonExport(out.Result),
		}
		return out, nil
	}}, nil
}
