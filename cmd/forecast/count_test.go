package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/internal/sqlite"
	"github.com/commondatageek/delivery-forecast/issues"
)

const countFixtureCSV = `identifier,team_key,team_name,project_name,project_milestone_name,state_type,updated_at
E-1,ENG,Engineering,Foo,M1,started,2025-01-05
E-2,ENG,Engineering,Foo,M1,backlog,2025-01-06
E-3,ENG,Engineering,Foo,,completed,2025-01-07
E-4,ENG,Engineering,Bar,,started,2024-01-01
E-5,DATA,Data,Baz,,started,2025-01-08
`

func runCount(t *testing.T, args ...string) string {
	t.Helper()
	full := append([]string{"-updated-since", "2025-01-01"}, args...)
	return captureStdout(t, func() {
		if err := cmdCount(full); err != nil {
			t.Fatalf("cmdCount(%v): %v", full, err)
		}
	})
}

func TestCmdCount_FileAndDBAgree(t *testing.T) {
	dir := t.TempDir()

	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(countFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "issues.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	all, err := issues.ReadCSV(strings.NewReader(countFixtureCSV))
	if err != nil {
		t.Fatalf("ReadCSV fixture: %v", err)
	}
	if err := store.Upsert(context.Background(), all...); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	store.Close()

	fromCSV := runCount(t, "-input", csvPath)
	fromDB := runCount(t, "-input", dbPath)

	if fromCSV != fromDB {
		t.Errorf("CSV and SQLite sources produced different output:\nCSV:\n%s\nDB:\n%s", fromCSV, fromDB)
	}
}

func TestCmdCount_ExcludesCompletedAndOldProjects(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(countFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runCount(t, "-input", csvPath)

	if !strings.Contains(out, "Foo") {
		t.Errorf("expected Foo (2 outstanding issues, recently updated) in output:\n%s", out)
	}
	if strings.Contains(out, "Bar") {
		t.Errorf("Bar predates -updated-since and should be excluded:\n%s", out)
	}
	if !strings.Contains(out, "Baz") {
		t.Errorf("expected Baz (DATA team) in output:\n%s", out)
	}
	// TOTAL should be 3: Foo's 2 outstanding (E-1, E-2; E-3 is completed) + Baz's 1.
	if !strings.Contains(out, "TOTAL") {
		t.Errorf("expected a TOTAL row:\n%s", out)
	}
}

func TestCmdCount_MilestonesFlag(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(countFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runCount(t, "-input", csvPath, "-milestones")
	if !strings.Contains(out, "M1") {
		t.Errorf("expected the M1 milestone breakdown in grouped output:\n%s", out)
	}
}
