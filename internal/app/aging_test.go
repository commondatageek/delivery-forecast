package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/commondatageek/delivery-forecast/aging"
)

const agingFixtureCSV = `identifier,team_key,assignee,status,created_at,started_at,completed_at
E-1,ENG,alice,completed,2025-01-01,2025-01-02,2025-01-05
E-2,ENG,,completed,2025-01-01,2025-01-02,2025-01-06
E-3,ENG,bob,started,2025-01-03,2025-01-04,
E-4,ENG,,backlog,2025-01-04,,
E-5,ENG,bob,started,2025-01-03,2025-01-08,
`

func agingRequest(t *testing.T) AgingRequest {
	return AgingRequest{
		Issues: readCSV(t, agingFixtureCSV),
		Options: aging.Options{
			SampleStart: localDay(2025, 1, 1),
			SampleEnd:   localDay(2025, 1, 10),
			Percentile:  85,
		},
		Today: localDay(2025, 1, 12),
	}
}

func TestRunAging_UnassignedCompletedIssueIncluded(t *testing.T) {
	res, err := RunAging(agingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	// E-1 (assigned) and E-2 (unassigned) both completed in the window.
	if res.Meta.CompletedCount != 2 || len(res.CycleTimes) != 2 {
		t.Errorf("CompletedCount=%d CycleTimes=%v, want both completed issues counted", res.Meta.CompletedCount, res.CycleTimes)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}
}

func TestRunAging_InProgressSortedByAgeDesc(t *testing.T) {
	res, err := RunAging(agingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.InProgress) != 2 {
		t.Fatalf("InProgress = %d items, want 2", len(res.InProgress))
	}
	if res.InProgress[0].AgeDays < res.InProgress[1].AgeDays {
		t.Errorf("not sorted by age descending: %+v", res.InProgress)
	}
	if res.Completed != nil {
		t.Errorf("Completed should be nil without ShowCompleted, got %d items", len(res.Completed))
	}

	req := agingRequest(t)
	req.ShowCompleted = true
	res, err = RunAging(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Completed) != 2 {
		t.Errorf("Completed = %d items, want 2", len(res.Completed))
	}
}

func TestRunAging_ValidateFieldErrors(t *testing.T) {
	req := agingRequest(t)
	req.Percentile = 0
	var fe *FieldError
	_, err := RunAging(req)
	if !errors.As(err, &fe) || fe.Msg != "-percentile must be between 1 and 100, got 0" || !reflect.DeepEqual(fe.Fields, []string{"percentile"}) {
		t.Errorf("percentile: err = %v", err)
	}

	req = agingRequest(t)
	req.SampleStart, req.SampleEnd = req.SampleEnd, req.SampleStart
	res, err := RunAging(req)
	if !errors.As(err, &fe) || fe.Msg != "-sample-start must be before -sample-end" || !reflect.DeepEqual(fe.Fields, []string{"sample-start", "sample-end"}) {
		t.Errorf("window: err = %v", err)
	}
	if res == nil {
		t.Error("result must be non-nil alongside an error")
	}
}

func TestRunAging_EmptyWindowWarns(t *testing.T) {
	req := agingRequest(t)
	req.SampleStart, req.SampleEnd = localDay(2024, 1, 1), localDay(2024, 2, 1)
	res, err := RunAging(req)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"no completed issues found in the sample window; percentiles will be 0 and multipliers blank"}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("Warnings = %q, want %q", res.Warnings, want)
	}
}
