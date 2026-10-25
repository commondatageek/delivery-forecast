package app

import (
	"testing"

	"github.com/commondatageek/delivery-forecast/issues"
)

// TestBlendingTeamsWarning guards the multi-team blend warning shared by
// count/aging/cfd: it must fire only when the user gave no -teams filter AND the
// store holds more than one team, and the message must name the count and keys.
func TestBlendingTeamsWarning(t *testing.T) {
	want := "no -teams filter given; blending data across all 2 teams (DATA, ENG)"

	tests := []struct {
		name     string
		teams    []string
		allTeams []string
		want     string
	}{
		{"multi team, no filter -> warns", nil, []string{"DATA", "ENG"}, want},
		{"multi team, filter set -> silent", []string{"ENG"}, []string{"DATA", "ENG"}, ""},
		{"single team -> silent", nil, []string{"ENG"}, ""},
		{"no teams in store -> silent", nil, nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BlendingTeamsWarning(tt.teams, tt.allTeams); got != tt.want {
				t.Errorf("BlendingTeamsWarning(%v, %v) = %q, want %q", tt.teams, tt.allTeams, got, tt.want)
			}
		})
	}
}

func TestDistinctTeamKeys(t *testing.T) {
	items := []issues.Issue{
		{TeamKey: "ENG"}, {TeamKey: "DATA"}, {TeamKey: "ENG"}, {TeamKey: ""},
	}
	got := DistinctTeamKeys(items)
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
