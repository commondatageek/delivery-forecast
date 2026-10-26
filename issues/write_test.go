package issues

import (
	"bytes"
	"encoding/csv"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func readSample(t *testing.T) []Issue {
	t.Helper()
	items, err := ReadFile("../testdata/sample-issues.csv")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("sample has no issues")
	}
	return items
}

// inUTC returns a copy of items with every timestamp moved to UTC, so
// DeepEqual compares instants rather than *time.Location pointers (a parsed
// "Z" is time.UTC even when time.Local is also UTC).
func inUTC(items []Issue) []Issue {
	out := slices.Clone(items)
	for i := range out {
		for _, t := range []*time.Time{&out[i].CreatedAt, &out[i].StartedAt, &out[i].CompletedAt, &out[i].CanceledAt, &out[i].UpdatedAt} {
			if !t.IsZero() {
				*t = t.UTC()
			}
		}
	}
	return out
}

func TestWriteCSV_RoundTrip(t *testing.T) {
	want := readSample(t)
	var buf bytes.Buffer
	if err := WriteCSV(&buf, want); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	got, err := ReadCSV(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	if !reflect.DeepEqual(inUTC(got), inUTC(want)) {
		t.Fatalf("CSV round trip differs:\ngot  %+v\nwant %+v", got[0], want[0])
	}
}

func TestWriteJSON_RoundTrip(t *testing.T) {
	want := readSample(t)
	var buf bytes.Buffer
	if err := WriteJSON(&buf, want); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if n := strings.Count(buf.String(), "\n"); n != len(want) {
		t.Fatalf("WriteJSON wrote %d lines, want one per issue (%d)", n, len(want))
	}
	got, err := ReadJSON(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if !reflect.DeepEqual(inUTC(got), inUTC(want)) {
		t.Fatalf("JSON round trip differs:\ngot  %+v\nwant %+v", got[0], want[0])
	}
}

func TestWriters_EveryField(t *testing.T) {
	want := []Issue{{
		Source: "jira", Identifier: "DEMO-1", Title: `Fix "Save", again`, URL: "https://x.test/DEMO-1",
		Assignee: "Alice <a@x.test>", TeamKey: "DEMO", TeamName: "Demo", ProjectID: "DEMO-0", ProjectName: "Epic",
		MilestoneID: "v1", MilestoneName: "Release 1", IssueType: "Bug", ParentIdentifier: "DEMO-0",
		Status: StatusCanceled, StatusName: "Won't Do", Resolution: "Duplicate",
		CreatedAt:  time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
		CanceledAt: time.Date(2026, 10, 1, 17, 30, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC),
	}}
	for name, rw := range map[string]struct {
		write func(*bytes.Buffer, []Issue) error
		read  func(*bytes.Buffer) ([]Issue, error)
	}{
		"csv":  {func(b *bytes.Buffer, x []Issue) error { return WriteCSV(b, x) }, func(b *bytes.Buffer) ([]Issue, error) { return ReadCSV(b) }},
		"json": {func(b *bytes.Buffer, x []Issue) error { return WriteJSON(b, x) }, func(b *bytes.Buffer) ([]Issue, error) { return ReadJSON(b) }},
	} {
		var buf bytes.Buffer
		if err := rw.write(&buf, want); err != nil {
			t.Fatalf("%s write: %v", name, err)
		}
		got, err := rw.read(&buf)
		if err != nil {
			t.Fatalf("%s read: %v", name, err)
		}
		if !reflect.DeepEqual(inUTC(got), inUTC(want)) {
			t.Errorf("%s round trip:\ngot  %+v\nwant %+v", name, got[0], want[0])
		}
	}
}

func TestWriteCSV_HeaderAndZeroTimes(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCSV(&buf, []Issue{{Identifier: "A-1"}}); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if !slices.Equal(rows[0], Columns()) {
		t.Fatalf("header = %v, want Columns() = %v", rows[0], Columns())
	}
	for i, name := range rows[0] {
		if isTimestampColumn(name) && rows[1][i] != "" {
			t.Errorf("%s = %q, want empty for the zero time", name, rows[1][i])
		}
	}
}

func TestWriteJSON_ZeroTimesAreNull(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, []Issue{{Identifier: "A-1", CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	line := buf.String()
	if !strings.HasPrefix(line, `{"source":"","identifier":"A-1",`) {
		t.Errorf("keys not in Columns() order: %s", line)
	}
	for _, want := range []string{`"created_at":"2026-01-02T03:04:05Z"`, `"started_at":null`, `"completed_at":null`, `"canceled_at":null`, `"updated_at":null`} {
		if !strings.Contains(line, want) {
			t.Errorf("output lacks %s: %s", want, line)
		}
	}
}

func TestColumns_ReturnsCopy(t *testing.T) {
	c := Columns()
	c[0] = "mutated"
	if Columns()[0] == "mutated" {
		t.Fatal("Columns() exposes the package's slice")
	}
}
