package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/flagvals"
	"github.com/commondatageek/delivery-forecast/internal/linear"
	"github.com/commondatageek/delivery-forecast/internal/logx"
	"github.com/commondatageek/delivery-forecast/internal/source"
	"github.com/commondatageek/delivery-forecast/internal/util"

	"github.com/mattn/go-isatty"
)

// --- Progress reporting ---

type progressBar struct {
	enabled bool
	total   int
	step    int
	mu      sync.Mutex
}

func newProgressBar(total int) *progressBar {
	return &progressBar{
		enabled: isatty.IsTerminal(os.Stderr.Fd()) && total > 0,
		total:   total,
		step:    max(1, total/200),
	}
}

func (b *progressBar) update(done, _ int) {
	if !b.enabled || (done != b.total && done%b.step != 0) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	const width = 30
	filled := width * done / b.total
	fmt.Fprintf(os.Stderr, "\r[%s%s] %d/%d", strings.Repeat("=", filled), strings.Repeat(" ", width-filled), done, b.total)
	if done == b.total {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

// --- Sim request helpers ---

// parseTargetWindowFlags parses -target-start-date, -days and
// -target-end-date into the typed values app.resolveTargetWindow expects:
// days is passed through only if -days was set, and end is parsed only if
// -target-end-date was set (a zero value means "not given").
func parseTargetWindowFlags(cmd *flag.FlagSet, days int, startStr, endStr string, now time.Time) (start time.Time, windowDays int, end time.Time, err error) {
	start, err = util.ParseFlexibleStartDate(startStr, now)
	if err != nil {
		return start, 0, end, fmt.Errorf("invalid -target-start-date: %w", err)
	}
	if isFlagSet(cmd, "days") {
		windowDays = days
	}
	if isFlagSet(cmd, "target-end-date") {
		end, err = util.ParseFlexibleDate(endStr, now)
		if err != nil {
			return start, 0, end, fmt.Errorf("invalid -target-end-date: %w", err)
		}
	}
	return start, windowDays, end, nil
}

// newSimBase builds the request block shared by every sim subcommand from the
// parsed sim flags. Issues and Exclusions are filled in later by loadSimInputs
// (after the request has been validated), and Progress by the caller.
func newSimBase(cmd *flag.FlagSet, sf *simFlags, sampleStart, sampleEnd, now time.Time) app.SimBase {
	return app.SimBase{
		TypicalEngineers: sf.TypicalEngineers,
		Engineers:        sf.Engineers.Count(),
		EngineerNames:    sf.Engineers.Names(),
		WholeTeam:        *sf.WholeTeam,
		SampleStart:      sampleStart,
		SampleEnd:        sampleEnd,
		Simulations:      *sf.Simulations,
		Workers:          *sf.Goroutines,
		Seed:             resolveSeed(cmd, *sf.RandomSeed, now),
	}
}

// loadSimInputs loads the issue set and the exclusions file into b.
func loadSimInputs(b *app.SimBase, inputPath, stdinFormat, exclusionsFile string) error {
	all, err := loadIssues(context.Background(), inputPath, stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}
	exc, err := source.LoadExclusions(exclusionsFile)
	if err != nil {
		return err
	}
	b.Issues, b.Exclusions = all, exc
	return nil
}

// printWarnings logs every warning an app call returned, in order.
func printWarnings(warnings []string) {
	for _, w := range warnings {
		logx.Warnf("%s", w)
	}
}

// simManifest assembles the manifest parameter bag for a finished sim run,
// filling in the pieces internal/app may not touch: flags, argv, build info,
// the input file's fingerprint and the clock.
func simManifest(subcommand string, cmd *flag.FlagSet, sf *simFlags, inputFile app.DataFile, run app.SimRun, extra map[string]any) app.ManifestInputs {
	wd, _ := os.Getwd()
	return app.ManifestInputs{
		Subcommand:       subcommand,
		Run:              run,
		TypicalEngineers: sf.TypicalEngineers,
		Engineers:        sf.Engineers.Count(),
		EngineerNames:    sf.Engineers.Names(),
		WholeTeam:        *sf.WholeTeam,
		ExclusionsPath:   *sf.ExclusionsFile,
		Extra:            extra,
		Flags:            flagRecords(cmd),
		Invocation:       app.Invocation{Subcommand: subcommand, Args: os.Args, WorkingDir: wd},
		Binary:           buildInfo(),
		DB:               inputFile,
		GeneratedAt:      nowFunc(),
	}
}

// fingerprintForManifest fingerprints the input file as it was when loaded, so
// the manifest describes the data the run actually used even if the file
// changes during a long simulation. It skips the hash when no manifest is
// wanted.
func fingerprintForManifest(manifestPath, inputPath string) app.DataFile {
	if manifestPath == "" {
		return app.DataFile{}
	}
	return source.Fingerprint(inputPath)
}

// writeSimManifest writes the run manifest for a finished sim run to path. It
// is a no-op (and skips the input file's hash) when path is empty.
func writeSimManifest(path, subcommand string, cmd *flag.FlagSet, sf *simFlags, inputFile app.DataFile, run app.SimRun, extra map[string]any) error {
	if path == "" {
		return nil
	}
	return writeManifest(path, simManifest(subcommand, cmd, sf, inputFile, run, extra))
}

// --- Flag helpers ---

// isFlagSet reports whether a flag was explicitly provided on the command line
// or via a config file (ApplyConfig applies config values via fs.Set, so they
// register as set and drive the same presence-sensitive behavior as CLI flags).
func isFlagSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// resolveSeed returns randomSeed if -random-seed was explicitly set, otherwise
// a time-based seed so runs are non-deterministic by default.
func resolveSeed(cmd *flag.FlagSet, randomSeed int64, now time.Time) int64 {
	if isFlagSet(cmd, "random-seed") {
		return randomSeed
	}
	return now.UnixNano()
}

// --- Shared flag registration ---
//
// These helpers just wrap repeated fs.String/fs.Var calls; each subcommand
// still owns its FlagSet, still calls Parse itself, and still applies
// -config and the isFlagSet presence checks in the same order as before.

// addDBFlag registers the -db flag used by every subcommand that reads a
// SQLite store.
func addDBFlag(fs *flag.FlagSet) *string {
	return fs.String("db", "", "path to SQLite database")
}

// requireDB reports an error if -db was left unset.
func requireDB(db *string) error {
	if *db == "" {
		return fmt.Errorf("-db is required")
	}
	return nil
}

// addConfigFlag registers the -config flag used by every subcommand.
func addConfigFlag(fs *flag.FlagSet) *string {
	return fs.String("config", "", "path to a YAML config file supplying flag values (CLI flags override)")
}

// addTeamsFlag registers the -teams flag. usage is passed in whole (not just
// an example) because its wording differs meaningfully between commands that
// filter to a team set (aging/cfd/count) and linear sync, where -teams
// extends the candidate set rather than filtering.
func addTeamsFlag(fs *flag.FlagSet, usage string) *linear.TeamKeyList {
	var teams linear.TeamKeyList
	fs.Var(&teams, "teams", usage)
	return &teams
}

// simFlags bundles the sample-window/mode flag block shared by all four `sim`
// subcommands (items/days/probability/backtest). -items/-days/-confidence/
// -manifest differ per command and are declared there instead.
type simFlags struct {
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

// addSimFlags registers simFlags's block onto fs and returns the bundle. The
// -simulations usage text defaults to "...to run"; callers with a different
// need (sim backtest runs simulations per backtested day) can override
// fs.Lookup("simulations").Usage afterward.
func addSimFlags(fs *flag.FlagSet) *simFlags {
	sf := &simFlags{}
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

// nowFunc is the CLI's clock. Tests replace it to make date defaults
// ("now", "today", "-3 months") and aging/backtest "today" deterministic.
var nowFunc = time.Now
