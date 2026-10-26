package app

import (
	"reflect"
	"testing"

	"github.com/commondatageek/delivery-forecast/counts"
)

func TestRunCount_ShowTeamsMatrix(t *testing.T) {
	cases := []struct {
		name      string
		teams     []string
		wantShow  bool
		wantWarns int
	}{
		{"multi team, no filter", nil, true, 1},
		{"multi team, filtered", []string{"ENG"}, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := RunCount(CountRequest{
				Issues:  readCSV(t, multiTeamCSV),
				Options: counts.Options{Teams: c.teams, Since: localDay(2025, 1, 1)},
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.ShowTeams != c.wantShow || len(res.Warnings) != c.wantWarns {
				t.Errorf("ShowTeams=%v warnings=%q, want %v and %d warnings", res.ShowTeams, res.Warnings, c.wantShow, c.wantWarns)
			}
		})
	}

	// A single-team set never shows the team column.
	res, err := RunCount(CountRequest{
		Issues:  readCSV(t, "identifier,team_key,project_name,status,updated_at\nE-1,ENG,Foo,started,2025-01-05\n"),
		Options: counts.Options{Since: localDay(2025, 1, 1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ShowTeams || len(res.Warnings) != 0 {
		t.Errorf("single team: ShowTeams=%v warnings=%q", res.ShowTeams, res.Warnings)
	}
}

// A file with completed_at/canceled_at but no status column must still
// treat those issues as terminal, like every other command does.
func TestRunCount_TerminalFromTimestampsWithoutStateType(t *testing.T) {
	const noStateTypeCSV = `identifier,team_key,project_name,completed_at,canceled_at,updated_at
E-1,ENG,Foo,,,2025-01-05
E-2,ENG,Foo,2025-01-06,,2025-01-06
E-3,ENG,Foo,,2025-01-07,2025-01-07
`
	res, err := RunCount(CountRequest{
		Issues:  readCSV(t, noStateTypeCSV),
		Options: counts.Options{Since: localDay(2025, 1, 1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Errorf("Total = %d, want 1 (only E-1 is outstanding)", res.Total)
	}
}

func TestRunCount_NothingOutstandingWarns(t *testing.T) {
	res, err := RunCount(CountRequest{
		Issues:  readCSV(t, "identifier,team_key,project_name,status,updated_at\nE-1,ENG,Foo,completed,2025-01-05\n"),
		Options: counts.Options{Since: localDay(2025, 1, 1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"no outstanding (non-terminal) issues found for the given filters"}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("Warnings = %q, want %q", res.Warnings, want)
	}
}
