package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/cfd"
)

const multiTeamCSV = `identifier,team_key,state_type,project_name,project_milestone_name,created_at,started_at,completed_at,canceled_at,updated_at
ENG-1,ENG,completed,P,M1,2025-01-02,2025-01-03,2025-01-08,,2025-01-08
ENG-2,ENG,started,P,,2025-01-06,2025-01-07,,,2025-01-15
DATA-1,DATA,completed,Q,,2025-01-05,2025-01-06,2025-01-14,,2025-01-14
DATA-2,DATA,canceled,Q,,2025-01-09,,,2025-01-12,2025-01-12
DATA-3,DATA,backlog,,,2025-01-10,,,,2025-01-10
`

func cfdRequest(t *testing.T, teams ...string) CFDRequest {
	return CFDRequest{
		Issues:  readCSV(t, multiTeamCSV),
		Options: cfd.Options{Teams: teams, Start: localDay(2025, 1, 1), End: localDay(2025, 1, 20)},
	}
}

func TestRunCFD_BlendingWarningOnlyWithoutTeams(t *testing.T) {
	res, err := RunCFD(cfdRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"no -teams filter given; blending data across all 2 teams (DATA, ENG)"}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("Warnings = %q, want %q", res.Warnings, want)
	}

	res, err = RunCFD(cfdRequest(t, "DATA"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("scoped run should not warn, got %q", res.Warnings)
	}
}

func TestRunCFD_HealthCounts(t *testing.T) {
	res, err := RunCFD(cfdRequest(t, "DATA"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Health.TotalIssues != 3 || res.Health.SkippedIssues != 0 {
		t.Errorf("TotalIssues=%d SkippedIssues=%d, want 3 and 0", res.Health.TotalIssues, res.Health.SkippedIssues)
	}
	if len(res.Rows) != 20 {
		t.Errorf("rows = %d, want one per day of the 20-day window", len(res.Rows))
	}
}

func TestRunCFD_EmptyTeamFilterWarns(t *testing.T) {
	res, err := RunCFD(cfdRequest(t, "NOPE"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		if w == "no issues found for the given team filter" {
			found = true
		}
	}
	if !found {
		t.Errorf("Warnings = %q, want the empty-filter warning", res.Warnings)
	}
}

func TestRunCFD_WindowFieldError(t *testing.T) {
	req := cfdRequest(t)
	req.Start, req.End = req.End, req.Start
	_, err := RunCFD(req)
	var fe *FieldError
	if !errors.As(err, &fe) || !strings.Contains(fe.Msg, "-start must be before -end") || !reflect.DeepEqual(fe.Fields, []string{"start", "end"}) {
		t.Errorf("err = %v", err)
	}
}
