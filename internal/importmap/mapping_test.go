package importmap

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
)

// exampleMapping is the plan's §4.1 example.
const exampleMapping = `version: 1                          # required; only 1 exists
source: jira                        # required; stored in source, part of the db key
format: csv                         # optional; only "csv"

time_format: "02/Jan/06 3:04 PM"    # required if any timestamp is mapped
timezone: America/Denver            # optional; default "Local"

columns:
  identifier:  Issue key
  title:       Summary
  assignee:    Assignee
  team_key:    Project key
  team_name:   Project name
  project_id:  Parent key
  project_name: Parent summary
  milestone_name: {column: Fix versions, pick: first}
  issue_type:  Issue Type
  parent_identifier: Parent key
  status_name: Status
  resolution:  Resolution
  created_at:  Created
  updated_at:  Updated

status:
  column: Status category
  values:
    To Do: backlog
    In Progress: started
    Done: completed
  overrides:
    won't do: canceled
    Wont Do: canceled
  resolution_overrides:
    Duplicate: canceled

timestamps:
  started_at:   {columns: [Status category changed], when: started}
  completed_at: {columns: [Resolved, Status category changed], when: completed}
  canceled_at:  {columns: [Resolved, Status category changed], when: canceled}

exclude:
  Issue Type: [Epic]

detect: [Issue key, Status category, Summary]
`

func TestParse_Example(t *testing.T) {
	m, err := Parse([]byte(exampleMapping))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Version != 1 || m.Source != "jira" {
		t.Errorf("Version/Source = %d/%q", m.Version, m.Source)
	}
	if !reflect.DeepEqual(m.TimeFormats, []string{"02/Jan/06 3:04 PM"}) {
		t.Errorf("TimeFormats = %q", m.TimeFormats)
	}
	if m.Location.String() != "America/Denver" {
		t.Errorf("Location = %v", m.Location)
	}
	var order []string
	for _, c := range m.Columns {
		order = append(order, c.Target)
	}
	wantOrder := []string{"identifier", "title", "assignee", "team_key", "team_name", "project_id", "project_name",
		"milestone_name", "issue_type", "parent_identifier", "status_name", "resolution", "created_at", "updated_at"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Errorf("column order = %v, want YAML order %v", order, wantOrder)
	}
	if got := m.Columns[7]; !reflect.DeepEqual(got, ColumnRule{Target: "milestone_name", Headers: []string{"Fix versions"}, Pick: "first"}) {
		t.Errorf("milestone_name rule = %+v", got)
	}
	if got := m.Columns[0]; !reflect.DeepEqual(got, ColumnRule{Target: "identifier", Headers: []string{"Issue key"}}) {
		t.Errorf("identifier rule = %+v", got)
	}
	wantStatus := &StatusRule{
		Column: "Status category",
		Values: []StatusMatch{
			{"To Do", issues.StatusBacklog}, {"In Progress", issues.StatusStarted}, {"Done", issues.StatusCompleted},
		},
		Overrides:           []StatusMatch{{"won't do", issues.StatusCanceled}, {"Wont Do", issues.StatusCanceled}},
		ResolutionOverrides: []StatusMatch{{"Duplicate", issues.StatusCanceled}},
	}
	if !reflect.DeepEqual(m.Status, wantStatus) {
		t.Errorf("Status = %+v, want %+v", m.Status, wantStatus)
	}
	wantTS := []TimestampRule{
		{"started_at", []string{"Status category changed"}, []issues.Status{issues.StatusStarted}},
		{"completed_at", []string{"Resolved", "Status category changed"}, []issues.Status{issues.StatusCompleted}},
		{"canceled_at", []string{"Resolved", "Status category changed"}, []issues.Status{issues.StatusCanceled}},
	}
	if !reflect.DeepEqual(m.Timestamps, wantTS) {
		t.Errorf("Timestamps = %+v", m.Timestamps)
	}
	if !reflect.DeepEqual(m.Exclude, []ExcludeRule{{"Issue Type", []string{"Epic"}}}) {
		t.Errorf("Exclude = %+v", m.Exclude)
	}
	if !reflect.DeepEqual(m.Detect, []string{"Issue key", "Status category", "Summary"}) {
		t.Errorf("Detect = %q", m.Detect)
	}
}

func TestParse_Defaults(t *testing.T) {
	m, err := Parse([]byte("version: 1\nsource: x\ncolumns:\n  identifier: Key\n  team_key: {value: BSD}\n  title: {columns: [A, B]}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Location != time.Local || m.Status != nil || m.TimeFormats != nil {
		t.Errorf("defaults: Location=%v Status=%v TimeFormats=%v", m.Location, m.Status, m.TimeFormats)
	}
	if !reflect.DeepEqual(m.Columns[1], ColumnRule{Target: "team_key", Value: "BSD"}) {
		t.Errorf("value rule = %+v", m.Columns[1])
	}
	if !reflect.DeepEqual(m.Columns[2], ColumnRule{Target: "title", Headers: []string{"A", "B"}}) {
		t.Errorf("columns rule = %+v", m.Columns[2])
	}
}

func TestParse_TimeFormatList(t *testing.T) {
	m, err := Parse([]byte("version: 1\nsource: x\ntime_format: [\"2006-01-02 15:04\", \"02/Jan/06 3:04 PM\"]\ntimezone: UTC\ncolumns:\n  identifier: Key\n  created_at: Created\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(m.TimeFormats) != 2 || m.Location != time.UTC {
		t.Errorf("TimeFormats=%q Location=%v", m.TimeFormats, m.Location)
	}
}

// base is a valid minimal mapping the error cases below perturb.
const base = "version: 1\nsource: jira\ncolumns:\n  identifier: Issue key\n"

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name, yaml, want string
	}{
		{"yaml syntax", "version: 1\ncolumns: [unclosed\n", "mapping: "},
		{"unknown key", "version: 1\nsource: jira\ncolums:\n  identifier: Key\n", "mapping: line 3: field colums not found in type importmap.rawMapping"},
		{"version 2", "version: 2\nsource: jira\ncolumns:\n  identifier: K\n", "version: unsupported mapping version 2 (this forecast understands 1)"},
		{"version missing", "source: jira\ncolumns:\n  identifier: K\n", "version: required"},
		{"source missing", "version: 1\ncolumns:\n  identifier: K\n", `source: required (e.g. "jira")`},
		{"source invalid", "version: 1\nsource: My Jira\ncolumns:\n  identifier: K\n", `source: "My Jira" must be lowercase letters, digits and dashes`},
		{"format", base + "format: xlsx\n", `format: unsupported format "xlsx"`},
		{"unknown target", base + "  state_type: Status\n", "columns.state_type: unknown target; valid targets: identifier, title, url,"},
		{"source target", base + "  source: X\n", "columns.source: unknown target;"},
		{"identifier missing", "version: 1\nsource: jira\ncolumns:\n  title: Summary\n", "columns.identifier: required"},
		{"columns missing", "version: 1\nsource: jira\n", "columns: required"},
		{"rule shape: two", base + "  milestone_name: {column: A, value: B}\n", "columns.milestone_name: use exactly one of column, columns, value"},
		{"rule shape: none", base + "  milestone_name: {pick: first}\n", "columns.milestone_name: use exactly one of column, columns, value"},
		{"rule shape: list", base + "  title: [A, B]\n", "columns.title: expected a header name"},
		{"rule unknown key", base + "  title: {colum: A}\n", `columns.title: unknown key "colum"`},
		{"bad pick", base + "  milestone_name: {column: Fix versions, pick: second}\n", "columns.milestone_name.pick: must be first, last or join"},
		{"pick without column", base + "  title: {columns: [A], pick: first}\n", "columns.title.pick: only goes with column"},
		{"bad status value", base + "  status_name: Status\nstatus:\n  overrides:\n    won't do: cancelled\n",
			`status.overrides["won't do"]: "cancelled" is not a status; use backlog, started, completed or canceled`},
		{"duplicate normalized key", base + "status:\n  column: C\n  values:\n    Done: completed\n    done: completed\n", `status.values: "Done" and "done" are the same value`},
		{"values without column", base + "status:\n  values:\n    Done: completed\n", "status: values and column go together"},
		{"column without values", base + "status:\n  column: C\n", "status: values and column go together"},
		{"override needs status_name", base + "status:\n  column: C\n  values: {Done: completed}\n  overrides: {X: canceled}\n", "status.overrides: needs columns.status_name"},
		{"resolution override needs resolution", base + "status:\n  column: C\n  values: {Done: completed}\n  resolution_overrides: {X: canceled}\n", "status.resolution_overrides: needs columns.resolution"},
		{"status twice", base + "  status: S\nstatus:\n  column: C\n  values: {Done: completed}\n", "status: can't be used together with columns.status"},
		{"when missing", base + "time_format: \"2006-01-02\"\nstatus: {column: C, values: {Done: completed}}\ntimestamps:\n  completed_at: {columns: [R]}\n", "timestamps.completed_at.when: required"},
		{"when invalid", base + "time_format: \"2006-01-02\"\nstatus: {column: C, values: {Done: completed}}\ntimestamps:\n  completed_at: {columns: [R], when: done}\n", `timestamps.completed_at.when: "done" is not a status`},
		{"when needs status", base + "time_format: \"2006-01-02\"\ntimestamps:\n  completed_at: {columns: [R], when: completed}\n", "timestamps.completed_at.when: needs a status rule"},
		{"timestamp target", base + "time_format: \"2006-01-02\"\nstatus: {column: C, values: {Done: completed}}\ntimestamps:\n  created_at: {columns: [R], when: completed}\n", "timestamps.created_at: must be one of started_at, completed_at, canceled_at"},
		{"target in both", base + "  completed_at: Resolved\ntime_format: \"2006-01-02\"\nstatus: {column: C, values: {Done: completed}}\ntimestamps:\n  completed_at: {columns: [R], when: completed}\n", "timestamps.completed_at: also mapped under columns"},
		{"no time_format", base + "  created_at: Created\n", "time_format: required because columns.created_at is a timestamp"},
		{"no time_format, timestamps only", base + "status: {column: C, values: {Done: completed}}\ntimestamps:\n  completed_at: {columns: [R], when: completed}\n", "time_format: required because timestamps.completed_at is a timestamp"},
		{"bad layout", base + "time_format: \"Created\"\n  \n", `time_format: "Created" does not round-trip a timestamp`},
		{"layout without a year", base + "time_format: \"02/Jan 3:04 PM\"\n", `time_format: "02/Jan 3:04 PM" does not round-trip a timestamp`},
		{"bad timezone", base + "timezone: Mountain\n", `timezone: unknown time zone "Mountain"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatalf("Parse = nil error, want %q", tt.want)
			}
			if !strings.HasPrefix(err.Error(), tt.want) {
				t.Fatalf("Parse error =\n%s\nwant prefix\n%s", err, tt.want)
			}
		})
	}
}

func TestParse_CollectsEveryProblem(t *testing.T) {
	_, err := Parse([]byte("version: 3\nsource: jira\ncolumns:\n  title: Summary\n"))
	if err == nil {
		t.Fatal("want error")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "version:") || !strings.HasPrefix(lines[1], "columns.identifier:") {
		t.Fatalf("problems = %q, want version and columns.identifier", lines)
	}
}

func TestParse_UnquotedApostropheKey(t *testing.T) {
	m, err := Parse([]byte(base + "  status_name: Status\nstatus:\n  overrides:\n    won't do: canceled\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Status.Overrides[0].Raw != "won't do" {
		t.Fatalf("override key = %q", m.Status.Overrides[0].Raw)
	}
}
