package params

import (
	"flag"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/linear"
)

// Resolved is what a command's Resolve produces: the input path to load, any
// warnings to show before anything else, and the validated request.
// Warnings are valid even when Resolve also returns an error.
type Resolved[R any] struct {
	InputPath string
	Warnings  []string
	Request   R
}

// IsSet reports whether flag name was explicitly set on fs: on the command
// line, through -config (util.ApplyConfig uses fs.Set), or by the web server
// (which also uses fs.Set).
func IsSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// ResolveSeed returns seed if -random-seed was explicitly set, otherwise a
// time-based seed so runs are non-deterministic by default.
func ResolveSeed(fs *flag.FlagSet, seed int64, now time.Time) int64 {
	if IsSet(fs, "random-seed") {
		return seed
	}
	return now.UnixNano()
}

// AddConfig registers the -config flag used by every subcommand.
func AddConfig(fs *flag.FlagSet) *string {
	return fs.String("config", "", "path to a YAML config file supplying flag values (CLI flags override)")
}

// AddTeams registers the -teams flag. usage is passed in whole (not just
// an example) because its wording differs meaningfully between commands that
// filter to a team set (aging/cfd/count) and linear sync, where -teams
// extends the candidate set rather than filtering.
func AddTeams(fs *flag.FlagSet, usage string) *linear.TeamKeyList {
	var teams linear.TeamKeyList
	fs.Var(&teams, "teams", usage)
	return &teams
}

// FlagRecords snapshots every flag in fs — the command-unique ones too
// (-days, -items, -percentile, -manifest) — with its default, current value
// and whether it was explicitly set (on the command line or via -config).
func FlagRecords(fs *flag.FlagSet) []app.FlagRecord {
	var flags []app.FlagRecord
	fs.VisitAll(func(f *flag.Flag) {
		flags = append(flags, app.FlagRecord{
			Name:    f.Name,
			Value:   f.Value.String(),
			Default: f.DefValue,
			Set:     IsSet(fs, f.Name),
		})
	})
	return flags
}
