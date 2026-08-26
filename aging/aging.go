package aging

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
)

// Options controls which issues feed the cycle-time distribution and the
// in-progress ranking. Every field mirrors a `forecast aging` flag (and thus a
// `-config` YAML key of the same name).
type Options struct {
	// Teams is the `-teams` flag: team keys to filter by; empty means all teams.
	Teams []string
	// SampleStart is the `-sample-start` flag, resolved to a concrete date
	// (default: today minus 3 months).
	SampleStart time.Time
	// SampleEnd is the `-sample-end` flag, resolved to a concrete date
	// (default: today).
	SampleEnd time.Time
	// MinCycleTime is the `-min-cycle-time` flag, resolved from its duration
	// string (e.g. "5m", "1h", "1d"); zero means no floor.
	MinCycleTime time.Duration
	// Percentile is the `-percentile` flag: which percentile of the cycle-time
	// distribution the report is anchored to (default 85).
	Percentile int
}

// Issue is the neutral input record for aging analysis: just the fields
// CycleTimes/InProgressItems/CompletedItems read. The caller maps its
// source's fields onto it (the CLI maps issues.Issue).
type Issue struct {
	Identifier  string
	Title       string
	Assignee    string
	ProjectName string
	StateType   string
	StateName   string
	StartedAt   time.Time
	CompletedAt time.Time
}

// Item is an in-progress issue annotated with its age and percentile rank
// against the historical cycle-time distribution.
type Item struct {
	Identifier  string
	Title       string
	Assignee    string
	ProjectName string
	StateType   string
	StateName   string
	StartedAt   time.Time
	AgeDays     float64
	Percentile  int
	// Multiplier is AgeDays expressed as a multiple of the report's
	// percentile threshold: 1.0 means exactly the threshold, >1.0 older,
	// <1.0 younger. It is 0 when the threshold is 0 (empty or degenerate
	// distribution); renderers check Meta.Threshold before displaying it.
	Multiplier float64
}

// Meta carries the report-level context the renderers need alongside the
// items themselves: which percentile the report is anchored to, the
// cycle-time value at that percentile, the window the distribution was
// drawn from, and how many completed issues went into it.
type Meta struct {
	// Percentile is the `-percentile` flag (1–100).
	Percentile int
	// Threshold is the cycle time in days at Percentile. Zero means the
	// distribution was empty or degenerate: renderers must not present a
	// multiplier in that case.
	Threshold float64
	// SampleStart and SampleEnd are the completed-issue window, for display.
	SampleStart time.Time
	SampleEnd   time.Time
	// CompletedCount is len(cycleTimes) — the size of the distribution.
	CompletedCount int
}

// Label renders the percentile as it appears in headers, e.g. "P85".
func (m Meta) Label() string { return fmt.Sprintf("P%d", m.Percentile) }

// HasThreshold reports whether a multiplier can be shown at all.
func (m Meta) HasThreshold() bool { return m.Threshold > 0 }

// CycleTimes extracts cycle times (started_at → completed_at) from completed
// issues, excluding issues missing either timestamp and those whose cycle time
// is below minCycleTime. The returned slice is unsorted.
func CycleTimes(issues []Issue, minCycleTime time.Duration) []float64 {
	var out []float64
	for _, it := range issues {
		if it.StartedAt.IsZero() || it.CompletedAt.IsZero() {
			continue
		}
		ct := it.CompletedAt.Sub(it.StartedAt)
		if ct < minCycleTime {
			continue
		}
		days := ct.Hours() / 24
		if days >= 0 {
			out = append(out, days)
		}
	}
	return out
}

// InProgressItems converts in-progress issues into Items with AgeDays computed
// relative to today. Issues missing StartedAt are skipped. AgeDays is floored
// at zero.
func InProgressItems(issues []Issue, today time.Time) []Item {
	var out []Item
	for _, it := range issues {
		if it.StartedAt.IsZero() {
			continue
		}
		ageDays := today.Sub(it.StartedAt).Hours() / 24
		if ageDays < 0 {
			ageDays = 0
		}
		out = append(out, Item{
			Identifier:  it.Identifier,
			Title:       it.Title,
			Assignee:    it.Assignee,
			ProjectName: it.ProjectName,
			StateType:   it.StateType,
			StateName:   it.StateName,
			StartedAt:   it.StartedAt,
			AgeDays:     ageDays,
		})
	}
	return out
}

// CompletedItems converts completed issues into Items with AgeDays set to
// their cycle time (started_at → completed_at) in days, applying the same
// filter as CycleTimes (missing timestamps, or cycle time below
// minCycleTime, are skipped). The result is the exact set of issues backing
// the percentile distribution, each annotated once ranked via RankItems.
func CompletedItems(issues []Issue, minCycleTime time.Duration) []Item {
	var out []Item
	for _, it := range issues {
		if it.StartedAt.IsZero() || it.CompletedAt.IsZero() {
			continue
		}
		ct := it.CompletedAt.Sub(it.StartedAt)
		if ct < minCycleTime {
			continue
		}
		days := ct.Hours() / 24
		if days < 0 {
			continue
		}
		out = append(out, Item{
			Identifier:  it.Identifier,
			Title:       it.Title,
			Assignee:    it.Assignee,
			ProjectName: it.ProjectName,
			StateType:   it.StateType,
			StateName:   it.StateName,
			StartedAt:   it.StartedAt,
			AgeDays:     days,
		})
	}
	return out
}

// RankItems sets the Percentile and Multiplier fields on each item based
// on its AgeDays: Percentile is its cumulative rank in the sorted
// cycle-time distribution, Multiplier is its age as a multiple of
// threshold (the distribution's value at the report's chosen percentile).
// A threshold of 0 leaves Multiplier at 0 rather than dividing.
func RankItems(items []Item, sortedCycleTimes []float64, threshold float64) {
	for i := range items {
		items[i].Percentile = util.ComputePercentile(sortedCycleTimes, items[i].AgeDays)
		if threshold > 0 {
			items[i].Multiplier = items[i].AgeDays / threshold
		}
	}
}

func formatStartDate(t time.Time) string {
	return t.Local().Format("Mon 1/2")
}

func formatState(name, typ string) string {
	switch {
	case name != "" && typ != "":
		return fmt.Sprintf("%s (%s)", name, typ)
	case name != "":
		return name
	default:
		return typ
	}
}

func truncateTitle(title string) string {
	const maxLen = 50
	if len(title) <= maxLen {
		return title
	}
	return title[:maxLen] + "..."
}

// ageClass buckets an item for display by how its age compares to the
// report's percentile threshold: at or past the threshold is "high",
// within 15% of it is "medium". Keying this on the multiplier rather than
// on the percentile rank keeps a cell's color and the number printed in it
// from ever disagreeing — the two are computed by different methods
// (cumulative rank vs. nearest rank) and contradict each other on roughly
// half a percent of rows, more on small distributions. With no usable
// threshold there is nothing to compare against, so nothing is colored.
func ageClass(mult float64, hasThreshold bool) string {
	switch {
	case !hasThreshold:
		return "normal"
	case mult >= 1.0:
		return "high"
	case mult >= 0.85:
		return "medium"
	default:
		return "normal"
	}
}

var textColumns = []string{
	"IDENTIFIER", "TITLE", "DAYS", "PERCENTILE", "MULTIPLIER",
	"STATE", "START DATE", "ASSIGNEE",
}

var textHeader = strings.Join(textColumns, "\t")

// textDivider is tab-separated with the same cell count as textHeader so it
// stays within the same tabwriter column block as the rows around it —
// otherwise the two sections' columns would be sized independently and no
// longer line up.
var textDivider = func() string {
	dashes := make([]string, len(textColumns))
	for i, c := range textColumns {
		dashes[i] = strings.Repeat("-", len(c))
	}
	return strings.Join(dashes, "\t")
}()

// formatMultiplier renders an item's age as a multiple of the report's
// percentile threshold, e.g. "1.34x". An em dash stands in when the
// distribution has no usable threshold to divide by.
func formatMultiplier(m float64, meta Meta) string {
	if !meta.HasThreshold() {
		return "—"
	}
	return fmt.Sprintf("%.2fx", m)
}

func writeItemRow(tw *tabwriter.Writer, item Item, meta Meta) {
	pct := item.Percentile
	fmt.Fprintf(tw, "%s\t%s\t%.1f\t%d%s\t%s\t%s\t%s\t%s\n",
		item.Identifier,
		truncateTitle(item.Title),
		item.AgeDays,
		pct, util.OrdinalSuffix(pct),
		formatMultiplier(item.Multiplier, meta),
		formatState(item.StateName, item.StateType),
		formatStartDate(item.StartedAt),
		item.Assignee,
	)
}

// RenderText writes a tabular aging report to w: in-progress issues ranked by
// age/percentile/multiplier. When showCompleted is true, a second section
// follows — separated by a divider — listing the completed issues that make
// up the percentile distribution itself. Both tables share a single
// tabwriter column block so their columns stay horizontally aligned.
func RenderText(w io.Writer, items []Item, completed []Item, showCompleted bool, meta Meta) error {
	fmt.Fprintf(w, "Cycle time distribution: %d completed issues (%s to %s)  ·  %s: %.1f days\n\n",
		meta.CompletedCount,
		meta.SampleStart.Format("2006-01-02"),
		meta.SampleEnd.Format("2006-01-02"),
		meta.Label(),
		meta.Threshold,
	)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, textHeader)
	for _, item := range items {
		writeItemRow(tw, item, meta)
	}
	if showCompleted {
		fmt.Fprintln(tw, textDivider)
		fmt.Fprintln(tw, textHeader)
		for _, item := range completed {
			writeItemRow(tw, item, meta)
		}
	}
	return tw.Flush()
}

type jsonItem struct {
	Identifier  string   `json:"identifier"`
	Title       string   `json:"title"`
	Assignee    string   `json:"assignee"`
	ProjectName string   `json:"project_name,omitempty"`
	StateType   string   `json:"state_type,omitempty"`
	StateName   string   `json:"state_name,omitempty"`
	StartedAt   string   `json:"started_at"`
	AgeDays     float64  `json:"age_days"`
	Percentile  int      `json:"percentile"`
	Multiplier  *float64 `json:"multiplier"`
}

// RenderJSON writes the in-progress items as a JSON array to w. Multiplier is
// null when meta has no usable threshold to divide by (a genuinely-zero
// multiplier is a different fact from "no threshold").
func RenderJSON(w io.Writer, items []Item, meta Meta) error {
	out := make([]jsonItem, len(items))
	for i, item := range items {
		var mult *float64
		if meta.HasThreshold() {
			v := math.Round(item.Multiplier*100) / 100
			mult = &v
		}
		out[i] = jsonItem{
			Identifier:  item.Identifier,
			Title:       item.Title,
			Assignee:    item.Assignee,
			ProjectName: item.ProjectName,
			StateType:   item.StateType,
			StateName:   item.StateName,
			StartedAt:   item.StartedAt.Format("2006-01-02"),
			AgeDays:     math.Round(item.AgeDays*10) / 10,
			Percentile:  item.Percentile,
			Multiplier:  mult,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

const htmlTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>In-Progress Issue Age Report</title>
<style>
  *, *::before, *::after { box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 2rem auto; max-width: 1100px; color: #1a1a1a; background: #f5f5f5; }
  h1 { font-size: 1.35rem; font-weight: 600; margin: 0 0 0.4rem; }
  h2 { font-size: 1.05rem; font-weight: 600; margin: 0 0 0.6rem; }
  .meta { color: #555; font-size: 0.875rem; margin-bottom: 1.75rem; }
  .meta strong { color: #1a1a1a; }
  .divider { border: none; border-top: 2px dashed #ccc; margin: 2.5rem 0 1.75rem; }
  table { border-collapse: collapse; width: 100%; table-layout: fixed; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.10); }
  th { background: #f0f0f0; text-align: left; padding: 0.6rem 1rem; font-size: 0.75rem; font-weight: 600; letter-spacing: 0.05em; text-transform: uppercase; color: #555; border-bottom: 1px solid #ddd; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  td { padding: 0.6rem 1rem; border-bottom: 1px solid #eee; font-size: 0.875rem; vertical-align: middle; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  tr:last-child td { border-bottom: none; }
  tr:hover td { background: #fafafa; }
  .num { font-variant-numeric: tabular-nums; }
  .high { color: #b91c1c; font-weight: 600; }
  .medium { color: #c2410c; }
  .normal { color: #1a1a1a; }
</style>
</head>
<body>
<h1>In-Progress Issue Age Report</h1>
<p class="meta">
  <strong>{{.Count}}</strong> in-progress issues &nbsp;·&nbsp;
  {{.Meta.Label}} cycle time: <strong>{{printf "%.1f" .Meta.Threshold}} days</strong> &nbsp;·&nbsp;
  Distribution: {{.Meta.CompletedCount}} completed issues from {{.SampleStart}} to {{.SampleEnd}}
</p>
{{template "table" .Items}}
{{if .ShowCompleted}}
<hr class="divider">
<h2>Completed issues (percentile distribution sample)</h2>
{{template "table" .Completed}}
{{end}}
</body>
</html>
{{define "table"}}
<table>
  <colgroup>
    <col style="width: 10%">
    <col style="width: 23%">
    <col style="width: 8%">
    <col style="width: 11%">
    <col style="width: 9%">
    <col style="width: 15%">
    <col style="width: 12%">
    <col style="width: 12%">
  </colgroup>
  <thead>
    <tr>
      <th>Identifier</th>
      <th>Title</th>
      <th>Days</th>
      <th>Percentile</th>
      <th>Multiplier</th>
      <th>State</th>
      <th>Start Date</th>
      <th>Assignee</th>
    </tr>
  </thead>
  <tbody>
    {{range .}}
    <tr>
      <td>{{.Identifier}}</td>
      <td>{{.Title}}</td>
      <td class="num {{.AgeClass}}">{{printf "%.1f" .AgeDays}}</td>
      <td class="num {{.AgeClass}}">{{.Percentile}}{{.Suffix}}</td>
      <td class="num {{.AgeClass}}">{{.Multiplier}}</td>
      <td>{{.State}}</td>
      <td>{{.StartDate}}</td>
      <td>{{.Assignee}}</td>
    </tr>
    {{end}}
  </tbody>
</table>
{{end}}`

type htmlItemData struct {
	Identifier string
	Title      string
	AgeDays    float64
	Percentile int
	Suffix     string
	Multiplier string
	AgeClass   string
	State      string
	StartDate  string
	Assignee   string
}

type htmlData struct {
	Count         int
	Meta          Meta
	SampleStart   string
	SampleEnd     string
	Items         []htmlItemData
	Completed     []htmlItemData
	ShowCompleted bool
}

func toHTMLItems(items []Item, meta Meta) []htmlItemData {
	var out []htmlItemData
	for _, item := range items {
		out = append(out, htmlItemData{
			Identifier: item.Identifier,
			Title:      item.Title,
			AgeDays:    item.AgeDays,
			Percentile: item.Percentile,
			Suffix:     util.OrdinalSuffix(item.Percentile),
			Multiplier: formatMultiplier(item.Multiplier, meta),
			AgeClass:   ageClass(item.Multiplier, meta.HasThreshold()),
			State:      formatState(item.StateName, item.StateType),
			StartDate:  formatStartDate(item.StartedAt),
			Assignee:   item.Assignee,
		})
	}
	return out
}

// RenderHTML writes an HTML aging report to w: in-progress issues ranked by
// age/percentile/multiplier. When showCompleted is true, a second section
// follows — separated by a divider — listing the completed issues that make
// up the percentile distribution itself. Both tables share the same fixed
// column widths so they stay horizontally aligned.
func RenderHTML(w io.Writer, items []Item, completed []Item, showCompleted bool, meta Meta) error {
	tmpl := template.Must(template.New("report").Parse(htmlTmpl))
	data := htmlData{
		Count:         len(items),
		Meta:          meta,
		SampleStart:   meta.SampleStart.Format("2006-01-02"),
		SampleEnd:     meta.SampleEnd.Format("2006-01-02"),
		Items:         toHTMLItems(items, meta),
		ShowCompleted: showCompleted,
	}
	if showCompleted {
		data.Completed = toHTMLItems(completed, meta)
	}
	return tmpl.Execute(w, data)
}
