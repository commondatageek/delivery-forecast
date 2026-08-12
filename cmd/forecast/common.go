package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/linear"
	"github.com/commondatageek/delivery-forecast/internal/logx"
	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"

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

// --- Pool loading ---

// poolData bundles the built pool with the raw inputs that produced it, so a
// run manifest can record exactly what fed the simulation.
type poolData struct {
	Pool       *simulate.SamplePool
	Issues     []linear.Issue
	Exclusions simulate.Exclusions
	Skipped    int
}

// issuesToCompletions converts issues.Issue records to simulate.Completion.
// No filtering is performed; call simulate.FilterInvalid on the result.
func issuesToCompletions(items []issues.Issue) []simulate.Completion {
	records := make([]simulate.Completion, len(items))
	for i, it := range items {
		records[i] = simulate.Completion{Engineer: it.Assignee, CompletedAt: it.CompletedAt}
	}
	return records
}

// completedForPool selects completed issues assigned to one of engineers (or
// any assignee, if engineers is empty) whose completed_at falls in
// [start, end) — start inclusive, end exclusive. Mirrors the old
// CompletedBetween SQL query (state_type = 'completed', now Issue.
// IsCompleted so file sources without state_type still work per D7), except
// matching is now case-sensitive-exact against the given engineer names, same
// as the SQL IN clause was.
//
// Unlike aging's equivalent, this deliberately keeps CompletedBetween's
// unconditional non-empty-assignee requirement: the pool is per-engineer, so
// an unassigned issue has nowhere to go. See cmd/forecast/aging.go's
// completedBetween for the contrasting case.
func completedForPool(items []issues.Issue, start, end time.Time, engineers []string) []issues.Issue {
	want := make(map[string]bool, len(engineers))
	for _, e := range engineers {
		want[e] = true
	}
	var out []issues.Issue
	for _, it := range items {
		if !it.IsCompleted() || it.CompletedAt.IsZero() || it.Assignee == "" {
			continue
		}
		if it.CompletedAt.Before(start) || !it.CompletedAt.Before(end) {
			continue
		}
		if len(want) > 0 && !want[it.Assignee] {
			continue
		}
		out = append(out, it)
	}
	return out
}

// warnUnmatchedTypicalEngineers logs a warning for any name in typicalEngineers
// that doesn't appear in seen, which usually indicates a typo in
// -typical-engineers.
func warnUnmatchedTypicalEngineers(typicalEngineers []string, seen map[string]bool) {
	for _, name := range typicalEngineers {
		if !seen[name] {
			logx.Warnf("-typical-engineers engineer %q not found in data", name)
		}
	}
}

// loadExclusions reads and parses an exclusions JSON file. If the file does
// not exist, an empty Exclusions is returned without error.
func loadExclusions(path string) (simulate.Exclusions, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return simulate.Exclusions{}, nil
		}
		return simulate.Exclusions{}, fmt.Errorf("reading exclusions file: %w", err)
	}
	return simulate.ParseExclusions(data)
}

// loadPool builds a SamplePool from all, an already-loaded issue set (any
// source — sim has no -teams flag, so every team is always pooled together,
// same as before). See completedForPool for the filtering it applies.
func loadPool(all []issues.Issue, exclusionsFile string, typicalEngineers []string, startDate, endDate time.Time, wholeTeam bool) (poolData, error) {
	completed := completedForPool(all, startDate, endDate, typicalEngineers)

	engineerSeen := make(map[string]bool, len(completed))
	for _, it := range completed {
		engineerSeen[it.Assignee] = true
	}
	warnUnmatchedTypicalEngineers(typicalEngineers, engineerSeen)

	exc, err := loadExclusions(exclusionsFile)
	if err != nil {
		return poolData{}, err
	}

	records, skipped := simulate.FilterInvalid(issuesToCompletions(completed))
	if skipped > 0 {
		logx.Warnf("skipped %d completed issue(s) with no assignee or completion date", skipped)
	}

	return poolData{
		Pool:       simulate.BuildPool(records, exc, startDate, endDate, wholeTeam),
		Issues:     completed,
		Exclusions: exc,
		Skipped:    skipped,
	}, nil
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

// blendingTeamsWarning returns the "blending across all teams" warning line, or
// "" when no warning is warranted: either the user scoped explicitly (teams
// non-empty) or the store holds at most one team (allTeams). Kept pure (no I/O)
// so callers that already know the full team set — like count — can reuse the
// exact message without a second DistinctTeamKeys query, and so it is unit
// testable.
func blendingTeamsWarning(teams linear.TeamKeyList, allTeams []string) string {
	if len(teams) > 0 || len(allTeams) <= 1 {
		return ""
	}
	return fmt.Sprintf("no -teams filter given; blending data across all %d teams (%s)",
		len(allTeams), strings.Join(allTeams, ", "))
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
	Engineers        *int
	WholeTeam        *bool
	Simulations      *int
	Goroutines       *int
	SampleStart      *string
	SampleEnd        *string
	RandomSeed       *int64
	TypicalEngineers stringList
	Team             stringList
}

// addSimFlags registers simFlags's block onto fs and returns the bundle. The
// -simulations usage text defaults to "...to run"; callers with a different
// need (sim backtest runs simulations per backtested day) can override
// fs.Lookup("simulations").Usage afterward.
func addSimFlags(fs *flag.FlagSet) *simFlags {
	sf := &simFlags{}
	sf.ExclusionsFile = fs.String("exclusions", "exclusions.json", "path to exclusions JSON file")
	sf.Engineers = fs.Int("engineers", 0, "number of (equivalent) engineers; one of -engineers, -team, or -whole-team is required")
	sf.WholeTeam = fs.Bool("whole-team", false, "use whole-team daily throughput from historical data (ignores -engineers)")
	sf.Simulations = fs.Int("simulations", 10_000, "number of Monte Carlo simulations to run")
	sf.Goroutines = fs.Int("goroutines", runtime.NumCPU(), "number of parallel worker goroutines")
	sf.SampleStart = fs.String("sample-start", "-3 months", `sample data start date (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months")`)
	sf.SampleEnd = fs.String("sample-end", "now", `sample data end date (YYYY-MM-DD; or: now, yesterday, today, tomorrow, "-3 months")`)
	sf.RandomSeed = fs.Int64("random-seed", 0, "seed for the random number generator (default: time-based, non-deterministic)")
	fs.Var(&sf.TypicalEngineers, "typical-engineers", "comma-separated list of the team's typical engineers to build the sample pool from (default: all)")
	fs.Var(&sf.Team, "team", "comma-separated list of specific engineer names to model individually")
	return sf
}
