package simulate

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveMode(t *testing.T) {
	cases := []struct {
		name         string
		engineersSet bool
		wholeTeam    bool
		want         Mode
		wantErr      bool
	}{
		{"nothing set is an error", false, false, 0, true},
		{"engineers set is anonymous", true, false, ModeAnonymous, false},
		{"whole-team", false, true, ModeFullTeam, false},
		{"engineers + whole-team conflict", true, true, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveMode(c.engineersSet, c.wholeTeam)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ResolveMode(%v, %v) = %v, want error", c.engineersSet, c.wholeTeam, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveMode error: %v", err)
			}
			if got != c.want {
				t.Errorf("ResolveMode = %v, want %v", got, c.want)
			}
		})
	}
}

func TestModeLabel(t *testing.T) {
	cases := []struct {
		mode      Mode
		engineers int
		names     []string
		want      string
	}{
		{ModeFullTeam, 3, nil, "whole-team throughput"},
		{ModeAnonymous, 3, nil, "3 equivalent engineers"},
		{ModeAnonymous, 3, []string{"alice", "bob", "carol"}, "3 equivalent engineers [alice, bob, carol]"},
	}
	for _, c := range cases {
		if got := ModeLabel(c.mode, c.engineers, c.names); got != c.want {
			t.Errorf("ModeLabel(%v) = %q, want %q", c.mode, got, c.want)
		}
	}
}

func TestValidatePool(t *testing.T) {
	named := NewSamplePool(map[string][]int{
		"alice": {1, 0, 2},
		"bob":   {3},
		"empty": {},        // present but every day excluded -> no samples
		"zero":  {0, 0, 0}, // present, has slots, but never completed anything
	})
	full := NewSamplePool(map[string][]int{WholeTeamKey: {0, 1, 0}})
	zeroFull := NewSamplePool(map[string][]int{WholeTeamKey: {0, 0, 0}})
	emptyFull := NewSamplePool(map[string][]int{WholeTeamKey: {}})
	emptyAnon := NewSamplePool(map[string][]int{})
	zeroAnon := NewSamplePool(map[string][]int{"alice": {0, 0}, "bob": {0}})

	cases := []struct {
		name            string
		pool            *SamplePool
		mode            Mode
		requireProgress bool
		wantErr         bool
	}{
		{"full team ok", full, ModeFullTeam, false, false},
		{"full team empty series", emptyFull, ModeFullTeam, false, true},
		{"anonymous ok", named, ModeAnonymous, false, false},
		{"anonymous empty pool", emptyAnon, ModeAnonymous, false, true},

		// requireProgress=false (cmdItems/cmdProbability): all-zero is a fine,
		// legitimate "0 items" answer, not an error.
		{"full team all-zero, fixed days ok", zeroFull, ModeFullTeam, false, false},
		{"anonymous all-zero, fixed days ok", zeroAnon, ModeAnonymous, false, false},

		// requireProgress=true (cmdDays): all-zero means SimulateDaysToComplete*
		// would loop forever (completed never advances), so it must error.
		{"full team all-zero, requireProgress errors", zeroFull, ModeFullTeam, true, true},
		{"full team nonzero, requireProgress ok", full, ModeFullTeam, true, false},
		{"anonymous all-zero, requireProgress errors", zeroAnon, ModeAnonymous, true, true},
		{"anonymous nonzero, requireProgress ok", named, ModeAnonymous, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePool(c.pool, c.mode, c.requireProgress)
			if c.wantErr && err == nil {
				t.Error("ValidatePool = nil, want error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("ValidatePool = %v, want nil", err)
			}
		})
	}
}

func TestParseEngineers(t *testing.T) {
	cases := []struct {
		in        string
		wantErr   string // "" = accepted
		wantCount int
		wantNames []string
	}{
		{" 3 ", "", 3, nil},
		{"alice, bob ,carol", "", 3, []string{"alice", "bob", "carol"}},
		{"alice,,", "", 1, []string{"alice"}},
		{"", "empty value", 0, nil},
		{"   ", "empty value", 0, nil},
		{"0", "must be positive", 0, nil},
		{"-2", "must be positive", 0, nil},
		{",,", "no names", 0, nil},
		{"alice,3", "looks like a count", 0, nil},
		{"alice,alice", "duplicate name", 0, nil},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			count, names, err := ParseEngineers(c.in)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("ParseEngineers(%q) error = %v, want one containing %q", c.in, err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEngineers(%q): %v", c.in, err)
			}
			if count != c.wantCount || !reflect.DeepEqual(names, c.wantNames) {
				t.Errorf("got (%d, %v), want (%d, %v)", count, names, c.wantCount, c.wantNames)
			}
		})
	}
}
