package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/internal/sqlite"
	"github.com/commondatageek/delivery-forecast/issues"
)

const agingFixtureCSV = `identifier,team_key,assignee,state_type,created_at,started_at,completed_at
E-1,ENG,alice,completed,2025-01-01,2025-01-02,2025-01-05
E-2,ENG,,completed,2025-01-01,2025-01-02,2025-01-06
E-3,ENG,bob,started,2025-01-03,2025-01-04,
E-4,ENG,,backlog,2025-01-04,,
`

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCmdAging_UnassignedCompletedIssueIncluded(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(agingFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := cmdAging([]string{
			"-input", csvPath, "-format", "text",
			"-sample-start", "2025-01-01", "-sample-end", "2025-01-10",
		}); err != nil {
			t.Fatalf("cmdAging: %v", err)
		}
	})

	// E-1 (assigned) and E-2 (unassigned) both completed in the window; the
	// fix documented in cmd/forecast/aging.go's completedBetween means E-2
	// is no longer silently dropped for having no assignee.
	if !strings.Contains(out, "2 completed issues") {
		t.Errorf("expected the distribution to include both completed issues (assigned and unassigned), got:\n%s", out)
	}
}

func runAgingJSON(t *testing.T, args ...string) string {
	t.Helper()
	full := append([]string{"-format", "json", "-sample-start", "2025-01-01", "-sample-end", "2025-01-10"}, args...)
	return captureStdout(t, func() {
		if err := cmdAging(full); err != nil {
			t.Fatalf("cmdAging(%v): %v", full, err)
		}
	})
}

func TestCmdAging_FileAndDBAgree(t *testing.T) {
	dir := t.TempDir()

	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(agingFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "issues.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	all, err := issues.ReadCSV(strings.NewReader(agingFixtureCSV))
	if err != nil {
		t.Fatalf("ReadCSV fixture: %v", err)
	}
	if err := store.Upsert(context.Background(), all...); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	store.Close()

	fromCSV := runAgingJSON(t, "-input", csvPath)
	fromDB := runAgingJSON(t, "-input", dbPath)

	if fromCSV != fromDB {
		t.Errorf("CSV and SQLite sources produced different output:\nCSV:\n%s\nDB:\n%s", fromCSV, fromDB)
	}
}
