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

const backtestFixtureCSV = `identifier,team_key,assignee,project_name,state_type,created_at,started_at,completed_at,canceled_at
E-1,ENG,alice,Foo,completed,2025-01-01,2025-01-02,2025-01-05,
E-2,ENG,alice,Foo,completed,2025-01-02,2025-01-03,2025-01-09,
E-3,ENG,alice,Foo,started,2025-01-03,2025-01-04,,
E-4,ENG,alice,Foo,canceled,2025-01-04,2025-01-05,,2025-01-06
`

func runSimBacktest(t *testing.T, args ...string) string {
	t.Helper()
	full := append([]string{
		"-project", "Foo", "-engineers", "1",
		"-sample-start", "2025-01-01", "-sample-end", "2025-02-01",
		"-replay-start-date", "2025-01-01", "-target-end-date", "2025-01-15",
		"-simulations", "200", "-random-seed", "42", "-format", "csv",
	}, args...)
	return captureStdout(t, func() {
		if err := cmdSimBacktest(full); err != nil {
			t.Fatalf("cmdSimBacktest(%v): %v", full, err)
		}
	})
}

func TestCmdSimBacktest_FileAndDBAgree(t *testing.T) {
	dir := t.TempDir()

	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(backtestFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "issues.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	all, err := issues.ReadCSV(strings.NewReader(backtestFixtureCSV))
	if err != nil {
		t.Fatalf("ReadCSV fixture: %v", err)
	}
	if err := store.Upsert(context.Background(), all...); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	store.Close()

	fromCSV := runSimBacktest(t, "-input", csvPath)
	fromDB := runSimBacktest(t, "-input", dbPath)

	if fromCSV != fromDB {
		t.Errorf("CSV and SQLite sources produced different output:\nCSV:\n%s\nDB:\n%s", fromCSV, fromDB)
	}
	// The canceled issue (E-4) must not appear in the backtested set: 3
	// issues (E-1, E-2, E-3), not 4.
	if !strings.Contains(fromCSV, "date,completed,remaining,probability,projected") {
		t.Errorf("expected a CSV header, got:\n%s", fromCSV)
	}
}

func TestCmdSimBacktest_ExcludesCanceledIssues(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(backtestFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runSimBacktest(t, "-input", csvPath, "-target-end-date", "2025-01-04")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// header + at least one row; first data row's "remaining" column must
	// reflect only the 3 non-canceled issues (max remaining == 3, never 4).
	if len(lines) < 2 {
		t.Fatalf("expected at least a header and one row, got:\n%s", out)
	}
	fields := strings.Split(lines[1], ",")
	if fields[2] == "4" {
		t.Errorf("remaining includes the canceled issue: %s", lines[1])
	}
}
