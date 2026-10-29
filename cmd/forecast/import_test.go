package main

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/importmap"
	"github.com/commondatageek/delivery-forecast/internal/source"
	"github.com/commondatageek/delivery-forecast/issues"
)

const (
	jiraFixture = "../../testdata/jira-export-sample.csv"
	jiraMapping = "testdata/golden/jira-mapping.yaml"
)

// comparable returns items sorted by identifier with timestamps in UTC, so
// sets read back from different stores compare by value.
func comparable(items []issues.Issue) []issues.Issue {
	out := slices.Clone(items)
	slices.SortFunc(out, func(a, b issues.Issue) int { return strings.Compare(a.Identifier, b.Identifier) })
	for i := range out {
		for _, t := range []*time.Time{&out[i].CreatedAt, &out[i].StartedAt, &out[i].CompletedAt, &out[i].CanceledAt, &out[i].UpdatedAt} {
			if !t.IsZero() {
				*t = t.UTC()
			}
		}
	}
	return out
}

func dryRunIssues(t *testing.T) []issues.Issue {
	t.Helper()
	res, err := source.Import(context.Background(), jiraFixture, jiraMapping, "", true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if res.Wrote {
		t.Fatal("dry run reports Wrote")
	}
	return res.Issues
}

func TestCmdImport_WritesEachFormat(t *testing.T) {
	want := comparable(dryRunIssues(t))
	for _, name := range []string{"x.db", "x.csv", "x.jsonl", "x.json"} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), name)
			stdout, _, err := runCaptured(t, func() error {
				return cmdImport([]string{"-from", jiraFixture, "-map", jiraMapping, "-out", out})
			})
			if err != nil {
				t.Fatalf("cmdImport: %v", err)
			}
			if !strings.Contains(stdout, "wrote 11 issues to "+out) {
				t.Errorf("stdout lacks the wrote line:\n%s", stdout)
			}
			got, err := source.Load(context.Background(), out, "", nil)
			if err != nil {
				t.Fatalf("load %s: %v", name, err)
			}
			if !reflect.DeepEqual(comparable(got), want) {
				t.Errorf("%s read back differs from the dry run:\ngot  %+v\nwant %+v", name, comparable(got)[0], want[0])
			}
		})
	}
}

func TestCmdImport_TwiceIntoOneDB(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bsd.db")
	args := []string{"-from", jiraFixture, "-map", jiraMapping, "-out", out}
	first, _, err := runCaptured(t, func() error { return cmdImport(args) })
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if !strings.Contains(first, `(replaced 0 issues from source "jira")`) {
		t.Errorf("first import:\n%s", first)
	}
	second, _, err := runCaptured(t, func() error { return cmdImport(args) })
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if !strings.Contains(second, `wrote 11 issues to `+out+` (replaced 11 issues from source "jira")`) {
		t.Errorf("second import:\n%s", second)
	}
	got, err := source.Load(context.Background(), out, "", nil)
	if err != nil || len(got) != 11 {
		t.Fatalf("db holds %d issues (%v), want 11", len(got), err)
	}
}

func TestCmdImport_Example(t *testing.T) {
	stdout, _, err := runCaptured(t, func() error { return cmdImport([]string{"-example", "list"}) })
	if err != nil || stdout != strings.Join(importmap.Templates(), "\n")+"\n" {
		t.Fatalf("-example list = %q, %v", stdout, err)
	}
	want, _ := importmap.Template("jira-cloud-csv")
	stdout, _, err = runCaptured(t, func() error { return cmdImport([]string{"-example", "jira-cloud-csv"}) })
	if err != nil || stdout != string(want) {
		t.Fatalf("-example jira-cloud-csv differs from the embedded template (err %v)", err)
	}
	_, _, err = runCaptured(t, func() error { return cmdImport([]string{"-example", "jira"}) })
	if err == nil || err.Error() != `-example: no template "jira"; available: jira-cloud-csv` {
		t.Fatalf("unknown template error = %v", err)
	}
}

func TestCmdImport_ExampleExcludesOtherFlags(t *testing.T) {
	_, _, err := runCaptured(t, func() error {
		return cmdImport([]string{"-example", "list", "-from", jiraFixture})
	})
	if err == nil || !strings.Contains(err.Error(), "-example prints a template and exits; it can't be combined with -from") {
		t.Fatalf("err = %v", err)
	}
}

func TestCmdImport_FlagErrors(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"-map", jiraMapping, "-dry-run"}, "-from is required"},
		{[]string{"-from", "-", "-map", jiraMapping, "-dry-run"}, "-from: stdin is not supported; pass the export file"},
		{[]string{"-from", jiraFixture, "-dry-run"}, "-map is required"},
		{[]string{"-from", jiraFixture, "-map", jiraMapping, "-out", "x.txt"}, `-out: unrecognized extension ".txt" (use .db, .csv, .json or .jsonl)`},
	} {
		_, _, err := runCaptured(t, func() error { return cmdImport(tt.args) })
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("%v: err = %v, want %q", tt.args, err, tt.want)
		}
	}
}

func TestCmdImport_FailureWritesNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "x.csv")
	_, _, err := runCaptured(t, func() error {
		return cmdImport([]string{"-from", jiraFixture, "-map", "testdata/golden/partial-jira-mapping.yaml", "-out", out})
	})
	if err == nil {
		t.Fatal("want the unmatched-status error")
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(out), "*")); len(matches) != 0 {
		t.Fatalf("a failed import left files behind: %v", matches)
	}
}
