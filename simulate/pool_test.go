package simulate

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// day builds a local-midnight calendar date. Fixtures use time.Local (not UTC)
// so they share the same zone as the day-bucketing under test, keeping the
// expected-value assertions correct regardless of the machine's timezone.
func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// at returns a Completion for engineer eng on the given calendar day, with a
// deliberate mid-day time to confirm time-of-day is truncated away.
func at(eng string, y int, m time.Month, d int) Completion {
	return Completion{Engineer: eng, CompletedAt: time.Date(y, m, d, 14, 30, 0, 0, time.Local)}
}

func TestFilterInvalid_SkipsMalformed(t *testing.T) {
	completedAt := day(2025, 1, 5)
	records := []Completion{
		{Engineer: "alice", CompletedAt: completedAt},
		{Engineer: "", CompletedAt: completedAt},    // no assignee
		{Engineer: "bob", CompletedAt: time.Time{}}, // no completion instant
		{Engineer: "", CompletedAt: time.Time{}},    // both missing
		{Engineer: "carol", CompletedAt: completedAt},
	}

	got, skipped := FilterInvalid(records)

	if skipped != 3 {
		t.Fatalf("skipped = %d, want 3", skipped)
	}
	want := []Completion{
		{Engineer: "alice", CompletedAt: completedAt},
		{Engineer: "carol", CompletedAt: completedAt},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %+v, want %+v", got, want)
	}
}

func TestFilterInvalid_AllValid(t *testing.T) {
	records := []Completion{
		{Engineer: "alice", CompletedAt: day(2025, 1, 5)},
		{Engineer: "bob", CompletedAt: day(2025, 1, 6)},
	}
	got, skipped := FilterInvalid(records)
	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0", skipped)
	}
	if len(got) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(got))
	}
}

func TestBuildPool_PreservesZeroDays(t *testing.T) {
	start, end := day(2025, 1, 1), day(2025, 1, 11) // 10-day window
	records := []Completion{
		at("alice", 2025, 1, 1), // idx 0
		at("alice", 2025, 1, 1), // idx 0
		at("alice", 2025, 1, 6), // idx 5
	}
	pool := BuildPool(records, nil, start, end, false)

	got := pool.PerEngineer["alice"]
	want := []int{2, 0, 0, 0, 0, 1, 0, 0, 0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("alice samples = %v, want %v (length must be 10, zeros preserved)", got, want)
	}
}

func TestBuildPool_PartialNowEndDayCountsToday(t *testing.T) {
	// When -sample-end is omitted, resolveEndDate returns a partial "now"
	// (mid-afternoon on 2025-01-05), so DaysBetween grants today an inclusive
	// slot at idx == totalDays-1. A completion landing on that final partial day
	// must be counted, not silently dropped as out-of-range.
	start := day(2025, 1, 1)
	now := time.Date(2025, 1, 5, 14, 30, 0, 0, time.Local) // partial last day
	records := []Completion{
		at("alice", 2025, 1, 1), // idx 0
		at("alice", 2025, 1, 5), // idx 4 == totalDays-1, the partial "today"
		at("alice", 2025, 1, 6), // idx 5, past the window -> dropped
	}
	pool := BuildPool(records, nil, start, now, false)

	got := pool.PerEngineer["alice"]
	want := []int{1, 0, 0, 0, 1} // 5 slots; today (idx 4) counted, 1/6 dropped
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("alice samples = %v, want %v (today's work must land in the final partial slot)", got, want)
	}
}

func TestBuildPool_DropsOutOfRangeCompletions(t *testing.T) {
	start, end := day(2025, 1, 1), day(2025, 1, 11)
	records := []Completion{
		at("alice", 2024, 12, 25), // before window -> dropped
		at("alice", 2025, 1, 1),   // idx 0
		at("alice", 2025, 1, 15),  // after window -> dropped
	}
	pool := BuildPool(records, nil, start, end, false)
	got := pool.PerEngineer["alice"]
	if len(got) != 10 {
		t.Fatalf("len = %d, want 10", len(got))
	}
	s := 0
	for _, v := range got {
		s += v
	}
	if s != 1 {
		t.Fatalf("sum = %d, want 1 (out-of-range completions must be dropped, not counted)", s)
	}
}

func TestBuildPool_GlobalExclusionRemovesSlot(t *testing.T) {
	start, end := day(2025, 1, 1), day(2025, 1, 11)
	records := []Completion{
		at("alice", 2025, 1, 1), // idx 0
		at("alice", 2025, 1, 1), // idx 0
		at("alice", 2025, 1, 6), // idx 5
	}
	exc := mustParseExclusions(t, `{"global": ["2025-01-02"]}`) // removes idx 1
	pool := BuildPool(records, NewCalendar(exc, start), start, end, false)
	got := pool.PerEngineer["alice"]
	want := []int{2, 0, 0, 0, 1, 0, 0, 0, 0} // length 9, idx1 dropped, the 1 shifts left
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildPool_PerEngineerExclusion(t *testing.T) {
	start, end := day(2025, 1, 1), day(2025, 1, 11)
	records := []Completion{
		at("alice", 2025, 1, 1), // idx 0
		at("alice", 2025, 1, 1), // idx 0
		at("alice", 2025, 1, 6), // idx 5
	}
	exc := mustParseExclusions(t, `{"engineers": {"alice": ["2025-01-06"]}}`) // removes idx 5
	pool := BuildPool(records, NewCalendar(exc, start), start, end, false)
	got := pool.PerEngineer["alice"]
	want := []int{2, 0, 0, 0, 0, 0, 0, 0, 0} // length 9, the lone "1" removed
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildPool_EngineerSetFromInWindowCompletions(t *testing.T) {
	start, end := day(2025, 1, 1), day(2025, 1, 11)
	records := []Completion{
		at("alice", 2025, 1, 2), // in window
		at("bob", 2024, 12, 20), // bob's only completion is BEFORE the window
	}
	pool := BuildPool(records, nil, start, end, false)

	if _, ok := pool.PerEngineer["alice"]; !ok {
		t.Fatal("alice should be in the pool (has an in-window completion)")
	}
	if _, ok := pool.PerEngineer["bob"]; ok {
		t.Fatal("bob must NOT be in the pool: an engineer with no in-window " +
			"completion should not appear")
	}
	if len(pool.PerEngineer) != 1 {
		t.Fatalf("pool should contain exactly 1 engineer, got %d", len(pool.PerEngineer))
	}
}

func TestBuildPool_WholeTeamSumsAndIgnoresPerEngineerExclusions(t *testing.T) {
	start, end := day(2025, 1, 1), day(2025, 1, 11)
	records := []Completion{
		at("alice", 2025, 1, 1), // idx 0
		at("bob", 2025, 1, 1),   // idx 0
		at("bob", 2025, 1, 6),   // idx 5
	}
	// Per-engineer exclusions must be ignored in whole-team mode.
	exc := mustParseExclusions(t, `{"engineers": {"bob": ["2025-01-06"]}}`)
	pool := BuildPool(records, NewCalendar(exc, start), start, end, true)

	if len(pool.PerEngineer) != 1 {
		t.Fatalf("whole-team pool should have exactly one series, got %d", len(pool.PerEngineer))
	}
	got := pool.PerEngineer[WholeTeamKey]
	want := []int{2, 0, 0, 0, 0, 1, 0, 0, 0, 0} // idx0: 1+1, idx5: 1
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNewSamplePool_CombinedDeterministicOrder(t *testing.T) {
	perEngineer := map[string][]int{
		"carol": {5, 6},
		"alice": {1, 2},
		"bob":   {3, 4},
	}
	want := []int{1, 2, 3, 4, 5, 6} // alice, bob, carol: sorted by engineer name
	for i := range 10 {
		if got := NewSamplePool(perEngineer).Combined; !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: Combined = %v, want %v", i, got, want)
		}
	}
}

func TestDaysBetween(t *testing.T) {
	cases := []struct {
		name       string
		start, end time.Time
		want       int
	}{
		{"whole days, midnight end excludes that day", day(2025, 1, 1), day(2025, 1, 11), 10},
		{"partial end day gets one inclusive slot",
			day(2025, 1, 1), time.Date(2025, 1, 5, 12, 0, 0, 0, time.Local), 5},
		{"midnight end day excluded", day(2025, 1, 1), day(2025, 1, 5), 4},
	}
	for _, c := range cases {
		if got := DaysBetween(c.start, c.end); got != c.want {
			t.Errorf("%s: DaysBetween = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestBuildPool_PanicsOnMismatchedAnchor(t *testing.T) {
	start, end := day(2025, 1, 1), day(2025, 1, 11)
	cal := NewCalendar(Exclusions{}, day(2025, 1, 2)) // off by one day

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("BuildPool did not panic on a calendar anchored at a different date")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "anchored at 2025-01-02") {
			t.Errorf("panic = %v, want it to name the calendar's anchor", r)
		}
	}()
	BuildPool(nil, cal, start, end, false)
}

func TestSlots_Anonymous(t *testing.T) {
	pool := NewSamplePool(map[string][]int{"alice": {1, 2}, "bob": {3}})
	got := pool.Slots(Params{Mode: ModeAnonymous, Engineers: 3})
	if len(got) != 3 {
		t.Fatalf("len(slots) = %d, want 3", len(got))
	}
	for i, s := range got {
		if s.Name != "" || !reflect.DeepEqual(s.Samples, []int{1, 2, 3}) {
			t.Errorf("slot %d = %+v, want anonymous over Combined {1,2,3}", i, s)
		}
	}
}

func TestSlots_Named(t *testing.T) {
	pool := NewSamplePool(map[string][]int{"alice": {1, 2}, "bob": {3}})
	got := pool.Slots(Params{Mode: ModeAnonymous, Engineers: 2, EngineerNames: []string{"x", "y"}})
	if len(got) != 2 || got[0].Name != "x" || got[1].Name != "y" {
		t.Fatalf("slots = %+v, want named x, y", got)
	}
	for _, s := range got {
		if !reflect.DeepEqual(s.Samples, []int{1, 2, 3}) {
			t.Errorf("slot %q draws %v, want pooled Combined {1,2,3} (not the named engineer's own history)", s.Name, s.Samples)
		}
	}
}

func TestSlots_WholeTeam(t *testing.T) {
	pool := NewSamplePool(map[string][]int{WholeTeamKey: {4, 0, 5}})
	got := pool.Slots(Params{Mode: ModeFullTeam, Engineers: 7})
	if len(got) != 1 || got[0].Name != "" || !reflect.DeepEqual(got[0].Samples, []int{4, 0, 5}) {
		t.Fatalf("slots = %+v, want one anonymous slot over the whole-team series", got)
	}
}
