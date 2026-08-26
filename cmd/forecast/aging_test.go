package main

import (
	"context"
	"encoding/json"
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

// e3Multiplier decodes JSON output from runAgingJSON and returns the
// multiplier field for issue E-3, the fixture's only in-progress item.
func e3Multiplier(t *testing.T, jsonOut string) float64 {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &rows); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput:\n%s", err, jsonOut)
	}
	for _, row := range rows {
		if row["identifier"] == "E-3" {
			mult, ok := row["multiplier"]
			if !ok {
				t.Fatalf("E-3 row missing multiplier field: %+v", row)
			}
			v, ok := mult.(float64)
			if !ok {
				t.Fatalf("E-3 multiplier is not a number: %+v (%T)", mult, mult)
			}
			return v
		}
	}
	t.Fatalf("no E-3 row found in output:\n%s", jsonOut)
	return 0
}

func TestCmdAging_PercentileFlag(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(agingFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	// The fixture's completed distribution is {3, 4} (E-1: 3 days, E-2: 4
	// days). util.PercentileValue is nearest-rank over len-1 == 1, so most
	// percentile values collide (0-50 -> index 0 -> 3.0; 51-100 -> index 1 ->
	// 4.0). -percentile 1 and -percentile 100 land on opposite ends
	// (thresholds 3.0 and 4.0) so the two runs actually differ.
	low := e3Multiplier(t, runAgingJSON(t, "-input", csvPath, "-percentile", "1"))
	high := e3Multiplier(t, runAgingJSON(t, "-input", csvPath, "-percentile", "100"))

	// E-3's age is measured against real time.Now(), so its absolute
	// multiplier grows daily — assert the relationship, not a fixed value.
	// A lower percentile means a lower threshold, which divides into a
	// larger multiple for the same age.
	if low <= high {
		t.Errorf("expected -percentile 1's multiplier (%v) to be larger than -percentile 100's (%v)", low, high)
	}
}

func TestCmdAging_PercentileValidation(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(agingFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"0", "101"} {
		err := cmdAging([]string{
			"-input", csvPath, "-format", "json",
			"-sample-start", "2025-01-01", "-sample-end", "2025-01-10",
			"-percentile", p,
		})
		if err == nil {
			t.Errorf("-percentile %s: expected an error, got nil", p)
		}
	}
}

func TestCmdAging_DefaultPercentileUnchanged(t *testing.T) {
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

	if !strings.Contains(out, "P85:") {
		t.Errorf("expected default output to be anchored at P85, got:\n%s", out)
	}
}
