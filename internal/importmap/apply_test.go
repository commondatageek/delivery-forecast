package importmap

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
)

// jiraMapping is the shipped Jira Cloud template (§6.1 of
// docs/plans/CSV_IMPORT_PLAN.md) with timezone UTC, so expectations don't
// depend on the machine's zone.
const jiraMapping = `version: 1
source: jira
time_format: "02/Jan/06 3:04 PM"
timezone: UTC
columns:
  identifier: Issue key
  title: Summary
  assignee: Assignee
  team_key: Project key
  team_name: Project name
  project_id: Parent key
  project_name: Parent summary
  milestone_name: {column: Fix versions, pick: first}
  issue_type: Issue Type
  parent_identifier: Parent key
  status_name: Status
  resolution: Resolution
  created_at: Created
  updated_at: Updated
status:
  column: Status category
  values:
    To Do: backlog
    In Progress: started
    Done: completed
  overrides:
    Won't Do: canceled
    Wont Do: canceled
    Canceled: canceled
    Cancelled: canceled
    Declined: canceled
    Duplicate: canceled
  resolution_overrides:
    Won't Do: canceled
    Won't Fix: canceled
    Duplicate: canceled
    Cannot Reproduce: canceled
    Declined: canceled
timestamps:
  started_at: {columns: [Status category changed], when: started}
  completed_at: {columns: [Resolved, Status category changed], when: completed}
  canceled_at: {columns: [Resolved, Status category changed], when: canceled}
exclude:
  Issue Type: [Epic]
detect: [Issue key, Status category, Summary]
`

func mustParse(t *testing.T, yaml string) *Mapping {
	t.Helper()
	m, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

func readFixture(t *testing.T) ([]string, [][]string) {
	t.Helper()
	f, err := os.Open("../../testdata/jira-export-sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header, rows, err := ReadCSV(f)
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	return header, rows
}

func utc(day, month, hh, mm int) time.Time {
	return time.Date(2026, time.Month(month), day, hh, mm, 0, 0, time.UTC)
}

func TestReadCSV_Fixture(t *testing.T) {
	header, rows := readFixture(t)
	if header[0] != "Summary" {
		t.Errorf("header[0] = %q, want the BOM stripped", header[0])
	}
	if len(header) != 26 || len(rows) != 12 {
		t.Fatalf("got %d columns, %d rows; want 26, 12", len(header), len(rows))
	}
	if rows[10][24] != "First line\nsecond line" {
		t.Errorf("multi-line comment = %q", rows[10][24])
	}
}

func TestReadCSV_RaggedRowIsAnError(t *testing.T) {
	if _, _, err := ReadCSV(strings.NewReader("a,b\n1,2,3\n")); err == nil {
		t.Fatal("want an error for a row wider than the header")
	}
}

func TestApply_JiraFixture(t *testing.T) {
	header, rows := readFixture(t)
	got, rep, err := Apply(mustParse(t, jiraMapping), header, rows)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	byID := map[string]issues.Issue{}
	var ids []string
	for _, it := range got {
		byID[it.Identifier] = it
		ids = append(ids, it.Identifier)
		if it.Source != "jira" {
			t.Errorf("%s: Source = %q", it.Identifier, it.Source)
		}
	}
	wantIDs := []string{"DEMO-2", "DEMO-3", "DEMO-4", "DEMO-5", "DEMO-6", "DEMO-7", "DEMO-8", "DEMO-9", "DEMO-10", "DEMO-11", "DEMO-12"}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("imported %v, want %v (file order, epic excluded)", ids, wantIDs)
	}

	want2 := issues.Issue{
		Source: "jira", Identifier: "DEMO-2", Title: "Cart page layout", Assignee: "Alice Example",
		TeamKey: "DEMO", TeamName: "Demo Delivery", ProjectID: "DEMO-1", ProjectName: "Checkout revamp",
		MilestoneName: "Release 1", IssueType: "Story", ParentIdentifier: "DEMO-1",
		Status: issues.StatusCompleted, StatusName: "Done", Resolution: "Done",
		CreatedAt: utc(2, 9, 9, 30), CompletedAt: utc(10, 10, 14, 45), UpdatedAt: utc(10, 10, 15, 0),
	}
	if g := byID["DEMO-2"]; !reflect.DeepEqual(g, want2) {
		t.Errorf("DEMO-2:\ngot  %+v\nwant %+v", g, want2)
	}

	if g := byID["DEMO-3"]; g.Status != issues.StatusCompleted || !g.CompletedAt.Equal(utc(8, 10, 9, 15)) || g.Resolution != "" {
		t.Errorf("DEMO-3 = %+v, want completed at 2026-10-08 09:15 from Status category changed", g)
	}
	if g := byID["DEMO-4"]; g.Status != issues.StatusCanceled || !g.CanceledAt.Equal(utc(12, 10, 10, 0)) || !g.CompletedAt.IsZero() {
		t.Errorf("DEMO-4 = %+v, want canceled via override", g)
	}
	if g := byID["DEMO-5"]; g.Status != issues.StatusCanceled || !g.CanceledAt.Equal(utc(14, 10, 11, 25)) || !g.CompletedAt.IsZero() || g.Resolution != "Done" {
		t.Errorf("DEMO-5 = %+v, want canceled at 2026-10-14 11:25 from Resolved despite resolution Done", g)
	}
	if g := byID["DEMO-6"]; g.Status != issues.StatusStarted || !g.StartedAt.Equal(utc(13, 10, 9, 30)) {
		t.Errorf("DEMO-6 = %+v, want started 2026-10-13 09:30", g)
	}
	if g := byID["DEMO-7"]; g.Status != issues.StatusStarted || !g.CompletedAt.IsZero() || !g.StartedAt.Equal(utc(1, 10, 9, 0)) {
		t.Errorf("DEMO-7 = %+v, want started with no completed_at despite Resolved", g)
	}
	if g := byID["DEMO-8"]; g.Status != issues.StatusBacklog || g.Assignee != "" || !g.StartedAt.IsZero() {
		t.Errorf("DEMO-8 = %+v, want backlog, unassigned, no started_at", g)
	}
	if g := byID["DEMO-9"]; g.Status != issues.StatusBacklog {
		t.Errorf("DEMO-9 status = %q, want backlog", g.Status)
	}
	if g := byID["DEMO-10"]; g.Status != issues.StatusCompleted || g.IssueType != "Sub-task" || g.Assignee != "" || g.ParentIdentifier != "DEMO-2" || !g.CompletedAt.Equal(utc(30, 9, 15, 55)) {
		t.Errorf("DEMO-10 = %+v", g)
	}
	if g := byID["DEMO-11"]; g.Title != `Fix "Save" button, again` || g.Status != issues.StatusCompleted || g.MilestoneName != "" {
		t.Errorf("DEMO-11 = %+v", g)
	}
	if g := byID["DEMO-12"]; g.Status != issues.StatusBacklog {
		t.Errorf("DEMO-12 status = %q, want backlog", g.Status)
	}

	wantRep := Report{
		Rows: 12, Columns: 26, Imported: 11,
		Excluded: []ValueCount{{"Issue Type = Epic", 1}},
		Status: []StatusCount{
			{"backlog", 3, []ValueCount{{"To Do", 3}}},
			{"started", 2, []ValueCount{{"In Progress", 2}}},
			{"completed", 4, []ValueCount{{"Done", 4}}},
			{"canceled", 2, []ValueCount{{"won't do (override)", 1}, {"Wont Do (override)", 1}}},
		},
		Timestamps: []FieldSource{
			{"created_at", 11, []ValueCount{{"Created", 11}}},
			{"started_at", 2, []ValueCount{{"Status category changed", 2}}},
			{"completed_at", 4, []ValueCount{{"Resolved", 3}, {"Status category changed", 1}}},
			{"canceled_at", 2, []ValueCount{{"Resolved", 1}, {"Status category changed", 1}}},
			{"updated_at", 11, []ValueCount{{"Updated", 11}}},
		},
		Fields: []FieldFill{
			{"identifier", 11}, {"title", 11}, {"assignee", 9}, {"team_key", 11}, {"team_name", 11},
			{"project_id", 4}, {"project_name", 4}, {"milestone_name", 4}, {"issue_type", 11},
			{"parent_identifier", 4}, {"status_name", 11}, {"resolution", 5},
		},
	}
	if !reflect.DeepEqual(rep, wantRep) {
		t.Errorf("report:\ngot  %+v\nwant %+v", rep, wantRep)
	}
}

// csvRows builds header and rows from inline CSV text.
func csvRows(t *testing.T, s string) ([]string, [][]string) {
	t.Helper()
	h, r, err := ReadCSV(strings.NewReader(s))
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	return h, r
}

func applyErr(t *testing.T, yaml, csv string) []string {
	t.Helper()
	h, r := csvRows(t, csv)
	got, _, err := Apply(mustParse(t, yaml), h, r)
	var de *DataError
	if !errors.As(err, &de) {
		t.Fatalf("Apply error = %v, want *DataError", err)
	}
	if got != nil {
		t.Fatalf("Apply returned %d issues alongside a DataError", len(got))
	}
	return de.Problems
}

const small = "version: 1\nsource: t\ncolumns:\n  identifier: Key\n"

func TestApply_HeaderProblems(t *testing.T) {
	probs := applyErr(t, small+"  title: Sumary\n  milestone_name: Fix versions\n  team_key: Nope\n",
		"Key,Summary,Fix versions,Fix versions\nA-1,x,v1,v2\n")
	want := []string{
		`columns.title: no column "Sumary" in the file (closest: "Summary")`,
		`columns.milestone_name: column "Fix versions" appears 2 times; add pick: first, last or join`,
		`columns.team_key: no column "Nope" in the file`,
	}
	if !reflect.DeepEqual(probs, want) {
		t.Fatalf("problems:\n%s\nwant\n%s", strings.Join(probs, "\n"), strings.Join(want, "\n"))
	}
}

func TestApply_HeaderMatchingIgnoresCaseAndSpace(t *testing.T) {
	h, r := csvRows(t, " key ,SUMMARY\nA-1,x\n")
	got, _, err := Apply(mustParse(t, small+"  title: summary\n"), h, r)
	if err != nil || got[0].Identifier != "A-1" || got[0].Title != "x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestApply_EmptyIdentifier(t *testing.T) {
	probs := applyErr(t, small, "Key,X\nA-1,a\n,b\nA-3,c\n  ,d\n")
	if !reflect.DeepEqual(probs, []string{"identifier: empty on 2 rows (rows 3, 5)"}) {
		t.Fatalf("problems = %q", probs)
	}
}

func TestApply_RepeatedIdentifier(t *testing.T) {
	probs := applyErr(t, small, "Key\nDEMO-1\nDEMO-3\nDEMO-2\nDEMO-3\n")
	if !reflect.DeepEqual(probs, []string{`identifier: "DEMO-3" appears on rows 3 and 5`}) {
		t.Fatalf("problems = %q", probs)
	}
}

func TestApply_BadTimestamp(t *testing.T) {
	probs := applyErr(t, small+"  created_at: Created\ntime_format: \"02/Jan/06 3:04 PM\"\n",
		"Key,Created\nA-1,01/Sep/26 9:00 AM\nA-2,2026-10-22\nA-3,\nA-4,nope\nA-5,yesterday\n")
	want := []string{`created_at (column "Created"): 3 values match no time_format, e.g. row 3 "2026-10-22"`}
	if !reflect.DeepEqual(probs, want) {
		t.Fatalf("problems = %q, want %q", probs, want)
	}
}

func TestApply_UnmatchedStatus(t *testing.T) {
	yaml := small + "status:\n  column: Cat\n  values:\n    Done: completed\n"
	probs := applyErr(t, yaml, "Key,Cat\nA-1,Done\nA-2,Paused\nA-3,On Hold\nA-4,paused\n")
	want := []string{`status: 3 rows match no rule: "Paused" (2, rows 3, 5), "On Hold" (1, row 4)`}
	if !reflect.DeepEqual(probs, want) {
		t.Fatalf("problems = %q, want %q", probs, want)
	}
}

func TestApply_UnmatchedStatusCapsExamples(t *testing.T) {
	yaml := small + "status:\n  column: Cat\n  values:\n    To Do: backlog\n"
	probs := applyErr(t, yaml, "Key,Cat\nA,Done\nB,Done\nC,Done\nD,Done\nE,Done\nF,Done\n")
	want := []string{`status: 6 rows match no rule: "Done" (6, rows 2, 3, 4, 5, 6)`}
	if !reflect.DeepEqual(probs, want) {
		t.Fatalf("problems = %q, want %q", probs, want)
	}
}

func TestApply_CollectsDifferentProblems(t *testing.T) {
	yaml := small + "status:\n  column: Cat\n  values:\n    Done: completed\n"
	probs := applyErr(t, yaml, "Key,Cat\n,Done\nA-2,Paused\n")
	if len(probs) != 2 || !strings.HasPrefix(probs[0], "identifier: empty") || !strings.HasPrefix(probs[1], "status: 1 row matches no rule") {
		t.Fatalf("problems = %q, want both the identifier and the status problem", probs)
	}
}

func TestApply_ExcludedRowsAreNotValidated(t *testing.T) {
	yaml := small + "status:\n  column: Cat\n  values:\n    Done: completed\nexclude:\n  Type: [epic]\n"
	h, r := csvRows(t, "Key,Cat,Type\nA-1,Done,Story\n,Weird,Epic\nA-1x,Done,EPIC\n")
	got, rep, err := Apply(mustParse(t, yaml), h, r)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(got) != 1 || !reflect.DeepEqual(rep.Excluded, []ValueCount{{"Type = Epic", 2}}) {
		t.Fatalf("got %d issues, excluded %+v", len(got), rep.Excluded)
	}
}

func TestApply_PickAndValue(t *testing.T) {
	csv := "Key,V,V,V\nA-1,,b,c\nA-2,a,,\nA-3,,,\n"
	for pick, want := range map[string][]string{
		"first": {"b", "a", ""},
		"last":  {"c", "a", ""},
		"join":  {"b, c", "a", ""},
	} {
		h, r := csvRows(t, csv)
		got, _, err := Apply(mustParse(t, small+"  milestone_name: {column: V, pick: "+pick+"}\n  team_key: {value: BSD}\n"), h, r)
		if err != nil {
			t.Fatalf("%s: %v", pick, err)
		}
		for i, w := range want {
			if got[i].MilestoneName != w || got[i].TeamKey != "BSD" {
				t.Errorf("pick %s row %d: milestone=%q team=%q, want %q/BSD", pick, i, got[i].MilestoneName, got[i].TeamKey, w)
			}
		}
	}
}

func TestApply_ColumnsFallback(t *testing.T) {
	h, r := csvRows(t, "Key,Dev,Assignee\nA-1,dana,alex\nA-2,,alex\n")
	got, _, err := Apply(mustParse(t, small+"  assignee: {columns: [Dev, Assignee]}\n"), h, r)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got[0].Assignee != "dana" || got[1].Assignee != "alex" {
		t.Fatalf("assignees = %q, %q", got[0].Assignee, got[1].Assignee)
	}
}

func TestApply_StatusRulePrecedence(t *testing.T) {
	yaml := small + `  status_name: Status
  resolution: Resolution
status:
  column: Cat
  values:
    Done: completed
    In Progress: started
  overrides:
    Shelved: backlog
  resolution_overrides:
    Duplicate: canceled
`
	// Row 2: override beats resolution and category. Row 3: resolution beats
	// category. Row 4: category. Row 5: empty resolution never matches.
	h, r := csvRows(t, "Key,Status,Resolution,Cat\nA-1,shelved,duplicate,Done\nA-2,Done,Duplicate,Done\nA-3,Done,Fixed,Done\nA-4,Doing,,In Progress\n")
	got, rep, err := Apply(mustParse(t, yaml), h, r)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []issues.Status{issues.StatusBacklog, issues.StatusCanceled, issues.StatusCompleted, issues.StatusStarted}
	for i, w := range want {
		if got[i].Status != w {
			t.Errorf("row %d: status %q, want %q", i+2, got[i].Status, w)
		}
	}
	wantStatus := []StatusCount{
		{"backlog", 1, []ValueCount{{"shelved (override)", 1}}},
		{"started", 1, []ValueCount{{"In Progress", 1}}},
		{"completed", 1, []ValueCount{{"Done", 1}}},
		{"canceled", 1, []ValueCount{{"Duplicate (resolution)", 1}}},
	}
	if !reflect.DeepEqual(rep.Status, wantStatus) {
		t.Errorf("report status = %+v", rep.Status)
	}
}

func TestApply_StatusColumnAndNoStatusRule(t *testing.T) {
	h, r := csvRows(t, "Key,S\nA-1,completed\nA-2,Started\n")
	got, _, err := Apply(mustParse(t, small+"  status: S\n"), h, r)
	if err != nil || got[0].Status != issues.StatusCompleted || got[1].Status != issues.StatusStarted {
		t.Fatalf("got %+v, %v", got, err)
	}
	probs := applyErr(t, small+"  status: S\n", "Key,S\nA-1,done\n")
	if !reflect.DeepEqual(probs, []string{`status: 1 row matches no rule: "done" (1, row 2)`}) {
		t.Fatalf("problems = %q", probs)
	}

	h, r = csvRows(t, "Key\nA-1\n")
	_, rep, err := Apply(mustParse(t, small), h, r)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if last := rep.Status[len(rep.Status)-1]; last.Status != "unknown" || last.N != 1 || len(rep.Status) != 5 {
		t.Fatalf("status report = %+v, want the four statuses at 0 plus unknown 1", rep.Status)
	}
}
