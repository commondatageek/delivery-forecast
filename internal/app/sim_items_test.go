package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

func localDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// constantIssues has alice completing exactly one issue on each day from
// 2025-01-01 through 2025-01-10, so with a 2025-01-01 .. 2025-01-11 sample
// window every sample is 1 and forecasts are exact.
func constantIssues() []issues.Issue {
	var out []issues.Issue
	for d := 1; d <= 10; d++ {
		out = append(out, issues.Issue{
			Identifier:  "C-" + string(rune('A'+d)),
			Assignee:    "alice",
			StateType:   "completed",
			CreatedAt:   localDay(2024, 12, 30),
			StartedAt:   localDay(2024, 12, 31),
			CompletedAt: localDay(2025, 1, d),
		})
	}
	return out
}

// constantBase is a one-engineer SimBase over constantIssues.
func constantBase() SimBase {
	return SimBase{
		Issues:      constantIssues(),
		Engineers:   1,
		SampleStart: localDay(2025, 1, 1),
		SampleEnd:   localDay(2025, 1, 11),
		Simulations: 200,
		Workers:     1,
		Seed:        42,
	}
}

var horizonStart = localDay(2025, 3, 2)

func TestSimItems_DefaultConfidences(t *testing.T) {
	res, err := SimItems(ItemsRequest{SimBase: constantBase(), TargetStart: horizonStart, Days: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Confidences, DefaultConfidences()) {
		t.Errorf("Confidences = %v, want %v", res.Confidences, DefaultConfidences())
	}
	if len(res.Rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(res.Rows))
	}
	for _, r := range res.Rows {
		if r.Items != 5 {
			t.Errorf("confidence %d: items = %d, want 5 (constant 1/day for 5 days)", r.Confidence, r.Items)
		}
	}
	if res.EffectiveDays != 5 || res.TargetEnd.Format("2006-01-02") != "2025-03-06" {
		t.Errorf("window = %d days ending %s, want 5 days ending 2025-03-06", res.EffectiveDays, res.TargetEnd.Format("2006-01-02"))
	}
}

func TestSimItems_RowsMatchItemsAtConfidence(t *testing.T) {
	req := ItemsRequest{
		SimBase:     constantBase(),
		TargetStart: horizonStart,
		Days:        5,
		Confidences: []int{95, 50},
	}
	// A noisy pool, so the rows differ by confidence.
	req.Issues = append(req.Issues, issues.Issue{
		Identifier: "N-1", Assignee: "bob", StateType: "completed", CompletedAt: localDay(2025, 1, 3),
	}, issues.Issue{
		Identifier: "N-2", Assignee: "bob", StateType: "completed", CompletedAt: localDay(2025, 1, 3),
	})
	res, err := SimItems(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 || res.Rows[0].Confidence != 95 || res.Rows[1].Confidence != 50 {
		t.Fatalf("rows = %+v, want confidences in request order [95 50]", res.Rows)
	}
	for _, r := range res.Rows {
		if want := simulate.ItemsAtConfidence(res.Distribution, float64(r.Confidence)); r.Items != want {
			t.Errorf("confidence %d: items = %d, want %d", r.Confidence, r.Items, want)
		}
	}
}

func TestSimItems_ValidateFieldErrors(t *testing.T) {
	base := constantBase()
	noMode := base
	noMode.Engineers = 0
	both := base
	both.WholeTeam = true

	end := localDay(2025, 3, 10)
	cases := []struct {
		name       string
		req        ItemsRequest
		wantMsg    string
		wantFields []string
	}{
		{"no mode", ItemsRequest{SimBase: noMode, TargetStart: horizonStart, Days: 5},
			"one of -engineers or -whole-team must be specified", []string{"engineers", "whole-team"}},
		{"engineers and whole-team", ItemsRequest{SimBase: both, TargetStart: horizonStart, Days: 5},
			"-whole-team and -engineers are mutually exclusive", []string{"engineers", "whole-team"}},
		{"days and end", ItemsRequest{SimBase: base, TargetStart: horizonStart, Days: 5, TargetEnd: end},
			"-days and -target-end-date are mutually exclusive", []string{"days", "target-end-date"}},
		{"neither", ItemsRequest{SimBase: base, TargetStart: horizonStart},
			"one of -days or -target-end-date must be provided", []string{"days", "target-end-date"}},
		{"negative days", ItemsRequest{SimBase: base, TargetStart: horizonStart, Days: -1},
			"-days must be positive", []string{"days"}},
		{"bad confidence", ItemsRequest{SimBase: base, TargetStart: horizonStart, Days: 5, Confidences: []int{50, 101}},
			"-confidence: values must be in (0, 100], got 101", []string{"confidence"}},
		{"zero confidence", ItemsRequest{SimBase: base, TargetStart: horizonStart, Days: 5, Confidences: []int{0}},
			"-confidence: values must be in (0, 100], got 0", []string{"confidence"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			var fe *FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("Validate() = %v, want *FieldError", err)
			}
			if fe.Msg != c.wantMsg || !reflect.DeepEqual(fe.Fields, c.wantFields) {
				t.Errorf("got Msg=%q Fields=%v, want Msg=%q Fields=%v", fe.Msg, fe.Fields, c.wantMsg, c.wantFields)
			}
			// The run function reports the same error with a non-nil result.
			res, runErr := SimItems(c.req)
			if res == nil || runErr == nil || runErr.Error() != c.wantMsg {
				t.Errorf("SimItems = (%v, %v), want non-nil result and %q", res, runErr, c.wantMsg)
			}
		})
	}
}

func TestSimItems_WarningsReturnedOnError(t *testing.T) {
	req := ItemsRequest{SimBase: constantBase(), TargetStart: horizonStart, Days: 5}
	req.TypicalEngineers = []string{"zed"} // matches nobody: empty pool, so ValidatePool fails
	res, err := SimItems(req)
	if err == nil || !strings.Contains(err.Error(), "no completed items in the selected window") {
		t.Fatalf("err = %v, want the empty-pool error", err)
	}
	want := []string{`-typical-engineers engineer "zed" not found in data`}
	if res == nil || !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("Warnings = %v, want %v", res, want)
	}
}
