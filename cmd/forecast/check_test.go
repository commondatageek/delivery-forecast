package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/issues"
)

func TestCheckResults_EmptyInput(t *testing.T) {
	got := checkResults(nil)
	if len(got) != 5 {
		t.Fatalf("len(results) = %d, want 5", len(got))
	}
	for _, r := range got {
		if r.Status != "no issues in input" {
			t.Errorf("%s: Status = %q, want %q", r.Command, r.Status, "no issues in input")
		}
	}
}

func TestCheckResults_AllClean(t *testing.T) {
	items := []issues.Issue{
		{Identifier: "E-1", Assignee: "alice", StateType: "completed", CreatedAt: day(2025, 1, 1), StartedAt: day(2025, 1, 2), CompletedAt: day(2025, 1, 5)},
		{Identifier: "E-2", Assignee: "bob", StateType: "started", CreatedAt: day(2025, 1, 3), StartedAt: day(2025, 1, 4)},
	}
	for _, r := range checkResults(items) {
		if r.Status != "ok" {
			t.Errorf("%s: Status = %q, want %q", r.Command, r.Status, "ok")
		}
	}
}

func TestCheckResults_FlagsEachGap(t *testing.T) {
	items := []issues.Issue{
		// Clean, complete issue.
		{Identifier: "E-1", Assignee: "alice", StateType: "completed", CreatedAt: day(2025, 1, 1), StartedAt: day(2025, 1, 2), CompletedAt: day(2025, 1, 5)},
		// Completed but unassigned: hits sim only.
		{Identifier: "E-2", StateType: "completed", CreatedAt: day(2025, 1, 1), StartedAt: day(2025, 1, 2), CompletedAt: day(2025, 1, 6)},
		// Completed but never started: hits aging only.
		{Identifier: "E-3", Assignee: "bob", StateType: "completed", CreatedAt: day(2025, 1, 1), CompletedAt: day(2025, 1, 7)},
		// No created_at, and in progress (not completed, so aging/sim untouched): hits history/cfd only.
		{Identifier: "E-4", Assignee: "carol", StateType: "started", StartedAt: day(2025, 1, 4)},
	}

	want := map[string]string{
		"history": "1 issue has no created_at and will be excluded",
		"cfd":     "1 issue has no created_at and will be excluded",
		"aging":   "1 completed issue has no started_at and will be excluded from the cycle-time distribution",
		"count":   "ok",
		"sim":     "1 completed issue has no assignee and will be excluded",
	}
	for _, r := range checkResults(items) {
		if got, ok := want[r.Command]; !ok || got != r.Status {
			t.Errorf("%s: Status = %q, want %q", r.Command, r.Status, want[r.Command])
		}
	}
}

func TestExcludedStatus_Pluralization(t *testing.T) {
	if got := excludedStatus(0, "issue", "no created_at and will be excluded"); got != "ok" {
		t.Errorf("n=0: got %q, want %q", got, "ok")
	}
	if got := excludedStatus(1, "issue", "no created_at and will be excluded"); got != "1 issue has no created_at and will be excluded" {
		t.Errorf("n=1: got %q", got)
	}
	if got := excludedStatus(12, "completed issue", "no assignee and will be excluded"); got != "12 completed issues have no assignee and will be excluded" {
		t.Errorf("n=12: got %q", got)
	}
}

const checkFixtureCSV = `identifier,team_key,assignee,state_type,created_at,started_at,completed_at
E-1,ENG,alice,completed,2025-01-01,2025-01-02,2025-01-05
E-2,ENG,,completed,2025-01-01,2025-01-02,2025-01-06
E-3,ENG,bob,completed,2025-01-01,,2025-01-07
E-4,ENG,carol,started,,2025-01-04,
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
		"  count     ok\n",
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
