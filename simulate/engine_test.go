package simulate

import (
	"math/rand"
	"testing"
)

// assertAll fails unless every element of got equals want. Used with constant
// sample pools, where each simulation trial is fully determined and must be identical.
func assertAll(t *testing.T, got []int, want int) {
	t.Helper()
	if len(got) == 0 {
		t.Fatalf("got empty result slice, want %d non-empty", want)
	}
	for i, v := range got {
		if v != want {
			t.Fatalf("result[%d] = %d, want every element == %d", i, v, want)
		}
	}
}

func TestSimulateItemsInDays_ConstantPool(t *testing.T) {
	got := SimulateItemsInDays([]int{2}, 3, 10, 1000, 4, 42, nil)
	assertAll(t, got, 60) // 3 engineers * 10 days * 2 per draw
}

func TestSimulateDaysToComplete_ConstantPool(t *testing.T) {
	got := SimulateDaysToComplete([]int{2}, 1, 10, 1000, 4, 42, nil)
	assertAll(t, got, 5) // 2 items/day, need 10 -> 5 days

	// Inexact case guards the termination off-by-one: ceil(11/2) = 6.
	got = SimulateDaysToComplete([]int{2}, 1, 11, 1000, 4, 42, nil)
	assertAll(t, got, 6)
}

// calFromJSON builds a Calendar anchored at day 0 = 2025-03-01 from an
// exclusions JSON literal.
func calFromJSON(t *testing.T, s string) *Calendar {
	t.Helper()
	return NewCalendar(mustParseExclusions(t, s), day(2025, 3, 1))
}

func anon(samples []int, n int) []Slot { return anonymousSlots(samples, n) }

func TestSimulateItems_CalendarZeroesGlobalDays(t *testing.T) {
	cal := calFromJSON(t, `{"global": ["2025-03-02/2025-03-04"]}`) // days 1..3
	got := simulateItems(anon([]int{2}, 3), cal, 10, 500, 4, 42, nil)
	assertAll(t, got, 42) // (10-3) days * 3 slots * 2
}

func TestSimulateItems_CalendarZeroesOnlyNamedSlot(t *testing.T) {
	cal := calFromJSON(t, `{"engineers": {"alice": ["2025-03-01/2025-03-04"]}}`) // days 0..3
	slots := []Slot{{Name: "alice", Samples: []int{2}}, {Name: "bob", Samples: []int{2}}}
	got := simulateItems(slots, cal, 10, 500, 4, 42, nil)
	assertAll(t, got, 10*2+6*2) // bob works all 10 days, alice 6
}

func TestSimulateItems_AnonymousSlotIgnoresPerNameRules(t *testing.T) {
	cal := calFromJSON(t, `{"engineers": {"alice": ["2025-03-01/2025-03-10"]}}`)
	got := simulateItems(anon([]int{2}, 3), cal, 10, 500, 4, 42, nil)
	assertAll(t, got, 60)
}

func TestSimulateItems_OffDaysBeyondHorizonAreIrrelevant(t *testing.T) {
	cal := calFromJSON(t, `{"global": ["2025-04-01/2025-04-30"]}`) // days 31..60
	got := simulateItems(anon([]int{2}, 3), cal, 10, 500, 4, 42, nil)
	assertAll(t, got, 60)
}

func TestSimulateDays_LeadingOffDaysAddCalendarDays(t *testing.T) {
	cal := calFromJSON(t, `{"global": ["2025-03-01/2025-03-03"]}`) // days 0..2
	got := simulateDays(anon([]int{2}, 2), cal, 20, 500, 4, 42, nil)
	assertAll(t, got, 3+5) // 3 idle days, then 4/day for 5 days
}

func TestSimulateDays_OffDayDoesNotConsumeRNG(t *testing.T) {
	const k = 3
	samples := []int{0, 1, 2, 5, 0, 3}
	cal := calFromJSON(t, `{"global": ["2025-03-01/2025-03-03"]}`) // first k days
	slots := anon(samples, 2)

	// Same seed and worker count, so trial i sees the same RNG stream in both
	// runs; the skipped days must not shift it. Compare unsorted per-trial
	// results by driving the trial function directly.
	run := func(c *Calendar) []int {
		out := make([]int, 200)
		rng := rand.New(rand.NewSource(7))
		for i := range out {
			completed, days := 0, 0
			for completed < 30 {
				for _, s := range slots {
					if c.Working(s.Name, days) {
						completed += s.Samples[rng.Intn(len(s.Samples))]
					}
				}
				days++
			}
			out[i] = days
		}
		return out
	}
	with, without := run(cal), run(nil)
	for i := range with {
		if with[i] != without[i]+k {
			t.Fatalf("trial %d: with calendar = %d, without = %d, want exactly +%d", i, with[i], without[i], k)
		}
	}

	// And the real engine agrees on the aggregate: sorted distributions shift
	// by exactly k.
	a := simulateDays(slots, cal, 30, 1000, 1, 7, nil)
	b := simulateDays(slots, nil, 30, 1000, 1, 7, nil)
	for i := range a {
		if a[i] != b[i]+k {
			t.Fatalf("sorted[%d]: with = %d, without = %d, want +%d", i, a[i], b[i], k)
		}
	}
}

func TestProbabilityAtLeast(t *testing.T) {
	dist := []int{1, 2, 3, 4}
	cases := []struct {
		n    int
		want float64
	}{
		{1, 100.0}, // all 4 are >= 1
		{3, 50.0},  // 3 and 4 -> 2/4
		{4, 25.0},  // only 4 -> 1/4
		{5, 0.0},   // none
	}
	for _, c := range cases {
		got := ProbabilityAtLeast(dist, c.n)
		if diff := got - c.want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("ProbabilityAtLeast(dist, %d) = %v, want %v", c.n, got, c.want)
		}
	}
	if got := ProbabilityAtLeast(nil, 1); got != 0 {
		t.Errorf("ProbabilityAtLeast(empty) = %v, want 0", got)
	}
}

func TestItemsAtConfidence_RoundTrip(t *testing.T) {
	dists := [][]int{
		{1, 2, 3, 4},
		{5, 5, 5, 5},
		{1, 1, 2, 3, 5, 8, 13, 21, 34},
		{10},
		{0, 0, 0, 1, 100},
	}
	confidences := []float64{0, 1, 15, 50, 85, 99, 100}

	for _, dist := range dists {
		for _, c := range confidences {
			n := ItemsAtConfidence(dist, c)
			if got := ProbabilityAtLeast(dist, n); got < c {
				t.Errorf("dist=%v confidence=%v: ItemsAtConfidence=%d, but ProbabilityAtLeast(dist, %d)=%v, want >= %v", dist, c, n, n, got, c)
			}
			if got := ProbabilityAtLeast(dist, n+1); got >= c && n < dist[len(dist)-1] {
				t.Errorf("dist=%v confidence=%v: ItemsAtConfidence=%d is not the largest such n; ProbabilityAtLeast(dist, %d)=%v still >= %v", dist, c, n, n+1, got, c)
			}
		}
	}
}

func TestItemsAtConfidence_Monotonic(t *testing.T) {
	dist := []int{1, 1, 2, 3, 5, 8, 13, 21, 34}
	prev := ItemsAtConfidence(dist, 0)
	for c := 1; c <= 100; c++ {
		got := ItemsAtConfidence(dist, float64(c))
		if got > prev {
			t.Errorf("ItemsAtConfidence not monotonic: confidence %d gave %d, higher than confidence %d's %d", c, got, c-1, prev)
		}
		prev = got
	}
}

func TestItemsAtConfidence_Edges(t *testing.T) {
	dist := []int{1, 2, 3, 4, 5}
	if got := ItemsAtConfidence(dist, 100); got != 1 {
		t.Errorf("ItemsAtConfidence(dist, 100) = %d, want 1 (the minimum)", got)
	}
	if got := ItemsAtConfidence(dist, 0); got != 5 {
		t.Errorf("ItemsAtConfidence(dist, 0) = %d, want 5 (the maximum)", got)
	}
	if got := ItemsAtConfidence(dist, -10); got != ItemsAtConfidence(dist, 0) {
		t.Errorf("ItemsAtConfidence(dist, -10) = %d, want same as confidence=0 (clamped)", got)
	}
	if got := ItemsAtConfidence(dist, 200); got != ItemsAtConfidence(dist, 100) {
		t.Errorf("ItemsAtConfidence(dist, 200) = %d, want same as confidence=100 (clamped)", got)
	}
	if got := ItemsAtConfidence(nil, 50); got != 0 {
		t.Errorf("ItemsAtConfidence(empty) = %d, want 0", got)
	}
	single := []int{7}
	if got := ItemsAtConfidence(single, 50); got != 7 {
		t.Errorf("ItemsAtConfidence(single, 50) = %d, want 7", got)
	}
}

// TestItemsAtConfidence_ConservativeDirection guards the actual bug this
// function exists to fix: for confidence > 50, the floor must be at or below
// the median, not above it (a naive PercentileValue(dist, confidence) call
// would read the wrong tail).
func TestItemsAtConfidence_ConservativeDirection(t *testing.T) {
	dist := make([]int, 0, 100)
	for i := 1; i <= 100; i++ {
		dist = append(dist, i)
	}
	median := PercentileValue(dist, 50)
	got := ItemsAtConfidence(dist, 85)
	if got >= median {
		t.Errorf("ItemsAtConfidence(dist, 85) = %d, want < median (%d) — 85%% confidence must be conservative, not optimistic", got, median)
	}
}
