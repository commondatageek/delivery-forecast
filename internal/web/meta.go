package web

// fieldMeta is the web UI's decision about one flag.
type fieldMeta struct {
	Hidden  string // non-empty = CLI-only; the reason (shown nowhere, enforced by tests)
	Kind    string // widget kind
	Section string // "data", "basic" or "advanced"
	Label   string
	Choices []string // Kind "choice" only
}

func hidden(reason string) fieldMeta { return fieldMeta{Hidden: reason} }

func meta(kind, section, label string) fieldMeta {
	return fieldMeta{Kind: kind, Section: section, Label: label}
}

// metaByFlag applies to every command; metaOverrides refine it per command.
var metaByFlag = map[string]fieldMeta{
	"input":             meta("input-path", "data", "Data source"),
	"db":                hidden("deprecated alias of -input"),
	"stdin-format":      hidden("no stdin in the web UI"),
	"config":            hidden("replaced by config import/export"),
	"format":            hidden("every format is a download"),
	"out":               hidden("downloads replace -out"),
	"manifest":          hidden("Download manifest replaces -manifest"),
	"exclusions":        meta("exclusions-path", "data", "Exclusions file"),
	"engineers":         meta("engineers", "basic", "Engineers (count or names)"),
	"whole-team":        meta("bool", "basic", "Whole team"),
	"sample-start":      meta("date-start", "basic", "Sample from"),
	"sample-end":        meta("date-end", "basic", "Sample to"),
	"simulations":       meta("int", "advanced", "Simulations"),
	"goroutines":        meta("int", "advanced", "Worker goroutines"),
	"random-seed":       meta("int", "advanced", "Random seed"),
	"typical-engineers": meta("assignee-list", "advanced", "Typical engineers"),
	"days":              meta("int", "basic", "Days"),
	"target-start-date": meta("date-start", "basic", "Forecast starts"),
	"target-end-date":   meta("date-end", "basic", "Forecast ends (inclusive)"),
	"confidence":        meta("int-list", "basic", "Confidence levels (%)"),
	"items":             meta("int", "basic", "Items"),
	"project":           meta("project", "basic", "Project"),
	"milestone":         meta("milestone", "basic", "Milestone"),
	"replay-start-date": meta("date-start", "basic", "Replay from"),
	"min-cycle-time":    meta("duration", "advanced", "Minimum cycle time"),
	"percentile":        meta("int", "basic", "Percentile"),
	"show-completed":    meta("bool", "basic", "Show completed sample"),
	"teams":             meta("team-list", "basic", "Teams"),
	"start":             meta("date-start", "basic", "Start"),
	"end":               meta("date-end", "basic", "End"),
	"updated-since":     meta("date-end", "basic", "Updated since"),
	"milestones":        meta("bool", "basic", "Per-milestone breakdown"),
	"window":            meta("int", "basic", "Rolling window (days)"),
	"all-teams":         meta("bool", "basic", "All accessible teams"),
	"full-reload":       meta("bool", "basic", "Full reload"),
}

// metaOverrides maps command ID → flag → meta.
var metaOverrides = map[string]map[string]fieldMeta{
	"sim-days":    {"items": meta("int-list", "basic", "Items (comma-separated for a grouped trajectory)")},
	"linear-sync": {"db": meta("db-path", "data", "Database file"), "teams": meta("string", "basic", "Teams")},
}

// metaFor returns the UI decision for flag name of command id, and whether
// one exists.
func metaFor(id, name string) (fieldMeta, bool) {
	if m, ok := metaOverrides[id][name]; ok {
		return m, true
	}
	m, ok := metaByFlag[name]
	return m, ok
}

func isPathKind(kind string) bool {
	return kind == "input-path" || kind == "exclusions-path" || kind == "db-path"
}
