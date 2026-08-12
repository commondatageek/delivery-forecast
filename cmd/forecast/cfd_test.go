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

const cfdFixtureCSV = `identifier,team_key,created_at,started_at,completed_at,canceled_at
E-1,ENG,2025-01-01,2025-01-02,2025-01-05,
E-2,ENG,2025-01-02,2025-01-03,2025-01-09,
E-3,ENG,2025-01-03,,,
E-4,ENG,2025-01-04,2025-01-05,,2025-01-07
`

func runCFDJSON(t *testing.T, args ...string) []byte {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "out.json")
	full := append([]string{"-format", "json", "-out", outPath, "-start", "2025-01-01", "-end", "2025-01-10"}, args...)
	if err := cmdCFD(full); err != nil {
		t.Fatalf("cmdCFD(%v): %v", full, err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return data
}

func TestCmdCFD_FileAndDBAgree(t *testing.T) {
	dir := t.TempDir()

	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(cfdFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "issues.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	all, err := issues.ReadCSV(strings.NewReader(cfdFixtureCSV))
	if err != nil {
		t.Fatalf("ReadCSV fixture: %v", err)
	}
	if err := store.Upsert(context.Background(), all...); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	store.Close()

	fromCSV := runCFDJSON(t, "-input", csvPath)
	fromDB := runCFDJSON(t, "-input", dbPath)

	if string(fromCSV) != string(fromDB) {
		t.Errorf("CSV and SQLite sources produced different output:\nCSV:\n%s\nDB:\n%s", fromCSV, fromDB)
	}
}

func TestCmdCFD_DBFlagStillWorksDeprecated(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(cfdFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "issues.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	all, err := issues.ReadCSV(strings.NewReader(cfdFixtureCSV))
	if err != nil {
		t.Fatalf("ReadCSV fixture: %v", err)
	}
	if err := store.Upsert(context.Background(), all...); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	store.Close()

	outPath := filepath.Join(dir, "out.json")
	if err := cmdCFD([]string{"-db", dbPath, "-format", "json", "-out", outPath, "-start", "2025-01-01", "-end", "2025-01-10"}); err != nil {
		t.Fatalf("cmdCFD with -db: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Errorf("expected output file to be written: %v", err)
	}
}
