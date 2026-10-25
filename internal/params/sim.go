package params

import (
	"flag"
	"fmt"
	"runtime"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/flagvals"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

// SimFlags bundles the sample-window/mode flag block shared by all four `sim`
// subcommands (items/days/probability/backtest). -items/-days/-confidence/
// -manifest differ per command and are declared there instead.
type SimFlags struct {
	ExclusionsFile   *string
	Engineers        flagvals.Engineers
	WholeTeam        *bool
	Simulations      *int
	Goroutines       *int
	SampleStart      *string
	SampleEnd        *string
	RandomSeed       *int64
	TypicalEngineers flagvals.StringList
}

// AddSim registers SimFlags's block onto fs and returns the bundle. The
// -simulations usage text defaults to "...to run"; callers with a different
// need (sim backtest runs simulations per backtested day) can override
// fs.Lookup("simulations").Usage afterward.
func AddSim(fs *flag.FlagSet) *SimFlags {
	sf := &SimFlags{}
	sf.ExclusionsFile = fs.String("exclusions", "", "path to an exclusions JSON file (holidays, PTO); applies to both the sample window and the forecast horizon; default: none")
	fs.Var(&sf.Engineers, "engineers", "number of equivalent engineers (e.g. 3) or their names (e.g. alice,bob,carol) so per-engineer exclusions apply; one of -engineers or -whole-team is required")
	sf.WholeTeam = fs.Bool("whole-team", false, "use whole-team daily throughput from historical data (ignores -engineers)")
	sf.Simulations = fs.Int("simulations", app.DefaultSimulations, "number of Monte Carlo simulations to run")
	sf.Goroutines = fs.Int("goroutines", runtime.NumCPU(), "number of parallel worker goroutines")
	sf.SampleStart = fs.String("sample-start", "-3 months", `sample data start date (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months")`)
	sf.SampleEnd = fs.String("sample-end", "now", `sample data end date (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months")`)
	sf.RandomSeed = fs.Int64("random-seed", 0, "seed for the random number generator (default: time-based, non-deterministic)")
	fs.Var(&sf.TypicalEngineers, "typical-engineers", "comma-separated list of the team's typical engineers to build the sample pool from (default: all)")
	return sf
}

// Base builds the request block shared by every sim subcommand from the
// parsed sim flags. Issues and Exclusions are filled in later by the adapter
// (after the request has been validated), and Progress by the caller.
func (sf *SimFlags) Base(fs *flag.FlagSet, sampleStart, sampleEnd, now time.Time) app.SimBase {
	return app.SimBase{
		TypicalEngineers: sf.TypicalEngineers,
		Engineers:        sf.Engineers.Count(),
		EngineerNames:    sf.Engineers.Names(),
		WholeTeam:        *sf.WholeTeam,
		SampleStart:      sampleStart,
		SampleEnd:        sampleEnd,
		Simulations:      *sf.Simulations,
		Workers:          *sf.Goroutines,
		Seed:             ResolveSeed(fs, *sf.RandomSeed, now),
	}
}

// fieldErr builds a FieldError blaming one flag.
func fieldErr(flagName, format string, args ...any) *app.FieldError {
	return &app.FieldError{Fields: []string{flagName}, Msg: fmt.Sprintf(format, args...)}
}

// parseSampleWindow parses -sample-start and -sample-end. prefix is the error
// text before the flag's "date" suffix: sim items/days/probability say
// "invalid -sample-start date", sim backtest says "invalid -sample-start".
func (sf *SimFlags) parseSampleWindow(now time.Time, withDate bool) (start, end time.Time, err error) {
	suffix := ""
	if withDate {
		suffix = " date"
	}
	start, err = util.ParseFlexibleStartDate(*sf.SampleStart, now)
	if err != nil {
		return start, end, fieldErr("sample-start", "invalid -sample-start%s: %v", suffix, err)
	}
	end, err = util.ParseFlexibleDate(*sf.SampleEnd, now)
	if err != nil {
		return start, end, fieldErr("sample-end", "invalid -sample-end%s: %v", suffix, err)
	}
	return start, end, nil
}

// parseTargetWindow parses -target-start-date, -days and -target-end-date
// into the typed values app.resolveTargetWindow expects: days is passed
// through only if -days was set, and end is parsed only if -target-end-date
// was set (a zero value means "not given").
func parseTargetWindow(fs *flag.FlagSet, days int, startStr, endStr string, now time.Time) (start time.Time, windowDays int, end time.Time, err error) {
	start, err = util.ParseFlexibleStartDate(startStr, now)
	if err != nil {
		return start, 0, end, fieldErr("target-start-date", "invalid -target-start-date: %v", err)
	}
	if IsSet(fs, "days") {
		windowDays = days
	}
	if IsSet(fs, "target-end-date") {
		end, err = util.ParseFlexibleDate(endStr, now)
		if err != nil {
			return start, 0, end, fieldErr("target-end-date", "invalid -target-end-date: %v", err)
		}
	}
	return start, windowDays, end, nil
}

// ManifestContext is what only an adapter knows about a run: how it was
// invoked, the binary, the input file's fingerprint and the clock.
type ManifestContext struct {
	Invocation  app.Invocation
	Binary      app.BinaryInfo
	Data        app.DataFile
	GeneratedAt time.Time
}

// SimManifest assembles the manifest parameter bag for a finished sim run,
// filling in the pieces internal/app may not touch via mc.
func SimManifest(subcommand string, fs *flag.FlagSet, sf *SimFlags, run app.SimRun, extra map[string]any, mc ManifestContext) app.ManifestInputs {
	return app.ManifestInputs{
		Subcommand:       subcommand,
		Run:              run,
		TypicalEngineers: sf.TypicalEngineers,
		Engineers:        sf.Engineers.Count(),
		EngineerNames:    sf.Engineers.Names(),
		WholeTeam:        *sf.WholeTeam,
		ExclusionsPath:   *sf.ExclusionsFile,
		Extra:            extra,
		Flags:            FlagRecords(fs),
		Invocation:       mc.Invocation,
		Binary:           mc.Binary,
		DB:               mc.Data,
		GeneratedAt:      mc.GeneratedAt,
	}
}
