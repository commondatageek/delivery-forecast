package simulate

import "testing"

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

func TestSimulateItemsInDaysPerEngineer_ConstantPool(t *testing.T) {
	pool := &SamplePool{PerEngineer: map[string][]int{
		"alice": {2},
		"bob":   {3},
	}}
	got := SimulateItemsInDaysPerEngineer(pool, []string{"alice", "bob"}, 10, 1000, 4, 42, nil)
	assertAll(t, got, 50) // (2+3) per day * 10 days
}

func TestSimulateDaysToCompletePerEngineer_ConstantPool(t *testing.T) {
	pool := &SamplePool{PerEngineer: map[string][]int{
		"alice": {2},
		"bob":   {3},
	}}
	got := SimulateDaysToCompletePerEngineer(pool, []string{"alice", "bob"}, 10, 1000, 4, 42, nil)
	assertAll(t, got, 2) // 5/day, need 10 -> 2 days
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
