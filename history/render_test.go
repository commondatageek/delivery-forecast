package history_test

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/history"
)

func naNResult() history.Result {
	return history.Result{
		WindowDays:    28,
		TotalIssues:   3,
		SkippedIssues: 1,
		Rows: []history.DayRow{
			{
				Date: day("2025-01-01"), Total: 3, Completed: 0, Canceled: 0, Backlog: 3, InProgress: 0, Remaining: 3,
				Throughput7d: math.NaN(), ThroughputWindow: math.NaN(),
				ScopeGrowthWindow: 0.5, NetFlowWindow: 0.25,
				LeadTimeP50: math.NaN(), LeadTimeP85: math.NaN(),
				CycleTimeP50: math.NaN(), CycleTimeP85: math.NaN(),
				WIPAgeAvg: math.NaN(), WIPAgeP85: math.NaN(), WIPAgeMax: math.NaN(),
				LittlesLawCT: math.NaN(), DaysRemainingAtRate: math.NaN(),
			},
		},
	}
}

func TestRenderCSV_NaNIsEmptyCell(t *testing.T) {
	var buf bytes.Buffer
	if err := history.RenderCSV(&buf, naNResult()); err != nil {
		t.Fatalf("RenderCSV: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 row, got %d lines", len(lines))
	}
	fields := strings.Split(lines[1], ",")
	// throughput_7d is the 12th column (index 11): date,total,completed,
	// canceled,backlog,in_progress,remaining,created_delta,started_delta,
	// completed_delta,canceled_delta,throughput_7d,...
	if fields[11] != "" {
		t.Errorf("throughput_7d cell = %q, want empty", fields[11])
	}
	if fields[0] != "2025-01-01" {
		t.Errorf("date cell = %q", fields[0])
	}
	if fields[2] != "0" {
		t.Errorf("completed cell = %q, want \"0\" (a real zero, not NaN)", fields[2])
	}
}

func TestRenderJSON_NaNIsNullAndMarshals(t *testing.T) {
	var buf bytes.Buffer
	if err := history.RenderJSON(&buf, naNResult()); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	var parsed struct {
		WindowDays    int `json:"window_days"`
		TotalIssues   int `json:"total_issues"`
		SkippedIssues int `json:"skipped_issues"`
		Series        []struct {
			Date             string   `json:"date"`
			Completed        int      `json:"completed"`
			Throughput7d     *float64 `json:"throughput_7d"`
			ThroughputWindow *float64 `json:"throughput_window"`
			ScopeGrowth      *float64 `json:"scope_growth_window"`
		} `json:"series"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output did not marshal/parse as valid JSON (the NaN trap): %v\noutput:\n%s", err, buf.String())
	}
	if parsed.WindowDays != 28 || parsed.TotalIssues != 3 || parsed.SkippedIssues != 1 {
		t.Errorf("header mismatch: %+v", parsed)
	}
	if len(parsed.Series) != 1 {
		t.Fatalf("expected 1 series row, got %d", len(parsed.Series))
	}
	row := parsed.Series[0]
	if row.Throughput7d != nil {
		t.Errorf("throughput_7d = %v, want null", *row.Throughput7d)
	}
	if row.ThroughputWindow != nil {
		t.Errorf("throughput_window = %v, want null", *row.ThroughputWindow)
	}
	if row.ScopeGrowth == nil || *row.ScopeGrowth != 0.5 {
		t.Errorf("scope_growth_window = %v, want 0.5", row.ScopeGrowth)
	}
	if row.Completed != 0 {
		t.Errorf("completed = %d, want 0 (a real zero, not null)", row.Completed)
	}
}

func TestRenderText_NaNIsDash(t *testing.T) {
	var buf bytes.Buffer
	if err := history.RenderText(&buf, naNResult()); err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "-") {
		t.Errorf("expected a \"-\" placeholder for NaN in text output:\n%s", out)
	}
	if !strings.Contains(out, "2025-01-01") {
		t.Errorf("expected the row's date in text output:\n%s", out)
	}
}
