package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
)

// backtestIssues mirrors the cmd/forecast backtest fixture: three live Foo
// issues (two completed, one open) and one canceled one.
func backtestIssues() []issues.Issue {
	return []issues.Issue{
		{Identifier: "E-1", Assignee: "alice", ProjectName: "Foo", Status: issues.StatusCompleted, CreatedAt: localDay(2025, 1, 1), StartedAt: localDay(2025, 1, 2), CompletedAt: localDay(2025, 1, 5)},
		{Identifier: "E-2", Assignee: "alice", ProjectName: "Foo", MilestoneName: "M1", Status: issues.StatusCompleted, CreatedAt: localDay(2025, 1, 2), StartedAt: localDay(2025, 1, 3), CompletedAt: localDay(2025, 1, 9)},
		{Identifier: "E-3", Assignee: "alice", ProjectName: "Foo", Status: issues.StatusStarted, CreatedAt: localDay(2025, 1, 3), StartedAt: localDay(2025, 1, 4)},
		{Identifier: "E-4", Assignee: "alice", ProjectName: "Foo", Status: issues.StatusCanceled, CreatedAt: localDay(2025, 1, 4), StartedAt: localDay(2025, 1, 5), CanceledAt: localDay(2025, 1, 6)},
	}
}

func backtestRequest() BacktestRequest {
	b := constantBase()
	b.Issues = backtestIssues()
	b.SampleStart, b.SampleEnd = localDay(2025, 1, 1), localDay(2025, 2, 1)
	return BacktestRequest{
		SimBase:     b,
		Project:     "Foo",
		ReplayStart: localDay(2025, 1, 1),
		TargetEnd:   localDay(2025, 1, 15),
		Today:       localDay(2025, 1, 10),
	}
}

func TestSimBacktest_ExcludesCanceled(t *testing.T) {
	req := backtestRequest()
	req.TargetEnd = localDay(2025, 1, 4)
	res, err := SimBacktest(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IssueCount != 3 || res.Scope != "Foo" {
		t.Errorf("IssueCount=%d Scope=%q, want 3 and Foo", res.IssueCount, res.Scope)
	}
	if len(res.Rows) == 0 {
		t.Fatal("no rows")
	}
	for _, r := range res.Rows {
		if r.Remaining > 3 {
			t.Errorf("row %v remaining includes the canceled issue", r)
		}
	}
}

func TestSimBacktest_InfersReplayStart(t *testing.T) {
	req := backtestRequest()
	req.ReplayStart = time.Time{}
	res, err := SimBacktest(req)
	if err != nil {
		t.Fatal(err)
	}
	// The earliest started_at in the backtested set is 2025-01-02.
	if !res.ReplayStartInferred || res.ReplayStart.Format("2006-01-02") != "2025-01-02" {
		t.Errorf("ReplayStart = %v inferred=%v, want 2025-01-02 inferred", res.ReplayStart, res.ReplayStartInferred)
	}

	explicit, err := SimBacktest(backtestRequest())
	if err != nil {
		t.Fatal(err)
	}
	if explicit.ReplayStartInferred || explicit.ReplayStart.Format("2006-01-02") != "2025-01-01" {
		t.Errorf("explicit start: got %v inferred=%v", explicit.ReplayStart, explicit.ReplayStartInferred)
	}
}

func TestSimBacktest_ProjectedFlag(t *testing.T) {
	res, err := SimBacktest(backtestRequest())
	if err != nil {
		t.Fatal(err)
	}
	var projected int
	for _, r := range res.Rows {
		want := r.Date.After(localDay(2025, 1, 10))
		if r.Projected != want {
			t.Errorf("%s: Projected = %v, want %v", r.Date.Format("2006-01-02"), r.Projected, want)
		}
		if r.Projected {
			projected++
		}
	}
	if projected == 0 || projected == len(res.Rows) {
		t.Errorf("expected a mix of replayed and projected rows, got %d projected of %d", projected, len(res.Rows))
	}
}

func TestSimBacktest_UnknownProjectFieldError(t *testing.T) {
	cases := []struct {
		name       string
		project    string
		milestone  string
		wantMsg    string
		wantFields []string
	}{
		{"project", "Nope", "", `no issues found for project "Nope" — check spelling`, []string{"project"}},
		{"milestone", "Foo", "Nope", `no issues found for project "Foo" milestone "Nope" — check spelling`, []string{"project", "milestone"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := backtestRequest()
			req.Project, req.Milestone = c.project, c.milestone
			res, err := SimBacktest(req)
			var fe *FieldError
			if !errors.As(err, &fe) || fe.Msg != c.wantMsg || !reflect.DeepEqual(fe.Fields, c.wantFields) {
				t.Fatalf("err = %v, want FieldError{%v, %q}", err, c.wantFields, c.wantMsg)
			}
			if res == nil {
				t.Error("result must be non-nil alongside an error")
			}
		})
	}
}

func TestSimBacktest_TargetEndBeforeStart(t *testing.T) {
	req := backtestRequest()
	req.TargetEnd = localDay(2025, 1, 1)
	_, err := SimBacktest(req)
	var fe *FieldError
	if !errors.As(err, &fe) || !strings.HasPrefix(fe.Msg, "-target-end-date must be after -replay-start-date (inferred: 2025-01-01)") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(fe.Fields, []string{"target-end-date", "replay-start-date"}) {
		t.Errorf("Fields = %v", fe.Fields)
	}
}

func TestSimBacktest_ValidateOrder(t *testing.T) {
	req := backtestRequest()
	req.Project = ""
	req.TargetEnd = time.Time{}
	req.Engineers = 0
	err := req.Validate()
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Msg != "-project is required" {
		t.Fatalf("err = %v, want the project error first", err)
	}

	req.Project = "Foo"
	err = req.Validate()
	if !errors.As(err, &fe) || fe.Msg != "-target-end-date is required" {
		t.Fatalf("err = %v, want the target-end error second", err)
	}

	req.TargetEnd = localDay(2025, 1, 15)
	err = req.Validate()
	if !errors.As(err, &fe) || !strings.Contains(fe.Msg, "-engineers or -whole-team") {
		t.Fatalf("err = %v, want the mode error third", err)
	}
}
