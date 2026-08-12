package simulate

import "testing"

// backtestFixture returns two items: one with a clean, midnight-aligned
// completion, and one completed mid-afternoon rather than at local midnight
// — the latter exercises D4 (HISTORY_PLAN.md): RunBacktest now counts
// completed/remaining via history.Compute, which day-truncates timestamps
// before counting, where the old inline CountAsOf loop compared them raw.
func backtestFixture() []BacktestItem {
	return []BacktestItem{
		// Non-midnight completion: truncates to day 5.
		{CreatedAt: day(2025, 1, 1), StartedAt: day(2025, 1, 1), CompletedAt: at("_", 2025, 1, 5).CompletedAt},
		// Clean, midnight-aligned completion on day 3.
		{CreatedAt: day(2025, 1, 2), StartedAt: day(2025, 1, 2), CompletedAt: day(2025, 1, 3)},
	}
}

// zeroPool is a constant SamplePool where every simulated trial produces
// exactly 0 items, regardless of Days or Engineers — so ProbabilityAtLeast
// is deterministically 0% whenever any items remain, isolating the
// Monte Carlo branch from the Remaining==0 shortcut for the test below.
func zeroPool() *SamplePool {
	return &SamplePool{Combined: []int{0}, PerEngineer: map[string][]int{WholeTeamKey: {0}}}
}

func TestRunBacktest_CountsAndShortcut(t *testing.T) {
	items := backtestFixture()
	start, target := day(2025, 1, 1), day(2025, 1, 10)

	rows := RunBacktest(zeroPool(), items, start, target, Params{
		Mode: ModeAnonymous, Engineers: 1, Simulations: 100, Workers: 1, Seed: 42,
	})

	wantDates := []struct {
		completed, remaining int
		prob                 float64
	}{
		{0, 1, 0},   // Jan 1: only item 1 created, nothing completed yet
		{0, 2, 0},   // Jan 2: both created, nothing completed yet
		{1, 1, 0},   // Jan 3: item 2 completes (midnight-aligned)
		{1, 1, 0},   // Jan 4: unchanged
		{2, 0, 100}, // Jan 5: item 1's 18:00 completion truncates to this day (D4) — both done, Remaining==0 shortcut fires
	}
	if len(rows) != len(wantDates) {
		t.Fatalf("got %d rows, want %d (early exit should trim the rest of the [start, target] window)", len(rows), len(wantDates))
	}
	for i, w := range wantDates {
		r := rows[i]
		if r.Completed != w.completed || r.Remaining != w.remaining || r.Prob != w.prob {
			t.Errorf("row %d (%s): got {completed:%d remaining:%d prob:%v}, want {completed:%d remaining:%d prob:%v}",
				i, r.Date.Format("2006-01-02"), r.Completed, r.Remaining, r.Prob, w.completed, w.remaining, w.prob)
		}
	}

	last := rows[len(rows)-1]
	if !last.Date.Equal(day(2025, 1, 5)) {
		t.Errorf("last row date = %s, want 2025-01-05 (day-truncated, not 01-06)", last.Date.Format("2006-01-02"))
	}
}

func TestRunBacktest_NoRemainingItemsStopsImmediately(t *testing.T) {
	items := []BacktestItem{
		{CreatedAt: day(2025, 1, 1), StartedAt: day(2025, 1, 1), CompletedAt: day(2025, 1, 1)},
	}
	start, target := day(2025, 1, 1), day(2025, 1, 30)

	rows := RunBacktest(zeroPool(), items, start, target, Params{
		Mode: ModeAnonymous, Engineers: 1, Simulations: 100, Workers: 1, Seed: 1,
	})

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (should exit immediately once the single item is complete)", len(rows))
	}
	if rows[0].Completed != 1 || rows[0].Remaining != 0 || rows[0].Prob != 100 {
		t.Errorf("row 0 = %+v, want {completed:1 remaining:0 prob:100}", rows[0])
	}
}
