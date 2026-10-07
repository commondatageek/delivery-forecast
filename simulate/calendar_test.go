package simulate

import (
	"reflect"
	"testing"
	"time"
)

func TestCalendar_Working_GlobalAndPerName(t *testing.T) {
	anchor := day(2025, 3, 1)
	cal := NewCalendar(mustParseExclusions(t, `{
		"global": ["2025-03-03"],
		"engineers": {"alice": ["2025-03-05"]}
	}`), anchor)

	cases := []struct {
		name string
		d    int
		want bool
	}{
		{"", 2, false},      // global off for everyone
		{"alice", 2, false}, // ...including named engineers (inherits global)
		{"bob", 2, false},
		{"alice", 4, false}, // alice's own day off
		{"bob", 4, true},    // not bob's
		{"", 4, true},       // ...nor the anonymous scope's
		{"alice", 3, true},  // an ordinary day
		{"", 0, true},
	}
	for _, c := range cases {
		if got := cal.Working(c.name, c.d); got != c.want {
			t.Errorf("Working(%q, %d) = %v, want %v", c.name, c.d, got, c.want)
		}
	}
}

func TestCalendar_NilIsAlwaysWorking(t *testing.T) {
	var cal *Calendar
	for _, d := range []int{-5, 0, 3, 400} {
		if !cal.Working("alice", d) || !cal.Working("", d) {
			t.Errorf("nil Calendar not working on day %d", d)
		}
	}
	if cal.Rebase(day(2025, 1, 1)) != nil {
		t.Error("nil.Rebase should be nil")
	}
	if cal.OffDays("", 0, 10) != nil || cal.OffDays("", 0, -1) != nil {
		t.Error("nil.OffDays should be nil")
	}
}

func TestCalendar_BeforeAnchor(t *testing.T) {
	cal := NewCalendar(mustParseExclusions(t, `{"global": ["2025-02-26"]}`), day(2025, 3, 1))
	// 2025-02-26 is 3 days before the anchor.
	if cal.Working("", -3) {
		t.Error("Working(-3) = true, want false for an excluded date before the anchor")
	}
	if !cal.Working("", -2) {
		t.Error("Working(-2) = false, want true for an unexcluded date before the anchor")
	}
}

func TestCalendar_Rebase(t *testing.T) {
	anchor := day(2025, 3, 1)
	cal := NewCalendar(mustParseExclusions(t, `{
		"global": ["2025-03-03", "2025-03-10/2025-03-12"],
		"engineers": {"alice": ["2025-03-06"]}
	}`), anchor)
	reb := cal.Rebase(anchor.AddDate(0, 0, 3))

	if !reb.Anchor().Equal(day(2025, 3, 4)) {
		t.Fatalf("Rebase anchor = %v", reb.Anchor())
	}
	for _, name := range []string{"", "alice", "bob"} {
		for d := -10; d <= 20; d++ {
			if got, want := reb.Working(name, d), cal.Working(name, d+3); got != want {
				t.Errorf("Rebase.Working(%q, %d) = %v, want %v", name, d, got, want)
			}
		}
	}
	// The original must be untouched.
	if cal.Working("", 2) {
		t.Error("Rebase mutated the receiver")
	}
}

func TestCalendar_OffDays_BoundedAndUnbounded(t *testing.T) {
	cal := NewCalendar(mustParseExclusions(t, `{
		"global": ["2025-03-03", "2025-03-20"],
		"engineers": {"alice": ["2025-03-05", "2025-03-03"]}
	}`), day(2025, 3, 1))

	// Bounded: [0, 10) sees 3/3 (global) and 3/5 (alice) but not 3/20.
	got := cal.OffDays("alice", 0, 10)
	want := []time.Time{day(2025, 3, 3), day(2025, 3, 5)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OffDays(alice, 0, 10) = %v, want %v", got, want)
	}
	if got := cal.OffDays("", 0, 10); !reflect.DeepEqual(got, []time.Time{day(2025, 3, 3)}) {
		t.Errorf("OffDays(\"\", 0, 10) = %v", got)
	}
	// Half-open: to is exclusive.
	if got := cal.OffDays("", 0, 2); got != nil {
		t.Errorf("OffDays(\"\", 0, 2) = %v, want nil (3/3 is index 2, excluded)", got)
	}
	if got := cal.OffDays("", 0, 3); len(got) != 1 {
		t.Errorf("OffDays(\"\", 0, 3) = %v, want just 3/3", got)
	}

	// Unbounded: every explicit date at or after from, de-duplicated and sorted.
	got = cal.OffDays("alice", 0, -1)
	want = []time.Time{day(2025, 3, 3), day(2025, 3, 5), day(2025, 3, 20)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OffDays(alice, 0, -1) = %v, want %v", got, want)
	}
	if got := cal.OffDays("alice", 3, -1); !reflect.DeepEqual(got, want[1:]) {
		t.Errorf("OffDays(alice, 3, -1) = %v, want %v", got, want[1:])
	}
}

func TestCalendar_Anchor_NormalizesToLocalMidnight(t *testing.T) {
	cal := NewCalendar(Exclusions{}, time.Date(2025, 3, 1, 14, 30, 0, 0, time.Local))
	if got := cal.Anchor(); !got.Equal(day(2025, 3, 1)) {
		t.Errorf("Anchor = %v, want %v", got, day(2025, 3, 1))
	}
}

func TestCalendar_WeekdayMaskReservedButInert(t *testing.T) {
	// 2025-03-01 is a Saturday. Nothing sets the mask yet; this pins the hook
	// the future weekends flag will use, including for negative indices.
	cal := NewCalendar(Exclusions{}, day(2025, 3, 1))
	if cal.Working("", 0) != true {
		t.Fatal("mask must default to all-working")
	}
	cal.weekdaysOff[time.Saturday] = true
	cal.weekdaysOff[time.Sunday] = true

	cases := []struct {
		d    int
		want bool
	}{
		{0, false},   // Sat
		{1, false},   // Sun
		{2, true},    // Mon
		{6, true},    // Fri
		{7, false},   // Sat
		{-1, true},   // Fri
		{-2, true},   // Thu
		{-6, false},  // Sun
		{-7, false},  // Sat
		{-14, false}, // Sat
		{-13, false}, // Sun
		{-12, true},  // Mon
	}
	for _, c := range cases {
		if got := cal.Working("", c.d); got != c.want {
			t.Errorf("Working(%d) = %v, want %v", c.d, got, c.want)
		}
	}
	// Rebasing keeps the mask aligned with real weekdays.
	reb := cal.Rebase(day(2025, 3, 3)) // a Monday
	if !reb.Working("", 0) || reb.Working("", 5) {
		t.Error("Rebase should keep weekday alignment (Mon works, Sat doesn't)")
	}
}
