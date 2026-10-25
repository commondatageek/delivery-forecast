package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/simulate"
)

const checkFixtureCSV = `identifier,team_key,assignee,state_type,created_at,started_at,completed_at,updated_at
E-1,ENG,alice,completed,2025-01-01,2025-01-02,2025-01-05,2025-01-05
E-2,ENG,,completed,2025-01-01,2025-01-02,2025-01-06,2025-01-06
E-3,ENG,bob,completed,2025-01-01,,2025-01-07,2025-01-07
E-4,ENG,carol,started,,2025-01-04,,
`

func TestCmdCheck_ReportsPerCommandGaps(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(checkFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := cmdCheck([]string{"-input", csvPath}); err != nil {
			t.Fatalf("cmdCheck: %v", err)
		}
	})

	if !strings.HasPrefix(out, "Read 4 issues from "+csvPath+"\n") {
		t.Errorf("output header = %q", out)
	}
	for _, want := range []string{
		"  history   1 issue has no created_at and will be excluded\n",
		"  cfd       1 issue has no created_at and will be excluded\n",
		"  aging     1 completed issue has no started_at and will be excluded from the cycle-time distribution\n",
		"  count     1 issue has no updated_at; a project is hidden unless one of its issues was updated since -updated-since\n",
		"  sim       1 completed issue has no assignee and will be excluded\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing line %q; got:\n%s", want, out)
		}
	}
}

func TestCmdCheck_RequiresInput(t *testing.T) {
	if err := cmdCheck(nil); err == nil {
		t.Fatal("cmdCheck with no -input = nil error, want one")
	}
}

func TestCheckExclusions_Invalid(t *testing.T) {
	got := checkExclusionsLines(app.CheckExclusions(simulate.Exclusions{}, errors.New("exclusions: global[0]: bad"), nil, day(2026, 10, 7)))
	want := []string{"invalid   exclusions: global[0]: bad"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func mustExclusions(t *testing.T, s string) simulate.Exclusions {
	t.Helper()
	exc, err := simulate.ParseExclusions([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return exc
}

func TestCheckExclusions_Summary(t *testing.T) {
	exc := mustExclusions(t, `{
		"global": ["2026-10-01/2026-10-02", "2026-10-07", "2026-12-25"],
		"engineers": {"alice": ["2026-10-02", "2026-11-02/2026-11-03"], "bob": ["2026-09-01"]}
	}`)
	assignees := map[string]bool{"alice": true, "bob": true}
	got := checkExclusionsLines(app.CheckExclusions(exc, nil, assignees, day(2026, 10, 7)))
	want := []string{
		"entries   3 global, 2 engineers (alice: 2, bob: 1)",
		// distinct: 9/1, 10/1, 10/2, 10/7, 11/2, 11/3, 12/25 (10/2 shared) = 7;
		// past (<= today): 9/1, 10/1, 10/2, 10/7 = 4; future: 3.
		"span      2026-09-01 .. 2026-12-25 (4 dates past, 3 future, as of 2026-10-07)",
		"engineers ok",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestCheckExclusions_Empty(t *testing.T) {
	got := checkExclusionsLines(app.CheckExclusions(simulate.Exclusions{}, nil, nil, day(2026, 10, 7)))
	want := []string{"entries   0 global, 0 engineers", "span      none", "engineers ok"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCheckExclusions_UnmatchedName(t *testing.T) {
	exc := mustExclusions(t, `{"engineers": {"alice": ["2026-10-02"], "zed": ["2026-10-03"], "yan": ["2026-10-04"]}}`)
	got := checkExclusionsLines(app.CheckExclusions(exc, nil, map[string]bool{"alice": true}, day(2026, 10, 7)))
	for _, want := range []string{
		`engineers yan: no assignee named "yan" in the input (fine if yan is given to sim -engineers)`,
		`engineers zed: no assignee named "zed" in the input (fine if zed is given to sim -engineers)`,
	} {
		found := false
		for _, l := range got {
			found = found || l == want
		}
		if !found {
			t.Errorf("missing line %q in:\n%s", want, strings.Join(got, "\n"))
		}
	}
	for _, l := range got {
		if l == "engineers ok" {
			t.Errorf("should not report ok with unmatched names:\n%s", strings.Join(got, "\n"))
		}
	}
}

func TestCmdCheck_ExclusionsFile(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(checkFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	excPath := filepath.Join(dir, "exclusions.json")
	if err := os.WriteFile(excPath, []byte(`{"global": ["2025-12-25"], "engineers": {"alice": ["2026-03-02/2026-03-13"], "ghost": ["2026-01-01"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := cmdCheck([]string{"-input", csvPath, "-exclusions", excPath}); err != nil {
			t.Fatalf("cmdCheck: %v", err)
		}
	})
	for _, want := range []string{
		"Exclusions: " + excPath + "\n",
		"  entries   1 global, 2 engineers (alice: 1, ghost: 1)\n",
		`  engineers ghost: no assignee named "ghost" in the input`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "engineers alice") {
		t.Errorf("alice is an assignee and should not be flagged:\n%s", out)
	}
}

func TestCmdCheck_ExclusionsInvalidAndMissingDoNotFail(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "issues.csv")
	if err := os.WriteFile(csvPath, []byte(checkFixtureCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte(`{"global": ["2025-13-01"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{badPath, filepath.Join(dir, "missing.json")} {
		out := captureStdout(t, func() {
			if err := cmdCheck([]string{"-input", csvPath, "-exclusions", path}); err != nil {
				t.Fatalf("cmdCheck(-exclusions %s) = %v, want nil", path, err)
			}
		})
		if !strings.Contains(out, "  invalid   ") {
			t.Errorf("%s: want an invalid line, got:\n%s", path, out)
		}
	}
}
