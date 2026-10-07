package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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

// simItemsRaw runs cmdSimItems with exactly args (no implicit -days), returning
// stdout and the error rather than failing on one.
func simItemsRaw(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = cmdSimItems(args) })
	return out, err
}

func TestCmdSimItems_RequiresDaysOrTargetEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "issues.csv")
	if err := os.WriteFile(path, []byte(simFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := simItemsRaw(t, "-input", path, "-engineers", "1", "-sample-start", "2025-01-01", "-sample-end", "2025-02-01")
	if err == nil || !strings.Contains(err.Error(), "one of -days or -target-end-date") {
		t.Fatalf("error = %v, want one about -days / -target-end-date", err)
	}
}

func TestCmdSimItems_TargetEndEquivalentToDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "issues.csv")
	if err := os.WriteFile(path, []byte(simFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	base := []string{"-input", path, "-engineers", "1", "-sample-start", "2025-01-01", "-sample-end", "2025-02-01",
		"-simulations", "200", "-random-seed", "42", "-target-start-date", "2025-03-02"}

	byDays, err := simItemsRaw(t, append(append([]string{}, base...), "-days", "10")...)
	if err != nil {
		t.Fatal(err)
	}
	byEnd, err := simItemsRaw(t, append(append([]string{}, base...), "-target-end-date", "2025-03-11")...)
	if err != nil {
		t.Fatal(err)
	}

	body := func(s string) string { _, rest, _ := strings.Cut(s, "\n"); return rest }
	if body(byDays) != body(byEnd) {
		t.Errorf("-days 10 and -target-end-date 2025-03-11 disagree:\n%s\nvs\n%s", byDays, byEnd)
	}
	if want := "2025-03-02 to 2025-03-11 (10 days)"; !strings.Contains(byDays, want) || !strings.Contains(byEnd, want) {
		t.Errorf("headers should show %q:\n%s\n%s", want, byDays, byEnd)
	}
}

// constantFixtureCSV has alice completing exactly one issue on each day from
// 2025-01-01 through 2025-01-10, so with -sample-start 2025-01-01 -sample-end
// 2025-01-11 every sample is 1 and forecasts are exact.
const constantFixtureCSV = `identifier,team_key,assignee,state_type,created_at,started_at,completed_at
C-1,ENG,alice,completed,2024-12-30,2024-12-31,2025-01-01
C-2,ENG,alice,completed,2024-12-31,2025-01-01,2025-01-02
C-3,ENG,alice,completed,2025-01-01,2025-01-02,2025-01-03
C-4,ENG,alice,completed,2025-01-02,2025-01-03,2025-01-04
C-5,ENG,alice,completed,2025-01-03,2025-01-04,2025-01-05
C-6,ENG,alice,completed,2025-01-04,2025-01-05,2025-01-06
C-7,ENG,alice,completed,2025-01-05,2025-01-06,2025-01-07
C-8,ENG,alice,completed,2025-01-06,2025-01-07,2025-01-08
C-9,ENG,alice,completed,2025-01-07,2025-01-08,2025-01-09
C-10,ENG,alice,completed,2025-01-08,2025-01-09,2025-01-10
`

// A Sunday; weekdays are irrelevant to the calendar this round.
const horizonStart = "2025-03-02"

// horizonExclusions: days 1-3 of the horizon are off for everyone, days 4-5
// additionally off for alice.
const horizonExclusions = `{"global":["2025-03-03/2025-03-05"],"engineers":{"alice":["2025-03-06","2025-03-07"]}}`

func writeConstantFixture(t *testing.T, exclusions string) (inputPath, exclusionsPath string) {
	t.Helper()
	dir := t.TempDir()
	inputPath = filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(inputPath, []byte(constantFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if exclusions != "" {
		exclusionsPath = filepath.Join(dir, "exclusions.json")
		if err := os.WriteFile(exclusionsPath, []byte(exclusions), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return inputPath, exclusionsPath
}

// constantArgs are the shared flags for the constant-fixture tests; callers
// append the subcommand-specific ones.
func constantArgs(input, exclusions string, extra ...string) []string {
	args := []string{
		"-input", input, "-sample-start", "2025-01-01", "-sample-end", "2025-01-11",
		"-target-start-date", horizonStart, "-simulations", "200", "-random-seed", "1",
	}
	if exclusions != "" {
		args = append(args, "-exclusions", exclusions)
	}
	return append(args, extra...)
}

func runSim(t *testing.T, fn func([]string) error, args []string) string {
	t.Helper()
	return captureStdout(t, func() {
		if err := fn(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	})
}

func assertConfidenceRows(t *testing.T, out string, want string) {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\d+%\s+at least (\d+)$`)
	m := re.FindAllStringSubmatch(out, -1)
	if len(m) == 0 {
		t.Fatalf("no confidence rows in:\n%s", out)
	}
	for _, row := range m {
		if row[1] != want {
			t.Errorf("row %q: want at least %s\n%s", row[0], want, out)
		}
	}
}

func TestCmdSimItems_HorizonGlobalExclusions(t *testing.T) {
	in, exc := writeConstantFixture(t, horizonExclusions)

	assertConfidenceRows(t, runSim(t, cmdSimItems, constantArgs(in, "", "-engineers", "2", "-days", "10")), "20")
	assertConfidenceRows(t, runSim(t, cmdSimItems, constantArgs(in, exc, "-engineers", "2", "-days", "10")), "14")
}

func TestCmdSimItems_HorizonNamedExclusions(t *testing.T) {
	in, exc := writeConstantFixture(t, horizonExclusions)
	// alice is off 3 global + 2 personal days (works 5); bob works 7.
	assertConfidenceRows(t, runSim(t, cmdSimItems, constantArgs(in, exc, "-engineers", "alice,bob", "-days", "10")), "12")
}

func TestCmdSimItems_AnonymousIgnoresNamedExclusions(t *testing.T) {
	in, exc := writeConstantFixture(t, horizonExclusions)
	assertConfidenceRows(t, runSim(t, cmdSimItems, constantArgs(in, exc, "-engineers", "2", "-days", "10")), "14")
}

func TestCmdSimDays_HorizonExclusionsAddCalendarDays(t *testing.T) {
	in, exc := writeConstantFixture(t, horizonExclusions)

	plain := runSim(t, cmdSimDays, constantArgs(in, "", "-engineers", "2", "-items", "20"))
	if !regexp.MustCompile(`(?m)^50%\s+10\s+2025-03-12`).MatchString(plain) {
		t.Errorf("no exclusions: want 10 days / 2025-03-12, got:\n%s", plain)
	}
	with := runSim(t, cmdSimDays, constantArgs(in, exc, "-engineers", "2", "-items", "20"))
	if !regexp.MustCompile(`(?m)^50%\s+13\s+2025-03-15`).MatchString(with) {
		t.Errorf("with exclusions: want 13 days / 2025-03-15, got:\n%s", with)
	}
}

func TestCmdSimProbability_HorizonExclusions(t *testing.T) {
	in, exc := writeConstantFixture(t, horizonExclusions)

	at14 := runSim(t, cmdSimProbability, constantArgs(in, exc, "-engineers", "2", "-days", "10", "-items", "14"))
	if !strings.Contains(at14, "100.0%") {
		t.Errorf("-items 14 should be certain, got:\n%s", at14)
	}
	at15 := runSim(t, cmdSimProbability, constantArgs(in, exc, "-engineers", "2", "-days", "10", "-items", "15"))
	if !strings.Contains(at15, "0.0%") || strings.Contains(at15, "100.0%") {
		t.Errorf("-items 15 should be impossible, got:\n%s", at15)
	}
}

func TestCmdSimItems_SampleExclusionsInManifest(t *testing.T) {
	in, exc := writeConstantFixture(t, `{"global":["2025-01-05"]}`)
	out := runSim(t, cmdSimItems, constantArgs(in, exc, "-engineers", "1", "-days", "5", "-manifest", "-"))

	// The manifest is the first JSON object on stdout; the table follows it.
	var m struct {
		SchemaVersion int `json:"schema_version"`
		Pool          struct {
			PerEngineerSampleDays map[string]int `json:"per_engineer_sample_days"`
		} `json:"pool"`
		Data struct {
			Exclusions struct {
				Path              string `json:"path"`
				SampleDaysDropped struct {
					Global []string `json:"global"`
				} `json:"sample_days_dropped"`
			} `json:"exclusions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&m); err != nil {
		t.Fatalf("decoding manifest: %v\n%s", err, out)
	}
	if m.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", m.SchemaVersion)
	}
	if got := m.Pool.PerEngineerSampleDays["alice"]; got != 9 {
		t.Errorf("per_engineer_sample_days[alice] = %d, want 9 (10 days minus the excluded one)", got)
	}
	if got := m.Data.Exclusions.SampleDaysDropped.Global; !reflect.DeepEqual(got, []string{"2025-01-05"}) {
		t.Errorf("sample_days_dropped.global = %v", got)
	}
	if m.Data.Exclusions.Path != exc {
		t.Errorf("exclusions.path = %q, want %q", m.Data.Exclusions.Path, exc)
	}
}
