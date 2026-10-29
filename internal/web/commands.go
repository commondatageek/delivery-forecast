package web

import (
	"flag"
	"io"
	"net/http"
	"time"
)

// commandDef is one CLI command exposed through the web API.
type commandDef struct {
	ID       string   // URL segment, e.g. "sim-items"
	Name     string   // CLI name, e.g. "sim items"
	Summary  string   // identical to the CLI's command-table Summary
	Group    string   // "forecast", "flow" or "data"
	Title    string   // page title
	Exports  []string // formats, first = the one shown as "Text output"
	Stream   bool     // reports progress events
	newFlags func(fs *flag.FlagSet, now time.Time) any
	prepare  func(rc *runContext, bundle any) (*runner, error)
}

// registry is the navigation order.
var registry = []*commandDef{
	{ID: "sim-days", Name: "sim days", Summary: "How many days for N engineers to complete I items?", Group: "forecast", Title: "When will it be done?", Exports: []string{"text", "json", "manifest"}, Stream: true, newFlags: newSimDaysFlags, prepare: prepareSimDays},
	{ID: "sim-items", Name: "sim items", Summary: "How many items can N engineers complete in D days?", Group: "forecast", Title: "How much can we finish?", Exports: []string{"text", "json", "manifest"}, Stream: true, newFlags: newSimItemsFlags, prepare: prepareSimItems},
	{ID: "sim-probability", Name: "sim probability", Summary: "What is the probability of completing I items in D days?", Group: "forecast", Title: "What are the odds?", Exports: []string{"text", "json", "manifest"}, Stream: true, newFlags: newSimProbabilityFlags, prepare: prepareSimProbability},
	{ID: "sim-backtest", Name: "sim backtest", Summary: "Replay probability forecasts day-by-day for a project/milestone.", Group: "forecast", Title: "How did past forecasts hold up?", Exports: []string{"text", "csv", "json"}, Stream: true, newFlags: newSimBacktestFlags, prepare: prepareSimBacktest},
	{ID: "history", Name: "history", Summary: "Per-day history of flow metrics for a project or team.", Group: "flow", Title: "Day-by-day history", Exports: []string{"text", "csv", "json"}, newFlags: newHistoryFlags, prepare: prepareHistory},
	{ID: "cfd", Name: "cfd", Summary: "Cumulative flow diagram.", Group: "flow", Title: "Cumulative flow", Exports: []string{"html", "json"}, newFlags: newCFDFlags, prepare: prepareCFD},
	{ID: "aging", Name: "aging", Summary: "WIP-age and cycle-time report.", Group: "flow", Title: "What's aging in progress?", Exports: []string{"text", "html", "json"}, newFlags: newAgingFlags, prepare: prepareAging},
	{ID: "count", Name: "count", Summary: "Count of non-terminal issues, grouped by project.", Group: "flow", Title: "What's still open?", Exports: []string{"text", "json"}, newFlags: newCountFlags, prepare: prepareCount},
	{ID: "check", Name: "check", Summary: "Validate a source file/db and report per-command data readiness.", Group: "data", Title: "Check my data", Exports: []string{"text", "json"}, newFlags: newCheckFlags, prepare: prepareCheck},
	{ID: "import", Name: "import", Summary: "Convert a tracker export into a forecast data source using a mapping file.", Group: "data", Title: "Import a tracker export", Exports: []string{"text", "json"}, newFlags: newImportFlags, prepare: prepareImport},
	{ID: "linear-sync", Name: "linear sync", Summary: "Sync issues from Linear into the database.", Group: "data", Title: "Sync from Linear", Stream: true, newFlags: newLinearSyncFlags, prepare: prepareLinearSync},
	{ID: "linear-teams", Name: "linear teams", Summary: "List accessible Linear teams.", Group: "data", Title: "Linear teams", Exports: []string{"json"}, newFlags: newLinearTeamsFlags, prepare: prepareLinearTeams},
}

func commandByID(id string) *commandDef {
	for _, d := range registry {
		if d.ID == id {
			return d
		}
	}
	return nil
}

// CommandSummaries maps each registered command's CLI name to its summary,
// so tests can check them against the CLI's command table.
func CommandSummaries() map[string]string {
	m := map[string]string{}
	for _, d := range registry {
		m[d.Name] = d.Summary
	}
	return m
}

// newDiscardFlagSet returns a FlagSet that reports errors to the caller only.
func newDiscardFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

type fieldDTO struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	Section string   `json:"section"`
	Usage   string   `json:"usage"`
	Default string   `json:"default"`
	Bool    bool     `json:"bool"`
	Choices []string `json:"choices"`
}

type commandDTO struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Summary string     `json:"summary"`
	Group   string     `json:"group"`
	Title   string     `json:"title"`
	Exports []string   `json:"exports"`
	Stream  bool       `json:"stream"`
	Fields  []fieldDTO `json:"fields"`
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	now := s.opts.Now()
	cmds := make([]commandDTO, 0, len(registry))
	for _, d := range registry {
		fs := newDiscardFlagSet(d.Name)
		d.newFlags(fs, now)
		fields := []fieldDTO{}
		fs.VisitAll(func(f *flag.Flag) {
			m, ok := metaFor(d.ID, f.Name)
			if !ok || m.Hidden != "" {
				return
			}
			choices := m.Choices
			if choices == nil {
				choices = []string{}
			}
			fields = append(fields, fieldDTO{
				Name: f.Name, Label: m.Label, Kind: m.Kind, Section: m.Section,
				Usage: f.Usage, Default: f.DefValue, Bool: isBool(f), Choices: choices,
			})
		})
		cmds = append(cmds, commandDTO{d.ID, d.Name, d.Summary, d.Group, d.Title, d.Exports, d.Stream, fields})
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": cmds})
}

func isBool(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}
