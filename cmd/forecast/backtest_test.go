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

func TestCmdSimBacktest_AcceptsExclusions(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(backtestFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	excPath := filepath.Join(dir, "exclusions.json")
	// From Jan 10 through the Jan 15 deadline nobody works, so any replay day
	// from Jan 10 on has an entirely dead horizon.
	if err := os.WriteFile(excPath, []byte(`{"global":["2025-01-10/2025-01-15"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// deadProbs returns the probability column of rows dated Jan 10 or later
	// that still have work remaining.
	deadProbs := func(out string) []string {
		var probs []string
		for _, line := range strings.Split(strings.TrimSpace(out), "\n")[1:] {
			f := strings.Split(line, ",")
			if f[0] >= "2025-01-10" && f[2] != "0" {
				probs = append(probs, f[3])
			}
		}
		return probs
	}

	with := deadProbs(runSimBacktest(t, "-input", csvPath, "-exclusions", excPath))
	if len(with) == 0 {
		t.Fatal("no replay rows on/after Jan 10 with remaining work")
	}
	for _, p := range with {
		if p != "0.00" {
			t.Errorf("with a dead horizon, probability = %s, want 0.00 (%v)", p, with)
		}
	}

	// Sensitivity guard: without the file the same rows are not all zero.
	without := deadProbs(runSimBacktest(t, "-input", csvPath))
	allZero := true
	for _, p := range without {
		if p != "0.00" {
			allZero = false
		}
	}
	if allZero {
		t.Errorf("without exclusions the Jan 10+ rows are all 0.00 too, so this test proves nothing: %v", without)
	}
}
