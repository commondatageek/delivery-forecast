package app

import (
	"reflect"
	"testing"

	"github.com/commondatageek/delivery-forecast/issues"
)

func TestSummarize(t *testing.T) {
	items := []issues.Issue{
		{Identifier: "A-1", TeamKey: "ENG", Assignee: "bob", ProjectName: "Zeta", ProjectMilestoneName: "M2", CreatedAt: localDay(2025, 2, 1), UpdatedAt: localDay(2025, 3, 1)},
		{Identifier: "A-2", TeamKey: "ENG", Assignee: "alice", ProjectName: "Zeta", ProjectMilestoneName: "M1", CreatedAt: localDay(2025, 1, 1), UpdatedAt: localDay(2025, 4, 1)},
		{Identifier: "B-1", TeamKey: "DATA", ProjectName: "Alpha", CreatedAt: localDay(2025, 1, 5), UpdatedAt: localDay(2025, 2, 1)},
		{Identifier: "B-2", TeamKey: "DATA"},
	}
	s := Summarize(items)

	if s.Issues != 4 {
		t.Errorf("Issues = %d", s.Issues)
	}
	if want := []string{"DATA", "ENG"}; !reflect.DeepEqual(s.Teams, want) {
		t.Errorf("Teams = %v, want %v", s.Teams, want)
	}
	wantProjects := []ProjectSummary{{Name: "Alpha"}, {Name: "Zeta", Milestones: []string{"M1", "M2"}}}
	if !reflect.DeepEqual(s.Projects, wantProjects) {
		t.Errorf("Projects = %+v, want %+v", s.Projects, wantProjects)
	}
	if want := []string{"alice", "bob"}; !reflect.DeepEqual(s.Assignees, want) {
		t.Errorf("Assignees = %v, want %v", s.Assignees, want)
	}
	if !s.EarliestCreated.Equal(localDay(2025, 1, 1)) || !s.LatestUpdated.Equal(localDay(2025, 4, 1)) {
		t.Errorf("created/updated = %v / %v", s.EarliestCreated, s.LatestUpdated)
	}
	if len(s.Readiness) == 0 {
		t.Error("Readiness is empty")
	}

	empty := Summarize(nil)
	if empty.Issues != 0 || !empty.EarliestCreated.IsZero() || !empty.LatestUpdated.IsZero() {
		t.Errorf("empty summary = %+v", empty)
	}
}
