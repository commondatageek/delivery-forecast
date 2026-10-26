package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
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

func TestLatestUpdatedAt(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	zero, err := store.LatestUpdatedAt(ctx, "linear", "ENG")
	if err != nil {
		t.Fatalf("LatestUpdatedAt on empty db: %v", err)
	}
	if !zero.IsZero() {
		t.Fatalf("LatestUpdatedAt on empty db = %v, want zero time", zero)
	}

	older := issues.Issue{Source: "linear", Identifier: "ENG-1", TeamKey: "ENG", UpdatedAt: mustParse(t, "2024-01-01T00:00:00Z")}
	newer := issues.Issue{Source: "linear", Identifier: "ENG-2", TeamKey: "ENG", UpdatedAt: mustParse(t, "2024-01-10T00:00:00Z")}
	otherTeam := issues.Issue{Source: "linear", Identifier: "DATA-1", TeamKey: "DATA", UpdatedAt: mustParse(t, "2024-06-01T00:00:00Z")}
	otherSource := issues.Issue{Source: "jira", Identifier: "ENG-9", TeamKey: "ENG", UpdatedAt: mustParse(t, "2024-07-01T00:00:00Z")}
	if err := store.Upsert(ctx, older, newer, otherTeam, otherSource); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.LatestUpdatedAt(ctx, "linear", "ENG")
	if err != nil {
		t.Fatalf("LatestUpdatedAt: %v", err)
	}
	if !got.Equal(newer.UpdatedAt) {
		t.Fatalf("LatestUpdatedAt(linear, ENG) = %v, want %v (other teams' and sources' watermarks must not leak in)", got, newer.UpdatedAt)
	}
	got, err = store.LatestUpdatedAt(ctx, "jira", "ENG")
	if err != nil {
		t.Fatalf("LatestUpdatedAt: %v", err)
	}
	if !got.Equal(otherSource.UpdatedAt) {
		t.Fatalf("LatestUpdatedAt(jira, ENG) = %v, want %v", got, otherSource.UpdatedAt)
	}
}

func TestDistinctTeams(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Upsert(ctx,
		issues.Issue{Source: "linear", Identifier: "ENG-1", TeamKey: "ENG"},
		issues.Issue{Source: "linear", Identifier: "DATA-1", TeamKey: "DATA"},
		issues.Issue{Source: "linear", Identifier: "ENG-2", TeamKey: "ENG"},
		issues.Issue{Source: "linear", Identifier: "NOTEAM-1", TeamKey: ""},
		issues.Issue{Source: "jira", Identifier: "OPS-1", TeamKey: "OPS"},
	); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.DistinctTeams(ctx, "linear")
	if err != nil {
		t.Fatalf("DistinctTeams: %v", err)
	}
	want := []string{"DATA", "ENG"}
	if !slices.Equal(got, want) {
		t.Fatalf("DistinctTeams = %v, want %v", got, want)
	}
}

func TestSameIdentifierInTwoSources(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.Upsert(ctx,
		issues.Issue{Source: "linear", Identifier: "ENG-1", TeamKey: "ENG", Title: "from linear"},
		issues.Issue{Source: "jira", Identifier: "ENG-1", TeamKey: "ENG", Title: "from jira"},
	); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	all, err := store.AllIssues(ctx)
	if err != nil {
		t.Fatalf("AllIssues: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("AllIssues = %d rows, want 2 (same identifier, different sources)", len(all))
	}

	ids, err := store.Identifiers(ctx, "jira", "ENG")
	if err != nil {
		t.Fatalf("Identifiers: %v", err)
	}
	if !slices.Equal(ids, []string{"ENG-1"}) {
		t.Fatalf("Identifiers(jira, ENG) = %v, want [ENG-1]", ids)
	}

	if err := store.Delete(ctx, "jira", "ENG-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	all, err = store.AllIssues(ctx)
	if err != nil {
		t.Fatalf("AllIssues: %v", err)
	}
	if len(all) != 1 || all[0].Source != "linear" || all[0].Title != "from linear" {
		t.Fatalf("after deleting jira's ENG-1, AllIssues = %+v, want only linear's", all)
	}
}

func TestOpen_RefusesOtherSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	// A pre-versioning (v1) database: issues table, user_version 0.
	if _, err := db.Exec(`CREATE TABLE issues (identifier TEXT NOT NULL PRIMARY KEY, state_type TEXT)`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	db.Close()

	for name, open := range map[string]func(string) (*Store, error){"Open": Open, "OpenExisting": OpenExisting} {
		_, err := open(path)
		if err == nil {
			t.Fatalf("%s on a legacy db = nil error, want refusal", name)
		}
		want := "database schema is v0, this build needs v2; delete the file and create it again"
		if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), path) {
			t.Fatalf("%s error = %q, want it to name the path and contain %q", name, err, want)
		}
	}
}

func TestOpen_StampsNewDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var v int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	store.Close()
	if v != SchemaVersion {
		t.Fatalf("user_version = %d, want %d", v, SchemaVersion)
	}
	// Reopening the stamped database works.
	store, err = OpenExisting(path)
	if err != nil {
		t.Fatalf("OpenExisting on a v%d db: %v", SchemaVersion, err)
	}
	store.Close()
}

func TestUpsertRoundTripsEveryField(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	want := issues.Issue{
		Source: "linear", Identifier: "ENG-1", Title: "t", URL: "https://example.test/ENG-1",
		Assignee: "alice", TeamKey: "ENG", TeamName: "Engineering",
		ProjectID: "p1", ProjectName: "P", MilestoneID: "m1", MilestoneName: "M",
		IssueType: "Bug", ParentIdentifier: "ENG-0",
		Status: issues.StatusCanceled, StatusName: "Duplicate", Resolution: "duplicate",
		CreatedAt:  mustParse(t, "2024-01-01T00:00:00Z"),
		StartedAt:  mustParse(t, "2024-01-02T00:00:00Z"),
		CanceledAt: mustParse(t, "2024-01-03T00:00:00Z"),
		UpdatedAt:  mustParse(t, "2024-01-04T00:00:00Z"),
	}
	if err := store.Upsert(ctx, want); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := store.AllIssues(ctx)
	if err != nil {
		t.Fatalf("AllIssues: %v", err)
	}
	g := got[0]
	if !g.CreatedAt.Equal(want.CreatedAt) || !g.StartedAt.Equal(want.StartedAt) ||
		!g.CanceledAt.Equal(want.CanceledAt) || !g.UpdatedAt.Equal(want.UpdatedAt) || !g.CompletedAt.IsZero() {
		t.Fatalf("timestamps = %+v, want %+v", g, want)
	}
	g.CreatedAt, g.StartedAt, g.CanceledAt, g.UpdatedAt = want.CreatedAt, want.StartedAt, want.CanceledAt, want.UpdatedAt
	if g != want {
		t.Fatalf("round trip:\ngot  %+v\nwant %+v", g, want)
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

	if err := store.Upsert(ctx, issues.Issue{Identifier: "ENG-1", Status: issues.StatusStarted, StartedAt: mustParse(t, "2024-01-01T00:00:00Z")}); err != nil {
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

func TestUpsertStoresAbsentOptionalFieldsAsNull(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// An unassigned issue with no project/milestone — every optional field empty.
	if err := store.Upsert(ctx, issues.Issue{Identifier: "ENG-1", Status: issues.StatusStarted, StartedAt: mustParse(t, "2024-01-01T00:00:00Z")}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	const q = `SELECT
		assignee IS NULL, project_id IS NULL, project_name IS NULL,
		milestone_id IS NULL, milestone_name IS NULL
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

func sourceIDs(t *testing.T, store *Store) []string {
	t.Helper()
	all, err := store.AllIssues(context.Background())
	if err != nil {
		t.Fatalf("AllIssues: %v", err)
	}
	var out []string
	for _, it := range all {
		out = append(out, it.Source+"/"+it.Identifier)
	}
	slices.Sort(out)
	return out
}

func TestReplaceSource(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.Upsert(ctx,
		issues.Issue{Source: "jira", Identifier: "DEMO-1"},
		issues.Issue{Source: "jira", Identifier: "DEMO-2"},
		issues.Issue{Source: "linear", Identifier: "DEMO-1"},
	); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	deleted, err := store.ReplaceSource(ctx, "jira", []issues.Issue{
		{Source: "jira", Identifier: "DEMO-2", Title: "new"},
		{Source: "jira", Identifier: "DEMO-3"},
	})
	if err != nil {
		t.Fatalf("ReplaceSource: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	want := []string{"jira/DEMO-2", "jira/DEMO-3", "linear/DEMO-1"}
	if got := sourceIDs(t, store); !slices.Equal(got, want) {
		t.Fatalf("after replace: %v, want %v (DEMO-1 gone from jira, linear untouched)", got, want)
	}

	// Replacing into an empty source deletes nothing.
	deleted, err = store.ReplaceSource(ctx, "csv", []issues.Issue{{Source: "csv", Identifier: "X-1"}})
	if err != nil || deleted != 0 {
		t.Fatalf("ReplaceSource(csv) = %d, %v; want 0, nil", deleted, err)
	}
}

func TestReplaceSource_MismatchedSourceWritesNothing(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.Upsert(ctx, issues.Issue{Source: "jira", Identifier: "DEMO-1"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	_, err := store.ReplaceSource(ctx, "jira", []issues.Issue{
		{Source: "jira", Identifier: "DEMO-2"},
		{Source: "linear", Identifier: "ENG-1"},
	})
	if err == nil || !strings.Contains(err.Error(), `issue ENG-1 has source "linear"`) {
		t.Fatalf("err = %v, want a source mismatch", err)
	}
	if got := sourceIDs(t, store); !slices.Equal(got, []string{"jira/DEMO-1"}) {
		t.Fatalf("store changed: %v", got)
	}
}

func TestReplaceSource_InsertFailureRollsBackDelete(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.Upsert(ctx, issues.Issue{Source: "jira", Identifier: "DEMO-1"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// A trigger makes one insert fail after the delete has run.
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER no_bad BEFORE INSERT ON issues
		WHEN NEW.identifier = 'BAD' BEGIN SELECT RAISE(ABORT, 'bad row'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	_, err := store.ReplaceSource(ctx, "jira", []issues.Issue{
		{Source: "jira", Identifier: "DEMO-2"},
		{Source: "jira", Identifier: "BAD"},
	})
	if err == nil {
		t.Fatal("ReplaceSource = nil error, want the insert failure")
	}
	if got := sourceIDs(t, store); !slices.Equal(got, []string{"jira/DEMO-1"}) {
		t.Fatalf("after a failed replace the store is %v, want the original jira/DEMO-1", got)
	}
}
