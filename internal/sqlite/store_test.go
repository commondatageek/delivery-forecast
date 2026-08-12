package sqlite

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func TestLatestUpdatedAtForTeam(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	zero, err := store.LatestUpdatedAtForTeam(ctx, "ENG")
	if err != nil {
		t.Fatalf("LatestUpdatedAtForTeam on empty db: %v", err)
	}
	if !zero.IsZero() {
		t.Fatalf("LatestUpdatedAtForTeam on empty db = %v, want zero time", zero)
	}

	older := issues.Issue{Identifier: "ENG-1", TeamKey: "ENG", UpdatedAt: mustParse(t, "2024-01-01T00:00:00Z")}
	newer := issues.Issue{Identifier: "ENG-2", TeamKey: "ENG", UpdatedAt: mustParse(t, "2024-01-10T00:00:00Z")}
	otherTeam := issues.Issue{Identifier: "DATA-1", TeamKey: "DATA", UpdatedAt: mustParse(t, "2024-06-01T00:00:00Z")}
	if err := store.Upsert(ctx, older, newer, otherTeam); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.LatestUpdatedAtForTeam(ctx, "ENG")
	if err != nil {
		t.Fatalf("LatestUpdatedAtForTeam: %v", err)
	}
	if !got.Equal(newer.UpdatedAt) {
		t.Fatalf("LatestUpdatedAtForTeam(ENG) = %v, want %v (other teams' watermarks must not leak in)", got, newer.UpdatedAt)
	}
}

func TestDistinctTeamKeys(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Upsert(ctx,
		issues.Issue{Identifier: "ENG-1", TeamKey: "ENG"},
		issues.Issue{Identifier: "DATA-1", TeamKey: "DATA"},
		issues.Issue{Identifier: "ENG-2", TeamKey: "ENG"},
		issues.Issue{Identifier: "NOTEAM-1", TeamKey: ""},
	); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.DistinctTeamKeys(ctx)
	if err != nil {
		t.Fatalf("DistinctTeamKeys: %v", err)
	}
	want := []string{"DATA", "ENG"}
	if !slices.Equal(got, want) {
		t.Fatalf("DistinctTeamKeys = %v, want %v", got, want)
	}
}

func TestUpsertConflictUpdates(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, issues.Issue{Identifier: "ENG-1", Title: "first"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store.Upsert(ctx, issues.Issue{Identifier: "ENG-1", Title: "second"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE identifier = 'ENG-1'`).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count for ENG-1 = %d, want 1 (upsert should update, not duplicate)", count)
	}

	var title string
	if err := store.db.QueryRowContext(ctx, `SELECT title FROM issues WHERE identifier = 'ENG-1'`).Scan(&title); err != nil {
		t.Fatalf("title query: %v", err)
	}
	if title != "second" {
		t.Fatalf("title = %q, want %q", title, "second")
	}
}

func TestUpsertNullTimeRoundTrip(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, issues.Issue{Identifier: "ENG-1", StateType: "started", StartedAt: mustParse(t, "2024-01-01T00:00:00Z")}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.AllIssues(ctx)
	if err != nil {
		t.Fatalf("AllIssues: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("AllIssues returned %d issues, want 1", len(got))
	}
	if !got[0].CompletedAt.IsZero() {
		t.Fatalf("CompletedAt = %v, want zero time", got[0].CompletedAt)
	}
}

func TestProjectMilestoneIssues(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	issues := []issues.Issue{
		{Identifier: "ENG-1", ProjectName: "Apollo", ProjectMilestoneName: "v1.0", StateType: "started",
			CreatedAt: mustParse(t, "2024-01-01T00:00:00Z")},
		{Identifier: "ENG-2", ProjectName: "Apollo", ProjectMilestoneName: "v1.0", StateType: "completed",
			CreatedAt: mustParse(t, "2024-01-02T00:00:00Z"), CompletedAt: mustParse(t, "2024-01-10T00:00:00Z")},
		{Identifier: "ENG-3", ProjectName: "Apollo", ProjectMilestoneName: "v1.0", StateType: "canceled"},
		{Identifier: "ENG-4", ProjectName: "Apollo", ProjectMilestoneName: "v1.0", StateType: "duplicate"},
		{Identifier: "ENG-5", ProjectName: "Apollo", ProjectMilestoneName: "v2.0", StateType: "started"},
		{Identifier: "ENG-6", ProjectName: "Zeus", ProjectMilestoneName: "v1.0", StateType: "started"},
	}
	if err := store.Upsert(ctx, issues...); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Project-only: all non-canceled/dup Apollo issues across both milestones.
	got, err := store.ProjectMilestoneIssues(ctx, "Apollo", "")
	if err != nil {
		t.Fatalf("ProjectMilestoneIssues(Apollo, \"\"): %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("project-only = %d issues, want 3: %+v", len(got), got)
	}
	for _, it := range got {
		if it.StateType == "canceled" || it.StateType == "duplicate" {
			t.Errorf("excluded state_type returned: %+v", it)
		}
		if it.ProjectName != "Apollo" {
			t.Errorf("wrong project: %+v", it)
		}
	}

	// Milestone-scoped: only v1.0, excludes canceled/dup.
	gotMS, err := store.ProjectMilestoneIssues(ctx, "Apollo", "v1.0")
	if err != nil {
		t.Fatalf("ProjectMilestoneIssues(Apollo, v1.0): %v", err)
	}
	if len(gotMS) != 2 {
		t.Fatalf("milestone filter = %d issues, want 2: %+v", len(gotMS), gotMS)
	}
	ids := []string{gotMS[0].Identifier, gotMS[1].Identifier}
	if !slices.Contains(ids, "ENG-1") || !slices.Contains(ids, "ENG-2") {
		t.Errorf("milestone filter ids = %v, want ENG-1 and ENG-2", ids)
	}
	// completed_at round-trips.
	for _, it := range gotMS {
		if it.Identifier == "ENG-2" && it.CompletedAt.IsZero() {
			t.Errorf("ENG-2.CompletedAt is zero, want non-zero")
		}
	}
}

func TestUpsertStoresAbsentOptionalFieldsAsNull(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// An unassigned issue with no project/milestone — every optional field empty.
	if err := store.Upsert(ctx, issues.Issue{Identifier: "ENG-1", StateType: "started", StartedAt: mustParse(t, "2024-01-01T00:00:00Z")}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	const q = `SELECT
		assignee IS NULL, project_id IS NULL, project_name IS NULL,
		project_milestone_id IS NULL, project_milestone_name IS NULL
	FROM issues WHERE identifier = 'ENG-1'`

	var assigneeNull, projIDNull, projNameNull, msIDNull, msNameNull bool
	if err := store.db.QueryRowContext(ctx, q).Scan(
		&assigneeNull, &projIDNull, &projNameNull, &msIDNull, &msNameNull,
	); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !(assigneeNull && projIDNull && projNameNull && msIDNull && msNameNull) {
		t.Fatalf("optional fields stored as NULL = assignee:%v proj_id:%v proj_name:%v ms_id:%v ms_name:%v, want all true",
			assigneeNull, projIDNull, projNameNull, msIDNull, msNameNull)
	}

	// Round-trips back to empty strings on the Go side.
	got, err := store.AllIssues(ctx)
	if err != nil {
		t.Fatalf("AllIssues: %v", err)
	}
	if got[0].Assignee != "" || got[0].ProjectName != "" {
		t.Fatalf("NULL did not round-trip to empty string: %+v", got[0])
	}
}
