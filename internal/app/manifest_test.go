package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

func TestNewManifest_Assembly(t *testing.T) {
	pool := simulate.NewSamplePool(map[string][]int{
		"alice": {1, 0, 2},
		"bob":   {0, 0},
	})
	testIssues := []issues.Issue{
		{Identifier: "ENG-2", Assignee: "bob", CompletedAt: day(2025, 1, 6)},
		{Identifier: "ENG-1", Assignee: "alice", CompletedAt: day(2025, 1, 5)},
	}

	exc, err := simulate.ParseExclusions([]byte(`{
		"global": [{"date": "2025-01-03", "reason": "offsite"}, "2025-03-10/2025-03-11"],
		"engineers": {"alice": ["2025-01-20", "2025-03-12"]}
	}`))
	if err != nil {
		t.Fatal(err)
	}

	generated := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
	flags := []FlagRecord{{Name: "days", Default: "30", Value: "30"}}
	m := NewManifest(ManifestInputs{
		Subcommand: "sim items",
		Run: SimRun{
			Mode:            simulate.ModeAnonymous,
			Seed:            42,
			SampleStart:     day(2025, 1, 1),
			SampleEnd:       day(2025, 2, 1),
			Pool:            pool,
			PoolIssues:      testIssues,
			Skipped:         1,
			Exclusions:      exc,
			SampleCalendar:  simulate.NewCalendar(exc, day(2025, 1, 1)),
			HorizonCalendar: simulate.NewCalendar(exc, day(2025, 3, 1)),
		},
		Engineers:      3,
		ExclusionsPath: "cal.json",
		Extra:          map[string]any{"effective_confidence_levels": []int{50, 75, 85}},
		Flags:          flags,
		Invocation:     Invocation{Subcommand: "sim items", Args: []string{"forecast"}, WorkingDir: "/w"},
		Binary:         BinaryInfo{GitSHA: "abc"},
		Input:          DataFile{Path: "/data/test.db"},
		GeneratedAt:    generated,
	})

	if m.SchemaVersion != 3 {
		t.Fatalf("SchemaVersion = %d, want 3", m.SchemaVersion)
	}
	if m.GeneratedAt != "2025-03-15T10:30:00Z" {
		t.Errorf("GeneratedAt = %q", m.GeneratedAt)
	}
	if m.Binary.GitSHA != "abc" || m.Data.Input.Path != "/data/test.db" || !reflect.DeepEqual(m.Flags, flags) || m.Invocation.WorkingDir != "/w" {
		t.Errorf("caller-supplied sections not passed through: %+v", m)
	}

	ex := m.Data.Exclusions
	if ex.Path != "cal.json" {
		t.Errorf("Exclusions.Path = %q", ex.Path)
	}
	// Sample window is [2025-01-01, 2025-02-01): only January dates count.
	if want := []string{"2025-01-03"}; !reflect.DeepEqual(ex.SampleDaysDropped.Global, want) {
		t.Errorf("SampleDaysDropped.Global = %v, want %v", ex.SampleDaysDropped.Global, want)
	}
	if want := []string{"2025-01-03", "2025-01-20"}; !reflect.DeepEqual(ex.SampleDaysDropped.Engineers["alice"], want) {
		t.Errorf("SampleDaysDropped.Engineers[alice] = %v, want %v", ex.SampleDaysDropped.Engineers["alice"], want)
	}
	// Horizon anchored 2025-03-01: only dates at/after it.
	if want := []string{"2025-03-10", "2025-03-11"}; !reflect.DeepEqual(ex.HorizonDaysOff.Global, want) {
		t.Errorf("HorizonDaysOff.Global = %v, want %v", ex.HorizonDaysOff.Global, want)
	}
	if want := []string{"2025-03-10", "2025-03-11", "2025-03-12"}; !reflect.DeepEqual(ex.HorizonDaysOff.Engineers["alice"], want) {
		t.Errorf("HorizonDaysOff.Engineers[alice] = %v, want %v", ex.HorizonDaysOff.Engineers["alice"], want)
	}
	if m.Invocation.Subcommand != "sim items" {
		t.Fatalf("Subcommand = %q, want %q", m.Invocation.Subcommand, "sim items")
	}

	if m.Resolved.Seed != 42 {
		t.Fatalf("Resolved.Seed = %d, want 42", m.Resolved.Seed)
	}
	if m.Resolved.TotalDays != 31 {
		t.Fatalf("Resolved.TotalDays = %d, want 31", m.Resolved.TotalDays)
	}
	if m.Resolved.ModeLabel != "3 equivalent engineers" || m.Resolved.Mode != "anonymous" {
		t.Errorf("Resolved mode = %q / %q", m.Resolved.Mode, m.Resolved.ModeLabel)
	}

	if m.Pool.EngineerCount != 2 {
		t.Fatalf("Pool.EngineerCount = %d, want 2", m.Pool.EngineerCount)
	}
	if got := m.Pool.PerEngineerCompletions["alice"]; got != 3 {
		t.Fatalf("PerEngineerCompletions[alice] = %d, want 3", got)
	}
	if got := m.Pool.TotalCompletions; got != 3 {
		t.Fatalf("Pool.TotalCompletions = %d, want 3", got)
	}
	if got := m.Pool.CombinedSampleCount; got != 5 {
		t.Fatalf("Pool.CombinedSampleCount = %d, want 5", got)
	}

	if m.Issues.QueriedCount != 2 || m.Issues.SkippedCount != 1 || len(m.Issues.Records) != 2 {
		t.Fatalf("Issues = %+v, want QueriedCount=2 SkippedCount=1 len(Records)=2", m.Issues)
	}
	// Records come back sorted by identifier, whatever order the run had them in.
	if m.Issues.Records[0].Identifier != "ENG-1" || m.Issues.Records[0].Assignee != "alice" {
		t.Fatalf("Records[0] = %+v, want Identifier=ENG-1 Assignee=alice", m.Issues.Records[0])
	}
}

func TestNewManifest_NoExclusionsMarshalsEmptyLists(t *testing.T) {
	m := NewManifest(ManifestInputs{
		Subcommand: "sim items",
		Run: SimRun{
			Mode:        simulate.ModeFullTeam,
			SampleStart: day(2025, 1, 1),
			SampleEnd:   day(2025, 2, 1),
			Pool:        simulate.NewSamplePool(map[string][]int{simulate.WholeTeamKey: {1}}),
		},
	})
	b, err := json.Marshal(m.Data.Exclusions)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"path":""`, `"sample_days_dropped":{"global":[]}`, `"horizon_days_off":{"global":[]}`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("exclusions JSON %s missing %s", b, want)
		}
	}
}

// TestNewManifest_HorizonFromRun goes through the real run functions rather
// than a hand-built SimRun, so it catches a result whose Run lacks the
// horizon calendar the engine used.
func TestNewManifest_HorizonFromRun(t *testing.T) {
	exc, err := simulate.ParseExclusions([]byte(`{"global": ["2025-03-05"]}`))
	if err != nil {
		t.Fatal(err)
	}
	base := constantBase()
	base.Exclusions = exc

	runs := map[string]func() (SimRun, error){
		"sim items": func() (SimRun, error) {
			res, err := SimItems(ItemsRequest{SimBase: base, TargetStart: horizonStart, Days: 10})
			return res.Run, err
		},
		"sim days": func() (SimRun, error) {
			res, err := SimDays(DaysRequest{SimBase: base, TargetStart: horizonStart, Items: []int{3}})
			return res.Run, err
		},
		"sim probability": func() (SimRun, error) {
			res, err := SimProbability(ProbabilityRequest{SimBase: base, TargetStart: horizonStart, Days: 10, Items: 5})
			return res.Run, err
		},
	}
	for name, run := range runs {
		t.Run(name, func(t *testing.T) {
			r, err := run()
			if err != nil {
				t.Fatal(err)
			}
			m := NewManifest(ManifestInputs{Subcommand: name, Run: r, Engineers: 1})
			if want := []string{"2025-03-05"}; !reflect.DeepEqual(m.Data.Exclusions.HorizonDaysOff.Global, want) {
				t.Errorf("HorizonDaysOff.Global = %v, want %v", m.Data.Exclusions.HorizonDaysOff.Global, want)
			}
		})
	}
}
