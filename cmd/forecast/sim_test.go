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

const simFixtureCSV = `identifier,team_key,assignee,state_type,created_at,started_at,completed_at
E-1,ENG,alice,completed,2025-01-01,2025-01-02,2025-01-03
E-2,ENG,alice,completed,2025-01-02,2025-01-03,2025-01-04
E-3,ENG,alice,completed,2025-01-03,2025-01-04,2025-01-05
E-4,ENG,,completed,2025-01-04,2025-01-05,2025-01-08
`

func runSimItems(t *testing.T, args ...string) string {
	t.Helper()
	full := append([]string{
		"-engineers", "1", "-sample-start", "2025-01-01", "-sample-end", "2025-02-01",
		"-days", "30", "-simulations", "200", "-random-seed", "42",
	}, args...)
	return captureStdout(t, func() {
		if err := cmdSimItems(full); err != nil {
			t.Fatalf("cmdSimItems(%v): %v", full, err)
		}
	})
}

func TestCmdSimItems_FileAndDBAgree(t *testing.T) {
	dir := t.TempDir()

	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(simFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "issues.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	all, err := issues.ReadCSV(strings.NewReader(simFixtureCSV))
	if err != nil {
		t.Fatalf("ReadCSV fixture: %v", err)
	}
	if err := store.Upsert(context.Background(), all...); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	store.Close()

	fromCSV := runSimItems(t, "-input", csvPath)
	fromDB := runSimItems(t, "-input", dbPath)

	if fromCSV != fromDB {
		t.Errorf("CSV and SQLite sources produced different output:\nCSV:\n%s\nDB:\n%s", fromCSV, fromDB)
	}
	if !strings.Contains(fromCSV, "Confidence") {
		t.Errorf("expected a confidence table in output, got:\n%s", fromCSV)
	}
}

func TestCmdSimItems_DBFlagStillWorksDeprecated(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "issues.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	all, err := issues.ReadCSV(strings.NewReader(simFixtureCSV))
	if err != nil {
		t.Fatalf("ReadCSV fixture: %v", err)
	}
	if err := store.Upsert(context.Background(), all...); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	store.Close()

	out := captureStdout(t, func() {
		args := []string{
			"-db", dbPath, "-engineers", "1", "-sample-start", "2025-01-01", "-sample-end", "2025-02-01",
			"-days", "30", "-simulations", "200", "-random-seed", "42",
		}
		if err := cmdSimItems(args); err != nil {
			t.Fatalf("cmdSimItems with -db: %v", err)
		}
	})
	if !strings.Contains(out, "Confidence") {
		t.Errorf("expected a confidence table in output, got:\n%s", out)
	}
}

func TestCmdSimItems_NamedEngineersLabel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "issues.csv")
	if err := os.WriteFile(path, []byte(simFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"-input", path, "-engineers", "alice,bob", "-sample-start", "2025-01-01", "-sample-end", "2025-02-01",
		"-days", "30", "-simulations", "200", "-random-seed", "42",
	}
	out := captureStdout(t, func() {
		if err := cmdSimItems(args); err != nil {
			t.Fatalf("cmdSimItems(%v): %v", args, err)
		}
	})
	if !strings.Contains(out, "2 equivalent engineers [alice, bob]") {
		t.Errorf("header should show the named engineers, got:\n%s", out)
	}
}
