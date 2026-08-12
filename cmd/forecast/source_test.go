package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/issues"
)

func TestResolveInput(t *testing.T) {
	tests := []struct {
		name      string
		input, db string
		want      string
		wantErr   bool
	}{
		{"input set", "issues.csv", "", "issues.csv", false},
		{"input wins over db", "issues.csv", "legacy.db", "issues.csv", false},
		{"db fallback", "", "legacy.db", "legacy.db", false},
		{"neither set", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			got, err := resolveInput(fs, &tt.input, &tt.db)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// withStdin points os.Stdin at a temp file holding content for the duration
// of the test, so the "-" path through loadIssues can be exercised.
func withStdin(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = orig; f.Close() })
}

// loadIssues is the single door every command's -input goes through, so
// covering "-" here covers stdin for all of them at once.
func TestLoadIssues_Stdin(t *testing.T) {
	const csv = "identifier,created_at\nENG-1,2025-01-01\nENG-2,2025-01-02\n"

	withStdin(t, csv)
	got, err := loadIssues(context.Background(), "-", "csv")
	if err != nil {
		t.Fatalf("loadIssues(-, csv): %v", err)
	}
	if len(got) != 2 || got[0].Identifier != "ENG-1" {
		t.Errorf("got %+v, want 2 issues starting with ENG-1", got)
	}

	withStdin(t, csv)
	_, err = loadIssues(context.Background(), "-", "")
	if err == nil {
		t.Fatal("expected an error when -input - is given without -stdin-format")
	}
	if !strings.Contains(err.Error(), "-stdin-format") {
		t.Errorf("error %q should name the -stdin-format flag, not an internal API", err)
	}
}

func TestDistinctTeamKeys(t *testing.T) {
	items := []issues.Issue{
		{TeamKey: "ENG"}, {TeamKey: "DATA"}, {TeamKey: "ENG"}, {TeamKey: ""},
	}
	got := distinctTeamKeys(items)
	want := []string{"DATA", "ENG"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}
