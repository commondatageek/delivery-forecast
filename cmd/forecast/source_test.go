package main

import (
	"flag"
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
