package importmap

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestTemplates_List(t *testing.T) {
	if got := Templates(); !reflect.DeepEqual(got, []string{"jira-cloud-csv"}) {
		t.Fatalf("Templates() = %q", got)
	}
	if _, ok := Template("nope"); ok {
		t.Fatal(`Template("nope") found something`)
	}
}

func TestTemplates_ParseAndDescribe(t *testing.T) {
	for _, name := range Templates() {
		data, ok := Template(name)
		if !ok {
			t.Fatalf("Template(%q) missing", name)
		}
		if !strings.HasPrefix(string(data), "# name: ") || Describe(name) == "" {
			t.Errorf("%s: first line must be \"# name: <display name>\"", name)
		}
		if _, err := Parse(data); err != nil {
			t.Errorf("%s does not parse: %v", name, err)
		}
	}
	if got := Describe("jira-cloud-csv"); got != "Jira Cloud CSV export" {
		t.Errorf("Describe = %q", got)
	}
}

func TestTemplates_JiraAppliesToFixture(t *testing.T) {
	data, _ := Template("jira-cloud-csv")
	header, rows := readFixture(t)
	got, rep, err := Apply(mustParse(t, string(data)), header, rows)
	if err != nil {
		t.Fatalf("the shipped template reports problems on the fixture:\n%v", err)
	}
	if rep.Imported != 11 {
		t.Errorf("imported %d, want 11", rep.Imported)
	}

	// The UTC copy the other tests use must be this template with only the
	// timezone changed: same issues once times are compared as instants in
	// the wall clock the export wrote.
	utcGot, _, err := Apply(mustParse(t, jiraMapping), header, rows)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		a, b := got[i], utcGot[i]
		for _, f := range []string{"created_at", "started_at", "completed_at", "canceled_at", "updated_at"} {
			ta, tb := getTime(a, f), getTime(b, f)
			if ta.IsZero() != tb.IsZero() || ta.Format("2006-01-02 15:04") != tb.Format("2006-01-02 15:04") {
				t.Errorf("%s %s: template %v vs UTC copy %v", a.Identifier, f, ta, tb)
			}
			setTime(&a, f, tb)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("template and its UTC copy disagree on %s:\n%+v\n%+v", a.Identifier, a, b)
		}
	}
}

func TestSuggest(t *testing.T) {
	header, _ := readFixture(t)
	if name, ok := Suggest(header); !ok || name != "jira-cloud-csv" {
		t.Fatalf("Suggest(fixture header) = %q, %v", name, ok)
	}
	// Case, whitespace and a BOM don't matter.
	if name, ok := Suggest([]string{"\uFEFFsummary", " ISSUE KEY ", "status category"}); !ok || name != "jira-cloud-csv" {
		t.Fatalf("Suggest(normalized) = %q, %v", name, ok)
	}
	sample, err := os.ReadFile("../../testdata/sample-issues.csv")
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(string(sample), "\n")
	if name, ok := Suggest(strings.Split(first, ",")); ok {
		t.Fatalf("Suggest(sample-issues header) = %q, want no match", name)
	}
}
