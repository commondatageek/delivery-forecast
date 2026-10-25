package web

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// DTO rules: dates are "YYYY-MM-DD" strings, names are snake_case, NaN is
// null, and no JSON tags are added to app or analysis types.

func date(t time.Time) string { return t.Format("2006-01-02") }

func dateOrNull(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := date(t)
	return &s
}

// num maps NaN and infinities to null.
func num(f float64) *float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}

// runDTO is shared by the sim results. Seed is a string because a time-based
// seed exceeds JavaScript's exact integer range.
type runDTO struct {
	ModeLabel   string `json:"mode_label"`
	Seed        string `json:"seed"`
	SampleStart string `json:"sample_start"`
	SampleEnd   string `json:"sample_end"`
	PoolIssues  int    `json:"pool_issues"`
}

func newRunDTO(r app.SimRun) runDTO {
	return runDTO{r.ModeLabel, strconv.FormatInt(r.Seed, 10), date(r.SampleStart), date(r.SampleEnd), len(r.PoolIssues)}
}

type histBin struct {
	Value int `json:"value"`
	Count int `json:"count"`
}

// histogram folds a sorted distribution into one bin per distinct value.
func histogram(sorted []int) []histBin {
	bins := []histBin{}
	for _, v := range sorted {
		if n := len(bins); n > 0 && bins[n-1].Value == v {
			bins[n-1].Count++
		} else {
			bins = append(bins, histBin{v, 1})
		}
	}
	return bins
}

type itemsRowDTO struct {
	Confidence int `json:"confidence"`
	Items      int `json:"items"`
}

type itemsDTO struct {
	Run           runDTO        `json:"run"`
	TargetStart   string        `json:"target_start"`
	TargetEnd     string        `json:"target_end"`
	EffectiveDays int           `json:"effective_days"`
	Confidences   []int         `json:"confidences"`
	Rows          []itemsRowDTO `json:"rows"`
	Histogram     []histBin     `json:"histogram"`
}

func newItemsDTO(r *app.ItemsResult) itemsDTO {
	d := itemsDTO{
		Run: newRunDTO(r.Run), TargetStart: date(r.TargetStart), TargetEnd: date(r.TargetEnd),
		EffectiveDays: r.EffectiveDays, Confidences: r.Confidences, Rows: []itemsRowDTO{},
		Histogram: histogram(r.Distribution),
	}
	for _, row := range r.Rows {
		d.Rows = append(d.Rows, itemsRowDTO{row.Confidence, row.Items})
	}
	return d
}

type daysRowDTO struct {
	Confidence int    `json:"confidence"`
	Days       int    `json:"days"`
	Date       string `json:"date"`
}

type cellDTO struct {
	Confidence     int    `json:"confidence"`
	MarginalDays   int    `json:"marginal_days"`
	CumulativeDays int    `json:"cumulative_days"`
	Date           string `json:"date"`
}

type groupDTO struct {
	Items int       `json:"items"`
	Cells []cellDTO `json:"cells"`
}

type daysDTO struct {
	Run         runDTO       `json:"run"`
	TargetStart string       `json:"target_start"`
	Confidences []int        `json:"confidences"`
	Items       []int        `json:"items"`
	Single      []daysRowDTO `json:"single"`
	Histogram   []histBin    `json:"histogram"`
	Groups      []groupDTO   `json:"groups"`
	Total       *groupDTO    `json:"total"`
}

func newCells(cells []app.TrajectoryCell) []cellDTO {
	out := []cellDTO{}
	for _, c := range cells {
		out = append(out, cellDTO{c.Confidence, c.MarginalDays, c.CumulativeDays, date(c.Date)})
	}
	return out
}

func newDaysDTO(r *app.DaysResult, items []int) daysDTO {
	d := daysDTO{Run: newRunDTO(r.Run), TargetStart: date(r.TargetStart), Confidences: r.Confidences, Items: items}
	if len(items) > 1 {
		d.Groups = []groupDTO{}
		for _, g := range r.Groups {
			d.Groups = append(d.Groups, groupDTO{g.Items, newCells(g.Cells)})
		}
		if r.Total != nil {
			d.Total = &groupDTO{r.Total.Items, newCells(r.Total.Cells)}
		}
		return d
	}
	d.Single = []daysRowDTO{}
	for _, row := range r.Single {
		d.Single = append(d.Single, daysRowDTO{row.Confidence, row.Days, date(row.Date)})
	}
	d.Histogram = histogram(r.Distribution)
	return d
}

type probRowDTO struct {
	Items       int      `json:"items"`
	Probability *float64 `json:"probability"`
}

type probabilityDTO struct {
	Run           runDTO       `json:"run"`
	TargetStart   string       `json:"target_start"`
	TargetEnd     string       `json:"target_end"`
	EffectiveDays int          `json:"effective_days"`
	Items         int          `json:"items"`
	Probability   *float64     `json:"probability"`
	Table         []probRowDTO `json:"table"`
	Histogram     []histBin    `json:"histogram"`
}

func newProbabilityDTO(r *app.ProbabilityResult) probabilityDTO {
	d := probabilityDTO{
		Run: newRunDTO(r.Run), TargetStart: date(r.TargetStart), TargetEnd: date(r.TargetEnd),
		EffectiveDays: r.EffectiveDays, Items: r.Items, Histogram: histogram(r.Distribution),
	}
	if r.Items >= 0 {
		d.Probability = num(r.Probability)
	} else {
		d.Table = []probRowDTO{}
		for _, row := range r.Table {
			d.Table = append(d.Table, probRowDTO{row.Items, num(row.Probability)})
		}
	}
	return d
}

type backtestRowDTO struct {
	Date        string   `json:"date"`
	Completed   int      `json:"completed"`
	Remaining   int      `json:"remaining"`
	Probability *float64 `json:"probability"`
	Projected   bool     `json:"projected"`
}

type backtestDTO struct {
	Run                 runDTO           `json:"run"`
	Scope               string           `json:"scope"`
	IssueCount          int              `json:"issue_count"`
	ReplayStart         string           `json:"replay_start"`
	ReplayStartInferred bool             `json:"replay_start_inferred"`
	Today               string           `json:"today"`
	Rows                []backtestRowDTO `json:"rows"`
}

func newBacktestDTO(r *app.BacktestResult, today time.Time) backtestDTO {
	d := backtestDTO{
		Run: newRunDTO(r.Run), Scope: r.Scope, IssueCount: r.IssueCount,
		ReplayStart: date(r.ReplayStart), ReplayStartInferred: r.ReplayStartInferred,
		Today: date(today), Rows: []backtestRowDTO{},
	}
	for _, row := range r.Rows {
		d.Rows = append(d.Rows, backtestRowDTO{date(row.Date), row.Completed, row.Remaining, num(row.Prob), row.Projected})
	}
	return d
}

// reportDTO embeds a renderer's own JSON output verbatim.
type reportDTO struct {
	Report json.RawMessage `json:"report"`
	Start  string          `json:"start,omitempty"`
	End    string          `json:"end,omitempty"`
}

// rawReport renders with write and returns the bytes as a RawMessage.
func rawReport(write func(w io.Writer) error) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := write(&buf); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSpace(buf.Bytes())), nil
}

type historyDTO struct {
	Report        json.RawMessage `json:"report"`
	Start         string          `json:"start"`
	StartInferred bool            `json:"start_inferred"`
}

type countMilestoneDTO struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type countProjectDTO struct {
	Name           string              `json:"name"`
	Team           string              `json:"team"`
	Total          int                 `json:"total"`
	MilestoneCount int                 `json:"milestone_count"`
	LastUpdated    *string             `json:"last_updated"`
	Milestones     []countMilestoneDTO `json:"milestones"`
}

type countDTO struct {
	Total     int               `json:"total"`
	ShowTeams bool              `json:"show_teams"`
	Projects  []countProjectDTO `json:"projects"`
}

func newCountDTO(r *app.CountResult) countDTO {
	d := countDTO{Total: r.Total, ShowTeams: r.ShowTeams, Projects: []countProjectDTO{}}
	for _, p := range r.Projects {
		pd := countProjectDTO{
			Name: p.Name, Team: p.TeamName, Total: p.Total, MilestoneCount: p.MilestoneCount(),
			LastUpdated: dateOrNull(p.LastUpdated), Milestones: []countMilestoneDTO{},
		}
		for _, m := range p.Milestones {
			pd.Milestones = append(pd.Milestones, countMilestoneDTO{m.Name, m.Count})
		}
		d.Projects = append(d.Projects, pd)
	}
	return d
}

type readinessDTO struct {
	Command  string `json:"command"`
	Excluded int    `json:"excluded"`
	Status   string `json:"status"`
}

type engineerEntriesDTO struct {
	Name    string `json:"name"`
	Entries int    `json:"entries"`
}

type exclusionsDTO struct {
	Path           string               `json:"path"`
	Invalid        *string              `json:"invalid"`
	GlobalEntries  int                  `json:"global_entries"`
	Engineers      []engineerEntriesDTO `json:"engineers"`
	HasDates       bool                 `json:"has_dates"`
	First          *string              `json:"first"`
	Last           *string              `json:"last"`
	Past           int                  `json:"past"`
	Future         int                  `json:"future"`
	Today          *string              `json:"today"`
	UnmatchedNames []string             `json:"unmatched_names"`
}

type checkDTO struct {
	Input      string         `json:"input"`
	Issues     int            `json:"issues"`
	Readiness  []readinessDTO `json:"readiness"`
	Exclusions *exclusionsDTO `json:"exclusions"`
}

func newExclusionsDTO(path string, r app.ExclusionsReport) *exclusionsDTO {
	d := &exclusionsDTO{Path: path, Engineers: []engineerEntriesDTO{}, UnmatchedNames: []string{}}
	if r.ParseErr != nil {
		msg := r.ParseErr.Error()
		d.Invalid = &msg
		return d
	}
	d.GlobalEntries = r.GlobalEntries
	for _, e := range r.Engineers {
		d.Engineers = append(d.Engineers, engineerEntriesDTO{e.Name, e.Entries})
	}
	d.HasDates = r.HasDates
	d.First, d.Last, d.Today = dateOrNull(r.First), dateOrNull(r.Last), dateOrNull(r.Today)
	d.Past, d.Future = r.Past, r.Future
	if r.UnmatchedNames != nil {
		d.UnmatchedNames = r.UnmatchedNames
	}
	return d
}

func newCheckDTO(input string, n int, ready []app.CheckResult, exc *exclusionsDTO) checkDTO {
	d := checkDTO{Input: input, Issues: n, Readiness: []readinessDTO{}, Exclusions: exc}
	for _, r := range ready {
		d.Readiness = append(d.Readiness, readinessDTO{r.Command, r.Excluded, r.Status})
	}
	return d
}
