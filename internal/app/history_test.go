package app

import (
	"errors"
	"reflect"
	"testing"
)

func historyRequest(t *testing.T) HistoryRequest {
	return HistoryRequest{
		Issues:     readCSV(t, multiTeamCSV),
		End:        localDay(2025, 1, 20),
		WindowDays: 28,
	}
}

func TestRunHistory_InferredStart(t *testing.T) {
	res, err := RunHistory(historyRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	// Earliest created_at in the fixture is ENG-1's 2025-01-02.
	if !res.StartInferred || res.Start.Format("2006-01-02") != "2025-01-02" {
		t.Errorf("Start=%v inferred=%v, want 2025-01-02 inferred", res.Start, res.StartInferred)
	}
	if len(res.Result.Rows) != 19 {
		t.Errorf("rows = %d, want 19 (Jan 2 through Jan 20)", len(res.Result.Rows))
	}

	req := historyRequest(t)
	req.Start = localDay(2025, 1, 5)
	res, err = RunHistory(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StartInferred || res.Start.Format("2006-01-02") != "2025-01-05" {
		t.Errorf("explicit start: Start=%v inferred=%v", res.Start, res.StartInferred)
	}
}

func TestRunHistory_EmptyScopeFieldErrors(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*HistoryRequest)
		wantMsg    string
		wantFields []string
	}{
		{"milestone", func(r *HistoryRequest) { r.Project, r.Milestone = "P", "Nope" },
			`no issues found for project "P" milestone "Nope" — check spelling`, []string{"project", "milestone"}},
		{"project", func(r *HistoryRequest) { r.Project = "Nope" },
			`no issues found for project "Nope" — check spelling`, []string{"project"}},
		{"teams", func(r *HistoryRequest) { r.Teams = []string{"NOPE"} },
			"no issues found for the given filters — check spelling", []string{"teams"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := historyRequest(t)
			c.mutate(&req)
			_, err := RunHistory(req)
			var fe *FieldError
			if !errors.As(err, &fe) || fe.Msg != c.wantMsg || !reflect.DeepEqual(fe.Fields, c.wantFields) {
				t.Errorf("err = %v, want FieldError{%v, %q}", err, c.wantFields, c.wantMsg)
			}
		})
	}
}

func TestRunHistory_BlendingWarningUsesFilteredSet(t *testing.T) {
	// Unscoped: both teams present.
	res, err := RunHistory(historyRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"no -teams filter given; blending data across all 2 teams (DATA, ENG)"}; !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("Warnings = %q, want %q", res.Warnings, want)
	}

	// Project P only has ENG issues, so no blending even though Teams is empty.
	req := historyRequest(t)
	req.Project = "P"
	res, err = RunHistory(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("filtered set has one team; Warnings = %q, want none", res.Warnings)
	}
}

func TestRunHistory_ValidateAndEndBeforeStart(t *testing.T) {
	req := historyRequest(t)
	req.WindowDays = 0
	var fe *FieldError
	if _, err := RunHistory(req); !errors.As(err, &fe) || fe.Msg != "-window must be positive" || !reflect.DeepEqual(fe.Fields, []string{"window"}) {
		t.Errorf("window: err = %v", err)
	}

	req = historyRequest(t)
	req.Start, req.End = localDay(2025, 1, 10), localDay(2025, 1, 5)
	if _, err := RunHistory(req); !errors.As(err, &fe) || fe.Msg != "-end must not be before -start (2025-01-10)" || !reflect.DeepEqual(fe.Fields, []string{"end", "start"}) {
		t.Errorf("end before start: err = %v", err)
	}
}
