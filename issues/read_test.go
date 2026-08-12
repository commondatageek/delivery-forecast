package issues

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustLocal(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, time.Local)
}

func TestReadCSV_RoundTrip(t *testing.T) {
	csv := `identifier,title,assignee,team_key,team_name,project_id,project_name,project_milestone_id,project_milestone_name,state_type,state_name,created_at,started_at,completed_at,canceled_at,archived_at,auto_archived_at,added_to_project_at,updated_at
ENG-1,Fix bug,alice,ENG,Engineering,proj-1,Project One,ms-1,Milestone One,completed,Done,2025-06-01T00:00:00Z,2025-06-02 09:00:00,2025-06-05T10:30:00,2025-06-06,2025-06-07,2025-06-08T00:00:00Z,2025-06-01,2025-06-05T10:30:00Z
`
	issues, err := ReadCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	got := issues[0]
	want := Issue{
		Identifier:           "ENG-1",
		Title:                "Fix bug",
		Assignee:             "alice",
		TeamKey:              "ENG",
		TeamName:             "Engineering",
		ProjectID:            "proj-1",
		ProjectName:          "Project One",
		ProjectMilestoneID:   "ms-1",
		ProjectMilestoneName: "Milestone One",
		StateType:            "completed",
		StateName:            "Done",
		CreatedAt:            time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		StartedAt:            mustLocal(2025, 6, 2, 9, 0, 0),
		CompletedAt:          mustLocal(2025, 6, 5, 10, 30, 0),
		CanceledAt:           mustLocal(2025, 6, 6, 0, 0, 0),
		ArchivedAt:           mustLocal(2025, 6, 7, 0, 0, 0),
		AutoArchivedAt:       time.Date(2025, 6, 8, 0, 0, 0, 0, time.UTC),
		AddedToProjectAt:     mustLocal(2025, 6, 1, 0, 0, 0),
		UpdatedAt:            time.Date(2025, 6, 5, 10, 30, 0, 0, time.UTC),
	}
	if got.Identifier != want.Identifier || got.Title != want.Title || got.Assignee != want.Assignee ||
		got.TeamKey != want.TeamKey || got.TeamName != want.TeamName || got.ProjectID != want.ProjectID ||
		got.ProjectName != want.ProjectName || got.ProjectMilestoneID != want.ProjectMilestoneID ||
		got.ProjectMilestoneName != want.ProjectMilestoneName || got.StateType != want.StateType ||
		got.StateName != want.StateName {
		t.Fatalf("string fields mismatch:\ngot  %+v\nwant %+v", got, want)
	}
	for name, pair := range map[string][2]time.Time{
		"created_at":          {got.CreatedAt, want.CreatedAt},
		"started_at":          {got.StartedAt, want.StartedAt},
		"completed_at":        {got.CompletedAt, want.CompletedAt},
		"canceled_at":         {got.CanceledAt, want.CanceledAt},
		"archived_at":         {got.ArchivedAt, want.ArchivedAt},
		"auto_archived_at":    {got.AutoArchivedAt, want.AutoArchivedAt},
		"added_to_project_at": {got.AddedToProjectAt, want.AddedToProjectAt},
		"updated_at":          {got.UpdatedAt, want.UpdatedAt},
	} {
		if !pair[0].Equal(pair[1]) {
			t.Errorf("%s: got %v, want %v", name, pair[0], pair[1])
		}
	}
}

func TestReadCSV_ScrambledAndUnknownColumns(t *testing.T) {
	csv := "bogus,identifier,team_key\nignored-value,ENG-1,ENG\n"
	issues, err := ReadCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Identifier != "ENG-1" || issues[0].TeamKey != "ENG" {
		t.Errorf("got %+v", issues[0])
	}
}

func TestReadCSV_PartialColumns(t *testing.T) {
	csv := "identifier,created_at,completed_at\nENG-1,2025-01-01,2025-01-05\n"
	issues, err := ReadCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	got := issues[0]
	if got.Identifier != "ENG-1" {
		t.Errorf("Identifier = %q", got.Identifier)
	}
	if got.Title != "" || got.Assignee != "" || got.TeamKey != "" {
		t.Errorf("expected zero string fields, got %+v", got)
	}
	if got.StartedAt.IsZero() != true {
		t.Errorf("expected zero StartedAt, got %v", got.StartedAt)
	}
}

func TestReadCSV_BadTimestamp(t *testing.T) {
	csv := "identifier,created_at\nENG-1,not-a-date\n"
	_, err := ReadCSV(strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "row 2") || !strings.Contains(msg, "created_at") {
		t.Errorf("error %q does not mention row number and column name", msg)
	}
}

func TestReadCSV_NoRecognizedColumns(t *testing.T) {
	csv := "foo,bar\n1,2\n"
	_, err := ReadCSV(strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no recognized columns") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReadCSV_RaggedRow(t *testing.T) {
	csv := "identifier,created_at\nENG-1,2025-01-01,extra\n"
	_, err := ReadCSV(strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "row") {
		t.Errorf("expected error to mention the row, got: %v", err)
	}
}

const jsonArrayFixture = `[
  {"identifier": "ENG-1", "team_key": "ENG", "created_at": "2025-06-01", "completed_at": "2025-06-05T10:30:00Z"},
  {"identifier": "ENG-2", "team_key": "ENG", "created_at": "2025-06-02"}
]`

const jsonLinesFixture = `{"identifier": "ENG-1", "team_key": "ENG", "created_at": "2025-06-01", "completed_at": "2025-06-05T10:30:00Z"}
{"identifier": "ENG-2", "team_key": "ENG", "created_at": "2025-06-02"}
`

func TestReadJSON_ArrayAndLinesAgree(t *testing.T) {
	fromArray, err := ReadJSON(strings.NewReader(jsonArrayFixture))
	if err != nil {
		t.Fatalf("ReadJSON(array): %v", err)
	}
	fromLines, err := ReadJSON(strings.NewReader(jsonLinesFixture))
	if err != nil {
		t.Fatalf("ReadJSON(lines): %v", err)
	}
	if len(fromArray) != 2 || len(fromLines) != 2 {
		t.Fatalf("expected 2 issues each, got %d and %d", len(fromArray), len(fromLines))
	}
	for i := range fromArray {
		a, b := fromArray[i], fromLines[i]
		if a.Identifier != b.Identifier || a.TeamKey != b.TeamKey || !a.CreatedAt.Equal(b.CreatedAt) || !a.CompletedAt.Equal(b.CompletedAt) {
			t.Errorf("item %d differs:\narray: %+v\nlines: %+v", i, a, b)
		}
	}
	if fromArray[0].Identifier != "ENG-1" || fromArray[1].Identifier != "ENG-2" {
		t.Errorf("unexpected identifiers: %+v", fromArray)
	}
}

func TestReadFile_DispatchesOnExtension(t *testing.T) {
	dir := t.TempDir()

	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte("identifier,created_at\nENG-1,2025-01-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(csvPath)
	if err != nil {
		t.Fatalf("ReadFile(csv): %v", err)
	}
	if len(got) != 1 || got[0].Identifier != "ENG-1" {
		t.Errorf("got %+v", got)
	}

	jsonPath := filepath.Join(dir, "issues.json")
	if err := os.WriteFile(jsonPath, []byte(jsonArrayFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("ReadFile(json): %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d issues, want 2", len(got))
	}

	unknownPath := filepath.Join(dir, "issues.txt")
	if err := os.WriteFile(unknownPath, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(unknownPath); err == nil {
		t.Fatal("expected error for unrecognized extension")
	}
}

func TestReadFile_StdinRequiresReadStream(t *testing.T) {
	if _, err := ReadFile("-"); err == nil {
		t.Fatal("expected error reading \"-\" via ReadFile")
	}
}

func TestStatusHelpers(t *testing.T) {
	completedAt := mustLocal(2025, 1, 5, 0, 0, 0)
	canceledAt := mustLocal(2025, 1, 5, 0, 0, 0)
	startedAt := mustLocal(2025, 1, 2, 0, 0, 0)

	tests := []struct {
		name                                      string
		issue                                     Issue
		completed, canceled, terminal, inProgress bool
	}{
		{"timestamp-only completed", Issue{CompletedAt: completedAt}, true, false, true, false},
		{"timestamp-only canceled", Issue{CanceledAt: canceledAt}, false, true, true, false},
		{"timestamp-only in progress", Issue{StartedAt: startedAt}, false, false, false, true},
		{"state-type-only completed", Issue{StateType: "completed"}, true, false, true, false},
		{"state-type-only canceled", Issue{StateType: "canceled"}, false, true, true, false},
		{"state-type-only duplicate", Issue{StateType: "duplicate"}, false, true, true, false},
		{"state-type-only started", Issue{StateType: "started"}, false, false, false, true},
		{"timestamp wins over disagreeing state_type", Issue{CompletedAt: completedAt, StateType: "started"}, true, false, true, false},
		{"neither set", Issue{}, false, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.issue.IsCompleted(); got != tt.completed {
				t.Errorf("IsCompleted() = %v, want %v", got, tt.completed)
			}
			if got := tt.issue.IsCanceled(); got != tt.canceled {
				t.Errorf("IsCanceled() = %v, want %v", got, tt.canceled)
			}
			if got := tt.issue.IsTerminal(); got != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.terminal)
			}
			if got := tt.issue.IsInProgress(); got != tt.inProgress {
				t.Errorf("IsInProgress() = %v, want %v", got, tt.inProgress)
			}
		})
	}
}
