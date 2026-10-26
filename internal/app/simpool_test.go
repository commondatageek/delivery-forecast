package app

import (
	"reflect"
	"testing"

	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

func TestUnmatchedExclusionNames(t *testing.T) {
	exc, err := simulate.ParseExclusions([]byte(`{
		"global": ["2025-12-25"],
		"engineers": {
			"alice": ["2026-01-02"],
			"bob": ["2026-01-02"],
			"carol": ["2026-01-02"],
			"zed": ["2026-01-02"],
			"yan": ["2026-01-02"],
			"empty": []
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	assignees := map[string]bool{"alice": true}
	got := unmatchedExclusionNames(exc, assignees, []string{"bob", "dave"})
	want := []string{"carol", "yan", "zed"} // sorted; alice (data) and bob (slot) match; "empty" has no entries
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unmatched = %v, want %v", got, want)
	}

	if got := unmatchedExclusionNames(simulate.Exclusions{}, assignees, nil); got != nil {
		t.Errorf("no exclusions: got %v, want nil", got)
	}
}

func TestPreparePool_WarningsInOrder(t *testing.T) {
	b := constantBase()
	b.Issues = append(b.Issues, issues.Issue{ // assigned but with no usable completion record is skipped elsewhere; here: unassigned completed
		Identifier: "U-1", StateType: "completed", CompletedAt: localDay(2025, 1, 4),
	})
	b.TypicalEngineers = []string{"alice", "zed"}
	exc, err := simulate.ParseExclusions([]byte(`{"engineers": {"yan": ["2025-03-03"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	b.Exclusions = exc

	mode, err := b.mode()
	if err != nil {
		t.Fatal(err)
	}
	run, warns, err := preparePool(b, mode, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`-typical-engineers engineer "zed" not found in data`,
		`exclusions: engineer "yan" matches no assignee in the sample data and no -engineers name`,
	}
	if !reflect.DeepEqual(warns, want) {
		t.Errorf("warnings = %q, want %q", warns, want)
	}
	if run.ModeLabel != "1 equivalent engineers" || run.Pool == nil || len(run.PoolIssues) != 10 {
		t.Errorf("run = %+v", run)
	}
}

func TestPreparePool_ValidateErrorKeepsWarnings(t *testing.T) {
	b := constantBase()
	b.TypicalEngineers = []string{"zed"}
	mode, _ := b.mode()
	_, warns, err := preparePool(b, mode, false)
	if err == nil {
		t.Fatal("expected the empty-pool error")
	}
	if want := []string{`-typical-engineers engineer "zed" not found in data`}; !reflect.DeepEqual(warns, want) {
		t.Errorf("warnings = %q, want %q", warns, want)
	}
}

func TestPreparePool_WholeTeamIgnoresPerEngineerExclusions(t *testing.T) {
	b := constantBase()
	b.Engineers, b.WholeTeam = 0, true
	exc, _ := simulate.ParseExclusions([]byte(`{"engineers": {"alice": ["2025-03-03"]}}`))
	b.Exclusions = exc
	mode, _ := b.mode()
	_, warns, err := preparePool(b, mode, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"exclusions: per-engineer entries are ignored in -whole-team mode"}; !reflect.DeepEqual(warns, want) {
		t.Errorf("warnings = %q, want %q", warns, want)
	}
}
