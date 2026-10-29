package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

func TestCheckResults_EmptyInput(t *testing.T) {
	got := CheckReadiness(nil)
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
		{Identifier: "E-1", Assignee: "alice", Status: issues.StatusCompleted, CreatedAt: day(2025, 1, 1), StartedAt: day(2025, 1, 2), CompletedAt: day(2025, 1, 5), UpdatedAt: day(2025, 1, 5)},
		{Identifier: "E-2", Assignee: "bob", Status: issues.StatusStarted, CreatedAt: day(2025, 1, 3), StartedAt: day(2025, 1, 4), UpdatedAt: day(2025, 1, 4)},
	}
	for _, r := range CheckReadiness(items) {
		if r.Status != "ok" {
			t.Errorf("%s: Status = %q, want %q", r.Command, r.Status, "ok")
		}
	}
}

func TestCheckResults_FlagsEachGap(t *testing.T) {
	items := []issues.Issue{
		// Clean, complete issue.
		{Identifier: "E-1", Assignee: "alice", Status: issues.StatusCompleted, CreatedAt: day(2025, 1, 1), StartedAt: day(2025, 1, 2), CompletedAt: day(2025, 1, 5), UpdatedAt: day(2025, 1, 5)},
		// Completed but unassigned: hits sim only.
		{Identifier: "E-2", Status: issues.StatusCompleted, CreatedAt: day(2025, 1, 1), StartedAt: day(2025, 1, 2), CompletedAt: day(2025, 1, 6), UpdatedAt: day(2025, 1, 6)},
		// Completed but never started: hits aging only.
		{Identifier: "E-3", Assignee: "bob", Status: issues.StatusCompleted, CreatedAt: day(2025, 1, 1), CompletedAt: day(2025, 1, 7), UpdatedAt: day(2025, 1, 7)},
		// No created_at, and in progress (not completed, so aging/sim untouched): hits history/cfd only.
		{Identifier: "E-4", Assignee: "carol", Status: issues.StatusStarted, StartedAt: day(2025, 1, 4), UpdatedAt: day(2025, 1, 4)},
		// No updated_at, otherwise clean: hits count only.
		{Identifier: "E-5", Assignee: "dave", Status: issues.StatusBacklog, CreatedAt: day(2025, 1, 1)},
	}

	want := map[string]string{
		"history": "1 issue has no created_at and will be excluded",
		"cfd":     "1 issue has no created_at and will be excluded",
		"aging":   "1 completed issue has no started_at and will be excluded from the cycle-time distribution",
		"count":   "1 issue has no updated_at; a project is hidden unless one of its issues was updated since -updated-since",
		"sim":     "1 completed issue has no assignee and will be excluded",
	}
	for _, r := range CheckReadiness(items) {
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

func TestCheckExclusions_ReportFields(t *testing.T) {
	exc, err := simulate.ParseExclusions([]byte(`{
		"global": ["2026-10-01", "2026-10-20"],
		"engineers": {"alice": ["2026-10-07"], "zed": ["2026-11-02/2026-11-03"]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	today := day(2026, 10, 7)
	r := CheckExclusions(exc, nil, map[string]bool{"alice": true}, today)

	if r.ParseErr != nil || r.GlobalEntries != 2 || !r.HasDates {
		t.Fatalf("report = %+v", r)
	}
	if want := []ExclusionsEngineer{{"alice", 1}, {"zed", 1}}; !reflect.DeepEqual(r.Engineers, want) {
		t.Errorf("Engineers = %+v, want %+v", r.Engineers, want)
	}
	if r.First.Format("2006-01-02") != "2026-10-01" || r.Last.Format("2006-01-02") != "2026-11-03" {
		t.Errorf("span = %v .. %v", r.First, r.Last)
	}
	// Oct 1 and Oct 7 are on or before today; Oct 20, Nov 2, Nov 3 are after.
	if r.Past != 2 || r.Future != 3 {
		t.Errorf("Past=%d Future=%d, want 2 and 3", r.Past, r.Future)
	}
	if !reflect.DeepEqual(r.UnmatchedNames, []string{"zed"}) || !r.Today.Equal(today) {
		t.Errorf("UnmatchedNames=%v Today=%v", r.UnmatchedNames, r.Today)
	}

	bad := CheckExclusions(simulate.Exclusions{}, errors.New("boom"), nil, today)
	if bad.ParseErr == nil || bad.GlobalEntries != 0 || bad.HasDates {
		t.Errorf("parse error report = %+v", bad)
	}
}

func TestAssignees(t *testing.T) {
	got := Assignees([]issues.Issue{{Assignee: "alice"}, {Assignee: ""}, {Assignee: "alice"}, {Assignee: "bob"}})
	if want := map[string]bool{"alice": true, "bob": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("Assignees = %v, want %v", got, want)
	}
}

func TestCheckReadiness_ExcludedCounts(t *testing.T) {
	items := []issues.Issue{
		{Identifier: "E-1", Status: issues.StatusCompleted, CompletedAt: day(2025, 1, 5)},
		{Identifier: "E-2", Status: issues.StatusCompleted, CompletedAt: day(2025, 1, 6)},
	}
	for _, r := range CheckReadiness(items) {
		want := map[string]int{"history": 2, "cfd": 2, "aging": 2, "count": 2, "sim": 2}[r.Command]
		if r.Excluded != want {
			t.Errorf("%s: Excluded = %d, want %d", r.Command, r.Excluded, want)
		}
	}
}
