package history

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"text/tabwriter"
)

// csvHeader lists the columns in DayRow's field order, date first.
var csvHeader = []string{
	"date", "total", "completed", "canceled", "backlog", "in_progress", "remaining",
	"created_delta", "started_delta", "completed_delta", "canceled_delta",
	"throughput_7d", "throughput_window", "scope_growth_window", "net_flow_window",
	"lead_time_p50", "lead_time_p85", "cycle_time_p50", "cycle_time_p85",
	"wip_age_avg", "wip_age_p85", "wip_age_max",
	"littles_law_ct", "days_remaining_at_rate",
}

func csvFloat(v float64) string {
	if math.IsNaN(v) {
		return ""
	}
	return strconv.FormatFloat(v, 'f', 3, 64)
}

func csvRecord(r DayRow) []string {
	return []string{
		r.Date.Format("2006-01-02"),
		strconv.Itoa(r.Total),
		strconv.Itoa(r.Completed),
		strconv.Itoa(r.Canceled),
		strconv.Itoa(r.Backlog),
		strconv.Itoa(r.InProgress),
		strconv.Itoa(r.Remaining),
		strconv.Itoa(r.CreatedDelta),
		strconv.Itoa(r.StartedDelta),
		strconv.Itoa(r.CompletedDelta),
		strconv.Itoa(r.CanceledDelta),
		csvFloat(r.Throughput7d),
		csvFloat(r.ThroughputWindow),
		csvFloat(r.ScopeGrowthWindow),
		csvFloat(r.NetFlowWindow),
		csvFloat(r.LeadTimeP50),
		csvFloat(r.LeadTimeP85),
		csvFloat(r.CycleTimeP50),
		csvFloat(r.CycleTimeP85),
		csvFloat(r.WIPAgeAvg),
		csvFloat(r.WIPAgeP85),
		csvFloat(r.WIPAgeMax),
		csvFloat(r.LittlesLawCT),
		csvFloat(r.DaysRemainingAtRate),
	}
}

// RenderCSV writes res's series as CSV to w: a header row of snake_case
// column names in DayRow's field order, then one row per day. Integers are
// written plain, floats to 3 decimal places, and NaN as an empty cell. No
// metadata header, so the output stays machine-parseable.
func RenderCSV(w io.Writer, res Result) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, r := range res.Rows {
		if err := cw.Write(csvRecord(r)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// jsonDayRow mirrors DayRow for JSON output. Float fields are *float64 so
// NaN can marshal as null (encoding/json errors on a bare NaN).
type jsonDayRow struct {
	Date string `json:"date"`

	Total      int `json:"total"`
	Completed  int `json:"completed"`
	Canceled   int `json:"canceled"`
	Backlog    int `json:"backlog"`
	InProgress int `json:"in_progress"`
	Remaining  int `json:"remaining"`

	CreatedDelta   int `json:"created_delta"`
	StartedDelta   int `json:"started_delta"`
	CompletedDelta int `json:"completed_delta"`
	CanceledDelta  int `json:"canceled_delta"`

	Throughput7d        *float64 `json:"throughput_7d"`
	ThroughputWindow    *float64 `json:"throughput_window"`
	ScopeGrowthWindow   *float64 `json:"scope_growth_window"`
	NetFlowWindow       *float64 `json:"net_flow_window"`
	LeadTimeP50         *float64 `json:"lead_time_p50"`
	LeadTimeP85         *float64 `json:"lead_time_p85"`
	CycleTimeP50        *float64 `json:"cycle_time_p50"`
	CycleTimeP85        *float64 `json:"cycle_time_p85"`
	WIPAgeAvg           *float64 `json:"wip_age_avg"`
	WIPAgeP85           *float64 `json:"wip_age_p85"`
	WIPAgeMax           *float64 `json:"wip_age_max"`
	LittlesLawCT        *float64 `json:"littles_law_ct"`
	DaysRemainingAtRate *float64 `json:"days_remaining_at_rate"`
}

type jsonResult struct {
	WindowDays    int          `json:"window_days"`
	TotalIssues   int          `json:"total_issues"`
	SkippedIssues int          `json:"skipped_issues"`
	Series        []jsonDayRow `json:"series"`
}

// jsonFloat returns nil for NaN (marshals to null) or a pointer to v rounded
// to 3 decimal places.
func jsonFloat(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	rounded := math.Round(v*1000) / 1000
	return &rounded
}

func toJSONDayRow(r DayRow) jsonDayRow {
	return jsonDayRow{
		Date:                r.Date.Format("2006-01-02"),
		Total:               r.Total,
		Completed:           r.Completed,
		Canceled:            r.Canceled,
		Backlog:             r.Backlog,
		InProgress:          r.InProgress,
		Remaining:           r.Remaining,
		CreatedDelta:        r.CreatedDelta,
		StartedDelta:        r.StartedDelta,
		CompletedDelta:      r.CompletedDelta,
		CanceledDelta:       r.CanceledDelta,
		Throughput7d:        jsonFloat(r.Throughput7d),
		ThroughputWindow:    jsonFloat(r.ThroughputWindow),
		ScopeGrowthWindow:   jsonFloat(r.ScopeGrowthWindow),
		NetFlowWindow:       jsonFloat(r.NetFlowWindow),
		LeadTimeP50:         jsonFloat(r.LeadTimeP50),
		LeadTimeP85:         jsonFloat(r.LeadTimeP85),
		CycleTimeP50:        jsonFloat(r.CycleTimeP50),
		CycleTimeP85:        jsonFloat(r.CycleTimeP85),
		WIPAgeAvg:           jsonFloat(r.WIPAgeAvg),
		WIPAgeP85:           jsonFloat(r.WIPAgeP85),
		WIPAgeMax:           jsonFloat(r.WIPAgeMax),
		LittlesLawCT:        jsonFloat(r.LittlesLawCT),
		DaysRemainingAtRate: jsonFloat(r.DaysRemainingAtRate),
	}
}

// RenderJSON writes res as JSON to w: {"window_days","total_issues",
// "skipped_issues","series":[...]}, with NaN fields marshaled as null.
func RenderJSON(w io.Writer, res Result) error {
	out := jsonResult{
		WindowDays:    res.WindowDays,
		TotalIssues:   res.TotalIssues,
		SkippedIssues: res.SkippedIssues,
	}
	out.Series = make([]jsonDayRow, len(res.Rows))
	for i, r := range res.Rows {
		out.Series[i] = toJSONDayRow(r)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func textFloat(v float64) string {
	if math.IsNaN(v) {
		return "-"
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

const textHeader = "DATE\tTOTAL\tCOMPLETED\tCANCELED\tBACKLOG\tIN_PROGRESS\tREMAINING\t" +
	"CREATED_D\tSTARTED_D\tCOMPLETED_D\tCANCELED_D\t" +
	"THROUGHPUT_7D\tTHROUGHPUT_WIN\tSCOPE_GROWTH\tNET_FLOW\t" +
	"LEAD_P50\tLEAD_P85\tCYCLE_P50\tCYCLE_P85\t" +
	"WIP_AVG\tWIP_P85\tWIP_MAX\tLITTLES_CT\tDAYS_REMAINING"

// RenderText writes res as a tabular report to w: a short header block
// (window, issue counts, skipped) followed by the table. NaN renders as "-";
// floats are shown to 1 decimal place.
func RenderText(w io.Writer, res Result) error {
	fmt.Fprintf(w, "History: %d issue(s), %d skipped (no created_at)  ·  rolling window: %d days\n\n",
		res.TotalIssues, res.SkippedIssues, res.WindowDays)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, textHeader)
	for _, r := range res.Rows {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Date.Format("2006-01-02"),
			r.Total, r.Completed, r.Canceled, r.Backlog, r.InProgress, r.Remaining,
			r.CreatedDelta, r.StartedDelta, r.CompletedDelta, r.CanceledDelta,
			textFloat(r.Throughput7d), textFloat(r.ThroughputWindow),
			textFloat(r.ScopeGrowthWindow), textFloat(r.NetFlowWindow),
			textFloat(r.LeadTimeP50), textFloat(r.LeadTimeP85),
			textFloat(r.CycleTimeP50), textFloat(r.CycleTimeP85),
			textFloat(r.WIPAgeAvg), textFloat(r.WIPAgeP85), textFloat(r.WIPAgeMax),
			textFloat(r.LittlesLawCT), textFloat(r.DaysRemainingAtRate),
		)
	}
	return tw.Flush()
}
