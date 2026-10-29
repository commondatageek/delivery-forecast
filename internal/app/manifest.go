package app

import (
	"sort"
	"time"

	"github.com/commondatageek/delivery-forecast/simulate"
)

// Manifest is a single JSON document recording everything that fed a
// simulation run, so two disagreeing runs can be diffed: the binary's git
// SHA, a fingerprint of the input file, every flag (set vs. defaulted), the
// resolved sampling mode/seed/window, the built sample pool, and a full dump
// of the issues that fed it.
type Manifest struct {
	SchemaVersion int           `json:"schema_version"`
	GeneratedAt   string        `json:"generated_at"`
	Binary        BinaryInfo    `json:"binary"`
	Invocation    Invocation    `json:"invocation"`
	Flags         []FlagRecord  `json:"flags"`
	Resolved      Resolved      `json:"resolved"`
	Data          DataSection   `json:"data"`
	Pool          PoolSection   `json:"pool"`
	Issues        IssuesSection `json:"issues"`
}

type BinaryInfo struct {
	GitSHA          string `json:"git_sha"`
	GitTime         string `json:"git_time"`
	Dirty           bool   `json:"dirty"`
	GoVersion       string `json:"go_version"`
	Module          string `json:"module"`
	VersionOverride string `json:"version_override,omitempty"`
}

type Invocation struct {
	Subcommand string   `json:"subcommand"`
	Args       []string `json:"args"`
	WorkingDir string   `json:"working_dir"`
}

type FlagRecord struct {
	Name    string `json:"name"`
	Default string `json:"default"`
	Value   string `json:"value"`
	Set     bool   `json:"set"`
}

type Resolved struct {
	Mode             string         `json:"mode"`
	ModeLabel        string         `json:"mode_label"`
	Engineers        int            `json:"engineers"`
	EngineerNames    []string       `json:"engineer_names,omitempty"`
	TypicalEngineers []string       `json:"typical_engineers"`
	WholeTeam        bool           `json:"whole_team"`
	Seed             int64          `json:"seed"`
	SampleStart      string         `json:"sample_start"`
	SampleEnd        string         `json:"sample_end"`
	TotalDays        int            `json:"total_days"`
	Extra            map[string]any `json:"extra,omitempty"`
}

type DataSection struct {
	Input      DataFile          `json:"input"`
	Exclusions ExclusionsSection `json:"exclusions"`
}

// ExclusionsSection records both what the exclusions file said and what it
// resolved to on each side of the forecast, so two runs that disagree can be
// told apart without re-reading the file.
type ExclusionsSection struct {
	Path              string              `json:"path"`                // "" when none
	Source            simulate.Exclusions `json:"source"`              // entries marshal as {"from","to","reason"}
	SampleDaysDropped DaysByScope         `json:"sample_days_dropped"` // within [sample-start, sample-end)
	HorizonDaysOff    DaysByScope         `json:"horizon_days_off"`    // every excluded date at/after target start
}

// DaysByScope lists resolved dates (YYYY-MM-DD) per scope. Engineer lists
// include the global dates, since that is when the engineer is actually off.
type DaysByScope struct {
	Global    []string            `json:"global"`
	Engineers map[string][]string `json:"engineers,omitempty"`
}

type DataFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"modtime"`
	SHA256  string `json:"sha256"`
	Error   string `json:"error,omitempty"`
}

type PoolSection struct {
	EngineerCount          int            `json:"engineer_count"`
	PerEngineerCompletions map[string]int `json:"per_engineer_completions"`
	PerEngineerSampleDays  map[string]int `json:"per_engineer_sample_days"`
	TotalSampleDays        int            `json:"total_sample_days"`
	TotalCompletions       int            `json:"total_completions"`
	CombinedSampleCount    int            `json:"combined_sample_count"`
}

type IssuesSection struct {
	QueriedCount int           `json:"queried_count"`
	SkippedCount int           `json:"skipped_count"`
	Records      []IssueRecord `json:"records"`
}

// IssueRecord is the subset of issues.Issue that identifies a pool issue and
// explains why it is in the pool (team key, project id, milestone, etc. are
// intentionally omitted).
type IssueRecord struct {
	Source      string `json:"source"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Assignee    string `json:"assignee"`
	TeamName    string `json:"team_name"`
	ProjectName string `json:"project_name"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func modeName(m simulate.Mode) string {
	switch m {
	case simulate.ModeFullTeam:
		return "whole_team"
	default:
		return "anonymous"
	}
}

func issueTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// resolveDays lists, per scope in exc, the dates cal marks non-working in
// [from, to) (to < 0: every explicit date from `from` on). Per-engineer scopes
// are skipped in whole-team mode, which ignores them on both sides.
func resolveDays(cal *simulate.Calendar, exc simulate.Exclusions, wholeTeam bool, from, to int) DaysByScope {
	out := DaysByScope{Global: formatDays(cal.OffDays("", from, to))}
	if wholeTeam {
		return out
	}
	for _, name := range exc.Scopes() {
		if out.Engineers == nil {
			out.Engineers = make(map[string][]string)
		}
		out.Engineers[name] = formatDays(cal.OffDays(name, from, to))
	}
	return out
}

// formatDays renders dates as YYYY-MM-DD, as a non-nil slice so an empty list
// marshals as [] rather than null.
func formatDays(days []time.Time) []string {
	out := make([]string, len(days))
	for i, d := range days {
		out[i] = d.Format("2006-01-02")
	}
	return out
}

// ManifestInputs is the single shared parameter bag the sim subcommands
// fill in to produce a Manifest.
type ManifestInputs struct {
	Subcommand       string
	Run              SimRun
	TypicalEngineers []string
	Engineers        int
	EngineerNames    []string
	WholeTeam        bool
	ExclusionsPath   string
	Extra            map[string]any
	// Supplied by the caller because they need IO, the flag package, or the
	// clock, none of which internal/app may touch:
	Flags       []FlagRecord
	Invocation  Invocation
	Binary      BinaryInfo
	Input       DataFile
	GeneratedAt time.Time
}

// NewManifest assembles a Manifest. Pure: same inputs, same output.
func NewManifest(in ManifestInputs) *Manifest {
	run := in.Run

	// Pool summary: direct iteration works for both per-engineer and
	// whole-team modes (the latter has the single WholeTeamKey key).
	perComp := make(map[string]int, len(run.Pool.PerEngineer))
	perDays := make(map[string]int, len(run.Pool.PerEngineer))
	totalComp, totalDays := 0, 0
	for name, samples := range run.Pool.PerEngineer {
		s := simulate.Sum(samples)
		perComp[name] = s
		perDays[name] = len(samples)
		totalComp += s
		totalDays += len(samples)
	}

	records := make([]IssueRecord, 0, len(run.PoolIssues))
	for _, it := range run.PoolIssues {
		records = append(records, IssueRecord{
			Source:      it.Source,
			Identifier:  it.Identifier,
			Title:       it.Title,
			Assignee:    it.Assignee,
			TeamName:    it.TeamName,
			ProjectName: it.ProjectName,
			Status:      string(it.Status),
			StartedAt:   issueTime(it.StartedAt),
			CompletedAt: issueTime(it.CompletedAt),
			UpdatedAt:   issueTime(it.UpdatedAt),
		})
	}
	// Sort by (source, identifier) so two manifests' issue lists diff cleanly
	// regardless of the source's row order.
	sort.Slice(records, func(i, j int) bool {
		if records[i].Source != records[j].Source {
			return records[i].Source < records[j].Source
		}
		return records[i].Identifier < records[j].Identifier
	})

	return &Manifest{
		SchemaVersion: 3,
		GeneratedAt:   in.GeneratedAt.UTC().Format(time.RFC3339),
		Binary:        in.Binary,
		Invocation:    in.Invocation,
		Flags:         in.Flags,
		Resolved: Resolved{
			Mode:             modeName(run.Mode),
			ModeLabel:        simulate.ModeLabel(run.Mode, in.Engineers, in.EngineerNames),
			Engineers:        in.Engineers,
			EngineerNames:    in.EngineerNames,
			TypicalEngineers: in.TypicalEngineers,
			WholeTeam:        in.WholeTeam,
			Seed:             run.Seed,
			SampleStart:      run.SampleStart.UTC().Format(time.RFC3339),
			SampleEnd:        run.SampleEnd.UTC().Format(time.RFC3339),
			TotalDays:        simulate.DaysBetween(run.SampleStart, run.SampleEnd),
			Extra:            in.Extra,
		},
		Data: DataSection{
			Input: in.Input,
			Exclusions: ExclusionsSection{
				Path:              in.ExclusionsPath,
				Source:            run.Exclusions,
				SampleDaysDropped: resolveDays(run.SampleCalendar, run.Exclusions, in.WholeTeam, 0, simulate.DaysBetween(run.SampleStart, run.SampleEnd)),
				HorizonDaysOff:    resolveDays(run.HorizonCalendar, run.Exclusions, in.WholeTeam, 0, -1),
			},
		},
		Pool: PoolSection{
			EngineerCount:          len(run.Pool.PerEngineer),
			PerEngineerCompletions: perComp,
			PerEngineerSampleDays:  perDays,
			TotalSampleDays:        totalDays,
			TotalCompletions:       totalComp,
			CombinedSampleCount:    len(run.Pool.Combined),
		},
		Issues: IssuesSection{
			QueriedCount: len(run.PoolIssues),
			SkippedCount: run.Skipped,
			Records:      records,
		},
	}
}
