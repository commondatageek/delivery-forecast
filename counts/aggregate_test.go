package counts

import "testing"

func findCount(t *testing.T, rows []ProjectMilestoneCount, project, milestone string) (ProjectMilestoneCount, bool) {
	t.Helper()
	for _, r := range rows {
		if r.ProjectName == project && r.MilestoneName == milestone {
			return r, true
		}
	}
	return ProjectMilestoneCount{}, false
}

func TestAggregate_ExcludesTerminalStates(t *testing.T) {
	all := []Issue{
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "Alpha", ProjectMilestoneName: "M1", StateType: "started", UpdatedAt: mustTime("2024-03-01")},
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "Alpha", ProjectMilestoneName: "M1", StateType: "backlog", UpdatedAt: mustTime("2024-03-02")},
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "Alpha", ProjectMilestoneName: "M1", StateType: "completed", UpdatedAt: mustTime("2024-03-03")},
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "Alpha", ProjectMilestoneName: "M1", StateType: "canceled", UpdatedAt: mustTime("2024-03-04")},
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "Alpha", ProjectMilestoneName: "M1", StateType: "duplicate", UpdatedAt: mustTime("2024-03-05")},
	}

	pmCounts, activity := Aggregate(all)

	row, ok := findCount(t, pmCounts, "Alpha", "M1")
	if !ok {
		t.Fatal("expected a count row for Alpha/M1")
	}
	if row.Count != 2 {
		t.Errorf("Count = %d, want 2 (only started+backlog are non-terminal)", row.Count)
	}

	// Activity spans ALL issues, including terminal ones: the most recent
	// updated_at (2024-03-05, the duplicate) must still be reflected.
	if len(activity) != 1 {
		t.Fatalf("got %d activity rows, want 1", len(activity))
	}
	if !activity[0].LastUpdated.Equal(mustTime("2024-03-05")) {
		t.Errorf("LastUpdated = %v, want 2024-03-05 (terminal issues still count toward recency)", activity[0].LastUpdated)
	}
}

func TestAggregate_GroupsByTeamProjectMilestone(t *testing.T) {
	all := []Issue{
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "Alpha", ProjectMilestoneName: "M1", StateType: "started", UpdatedAt: mustTime("2024-01-01")},
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "Alpha", ProjectMilestoneName: "M1", StateType: "started", UpdatedAt: mustTime("2024-01-02")},
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "Alpha", ProjectMilestoneName: "M2", StateType: "started", UpdatedAt: mustTime("2024-01-03")},
		{TeamKey: "DATA", TeamName: "Data", ProjectName: "Alpha", ProjectMilestoneName: "M1", StateType: "started", UpdatedAt: mustTime("2024-01-04")},
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "", ProjectMilestoneName: "", StateType: "started", UpdatedAt: mustTime("2024-01-05")},
	}

	pmCounts, activity := Aggregate(all)

	engM1, ok := findCount(t, pmCounts, "Alpha", "M1")
	if !ok || engM1.Count != 2 || engM1.TeamKey != "ENG" {
		t.Errorf("ENG/Alpha/M1 = %+v, ok=%v, want Count=2 TeamKey=ENG", engM1, ok)
	}
	engM2, ok := findCount(t, pmCounts, "Alpha", "M2")
	if !ok || engM2.Count != 1 {
		t.Errorf("ENG/Alpha/M2 = %+v, ok=%v, want Count=1", engM2, ok)
	}

	// DATA's Alpha/M1 must be a distinct row from ENG's, not merged with it
	// (findCount only matches on project+milestone, so check team directly).
	var dataRows, engRows int
	for _, r := range pmCounts {
		if r.ProjectName == "Alpha" && r.MilestoneName == "M1" {
			if r.TeamKey == "DATA" {
				dataRows++
				if r.Count != 1 {
					t.Errorf("DATA/Alpha/M1 Count = %d, want 1", r.Count)
				}
			}
			if r.TeamKey == "ENG" {
				engRows++
			}
		}
	}
	if dataRows != 1 || engRows != 1 {
		t.Errorf("expected exactly one ENG and one DATA row for Alpha/M1, got engRows=%d dataRows=%d", engRows, dataRows)
	}

	noProject, ok := findCount(t, pmCounts, "", "")
	if !ok || noProject.Count != 1 {
		t.Errorf("no-project row = %+v, ok=%v, want Count=1", noProject, ok)
	}

	if len(activity) != 3 { // ENG/Alpha, DATA/Alpha, ENG/""
		t.Errorf("got %d activity rows, want 3", len(activity))
	}
}

func TestAggregate_EndToEndThroughCompute(t *testing.T) {
	all := []Issue{
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "Alpha", ProjectMilestoneName: "M1", StateType: "started", UpdatedAt: mustTime("2024-03-01")},
		{TeamKey: "ENG", TeamName: "Engineering", ProjectName: "Beta", ProjectMilestoneName: "", StateType: "backlog", UpdatedAt: mustTime("2024-01-01")},
	}
	pmCounts, activity := Aggregate(all)
	projects, total := Compute(pmCounts, activity, mustTime("2024-02-01"))

	if total != 1 {
		t.Errorf("total = %d, want 1 (Beta predates -updated-since and should be dropped)", total)
	}
	if len(projects) != 1 || projects[0].Name != "Alpha" {
		t.Fatalf("projects = %+v, want just Alpha", projects)
	}
}
