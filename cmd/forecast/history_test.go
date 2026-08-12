package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/sqlite"
	"github.com/commondatageek/delivery-forecast/issues"
)

func TestToHistoryIssues(t *testing.T) {
	created := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	started := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	completed := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC)
	canceled := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC)

	in := []issues.Issue{
		{
			Identifier: "ENG-1", TeamKey: "ENG", // non-timestamp fields must not leak through
			CreatedAt: created, StartedAt: started, CompletedAt: completed, CanceledAt: canceled,
		},
	}
	out := toHistoryIssues(in)
	if len(out) != 1 {
		t.Fatalf("got %d issues, want 1", len(out))
	}
	got := out[0]
	if !got.CreatedAt.Equal(created) || !got.StartedAt.Equal(started) ||
		!got.CompletedAt.Equal(completed) || !got.CanceledAt.Equal(canceled) {
		t.Errorf("got %+v", got)
	}
}

// historyFixtureCSV is a small, deterministic issue set: three issues over
// early January, one completed, one still open, one pure backlog.
const historyFixtureCSV = `identifier,team_key,created_at,started_at,completed_at
ENG-1,ENG,2025-01-01,2025-01-02,2025-01-05
ENG-2,ENG,2025-01-02,2025-01-03,
ENG-3,ENG,2025-01-03,,
`

func writeHistoryFixtureCSV(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(path, []byte(historyFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeHistoryFixtureDB(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "issues.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	all, err := issues.ReadCSV(strings.NewReader(historyFixtureCSV))
	if err != nil {
		t.Fatalf("ReadCSV fixture: %v", err)
	}
	if err := store.Upsert(context.Background(), all...); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	return path
}

func runHistoryJSON(t *testing.T, args ...string) []byte {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "out.json")
	full := append([]string{"-format", "json", "-out", outPath}, args...)
	if err := cmdHistory(full); err != nil {
		t.Fatalf("cmdHistory(%v): %v", full, err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return data
}

func TestCmdHistory_EndToEndCSV(t *testing.T) {
	dir := t.TempDir()
	csvPath := writeHistoryFixtureCSV(t, dir)

	data := runHistoryJSON(t, "-input", csvPath, "-start", "2025-01-01", "-end", "2025-01-10")

	var parsed struct {
		TotalIssues int `json:"total_issues"`
		Series      []struct {
			Date      string `json:"date"`
			Total     int    `json:"total"`
			Completed int    `json:"completed"`
		} `json:"series"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, data)
	}
	if parsed.TotalIssues != 3 {
		t.Errorf("total_issues = %d, want 3", parsed.TotalIssues)
	}
	if len(parsed.Series) != 10 {
		t.Fatalf("got %d rows, want 10", len(parsed.Series))
	}
	if parsed.Series[0].Date != "2025-01-01" {
		t.Errorf("first row date = %s, want 2025-01-01", parsed.Series[0].Date)
	}
	last := parsed.Series[len(parsed.Series)-1]
	if last.Total != 3 || last.Completed != 1 {
		t.Errorf("last row = %+v, want total=3 completed=1", last)
	}
}

func TestCmdHistory_FileAndDBAgree(t *testing.T) {
	dir := t.TempDir()
	csvPath := writeHistoryFixtureCSV(t, dir)
	dbPath := writeHistoryFixtureDB(t, dir)

	fromCSV := runHistoryJSON(t, "-input", csvPath, "-start", "2025-01-01", "-end", "2025-01-10")
	fromDB := runHistoryJSON(t, "-input", dbPath, "-start", "2025-01-01", "-end", "2025-01-10")

	if string(fromCSV) != string(fromDB) {
		t.Errorf("CSV and SQLite sources produced different output:\nCSV:\n%s\nDB:\n%s", fromCSV, fromDB)
	}
}

func TestCmdHistory_DefaultStartIsEarliestCreatedAt(t *testing.T) {
	dir := t.TempDir()
	csvPath := writeHistoryFixtureCSV(t, dir)

	data := runHistoryJSON(t, "-input", csvPath, "-end", "2025-01-10")

	var parsed struct {
		Series []struct {
			Date string `json:"date"`
		} `json:"series"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, data)
	}
	if len(parsed.Series) == 0 {
		t.Fatal("expected at least one row")
	}
	if parsed.Series[0].Date != "2025-01-01" {
		t.Errorf("first row date = %s, want 2025-01-01 (earliest created_at)", parsed.Series[0].Date)
	}
}

func TestCmdHistory_EmptyFilteredSetErrorsWithProjectName(t *testing.T) {
	dir := t.TempDir()
	csvPath := writeHistoryFixtureCSV(t, dir)

	err := cmdHistory([]string{"-input", csvPath, "-project", "Nonexistent Project", "-end", "2025-01-10"})
	if err == nil {
		t.Fatal("expected an error for a project with no matching issues")
	}
	if got := err.Error(); !strings.Contains(got, "Nonexistent Project") {
		t.Errorf("error %q does not mention the project name", got)
	}
}

func TestCmdHistory_BadFormat(t *testing.T) {
	dir := t.TempDir()
	csvPath := writeHistoryFixtureCSV(t, dir)

	err := cmdHistory([]string{"-input", csvPath, "-format", "yaml"})
	if err == nil {
		t.Fatal("expected an error for an unrecognized -format")
	}
}
