package history

import (
	"math"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/cfd"
	"github.com/commondatageek/delivery-forecast/simulate"
)

// day returns local midnight of the given YYYY-MM-DD date, matching
// util.LocalDay so fixtures need no further truncation.
func day(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

// at returns a local timestamp on the given date at hh:mm, used to build
// non-midnight-aligned fixtures for the day-truncation cross-check.
func at(s string, hh, mm int) time.Time {
	d := day(s)
	return d.Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute)
}

// tier1Fixture is the 5-issue, ~10-day set used by the Tier 1 correctness
// test and the cfd cross-check. Hand-computed expectations live alongside
// the callers.
func tier1Fixture() []Issue {
	return []Issue{
		// I1: created 1, started 2, completed 5.
		{CreatedAt: day("2025-01-01"), StartedAt: day("2025-01-02"), CompletedAt: day("2025-01-05")},
		// I2: created 2, started 3, completed 9.
		{CreatedAt: day("2025-01-02"), StartedAt: day("2025-01-03"), CompletedAt: day("2025-01-09")},
		// I3: created 3, never started, never terminal (pure backlog).
		{CreatedAt: day("2025-01-03")},
		// I4: created 4, started 5, canceled 7.
		{CreatedAt: day("2025-01-04"), StartedAt: day("2025-01-05"), CanceledAt: day("2025-01-07")},
		// I5: created 6, started 6, still in progress.
		{CreatedAt: day("2025-01-06"), StartedAt: day("2025-01-06")},
	}
}

type tier1Want struct {
	total, completed, canceled, backlog, inProgress, remaining int
}

func TestCompute_Tier1Correctness(t *testing.T) {
	issues := tier1Fixture()
	res, err := Compute(issues, Options{Start: day("2025-01-01"), End: day("2025-01-10")})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(res.Rows) != 10 {
		t.Fatalf("expected 10 rows, got %d", len(res.Rows))
	}

	want := map[string]tier1Want{
		"2025-01-01": {total: 1, completed: 0, canceled: 0, backlog: 1, inProgress: 0, remaining: 1},
		"2025-01-02": {total: 2, completed: 0, canceled: 0, backlog: 1, inProgress: 1, remaining: 2},
		"2025-01-03": {total: 3, completed: 0, canceled: 0, backlog: 1, inProgress: 2, remaining: 3},
		"2025-01-04": {total: 4, completed: 0, canceled: 0, backlog: 2, inProgress: 2, remaining: 4},
		"2025-01-05": {total: 4, completed: 1, canceled: 0, backlog: 1, inProgress: 2, remaining: 3},
		"2025-01-06": {total: 5, completed: 1, canceled: 0, backlog: 1, inProgress: 3, remaining: 4},
		"2025-01-07": {total: 5, completed: 1, canceled: 1, backlog: 1, inProgress: 2, remaining: 3},
		"2025-01-08": {total: 5, completed: 1, canceled: 1, backlog: 1, inProgress: 2, remaining: 3},
		"2025-01-09": {total: 5, completed: 2, canceled: 1, backlog: 1, inProgress: 1, remaining: 2},
		"2025-01-10": {total: 5, completed: 2, canceled: 1, backlog: 1, inProgress: 1, remaining: 2},
	}

	for _, r := range res.Rows {
		date := r.Date.Format("2006-01-02")
		w, ok := want[date]
		if !ok {
			t.Fatalf("unexpected date %s in results", date)
		}
		if r.Total != w.total || r.Completed != w.completed || r.Canceled != w.canceled ||
			r.Backlog != w.backlog || r.InProgress != w.inProgress || r.Remaining != w.remaining {
			t.Errorf("%s: got {total:%d completed:%d canceled:%d backlog:%d inProgress:%d remaining:%d}, want %+v",
				date, r.Total, r.Completed, r.Canceled, r.Backlog, r.InProgress, r.Remaining, w)
		}
	}
}

func TestCompute_Invariants(t *testing.T) {
	res, err := Compute(tier1Fixture(), Options{Start: day("2025-01-01"), End: day("2025-01-10")})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if err := AssertInvariants(res.Rows); err != nil {
		t.Errorf("unexpected invariant violation: %v", err)
	}

	corrupt := []DayRow{
		{Date: day("2025-01-01"), Total: 5, Completed: 1, Canceled: 0, InProgress: 1, Backlog: 1, Remaining: 4}, // band sum (3) != Total (5)
	}
	if err := AssertInvariants(corrupt); err == nil {
		t.Error("expected invariant violation for corrupt rows, got nil")
	}

	corruptMonotonic := []DayRow{
		{Date: day("2025-01-01"), Total: 5, Completed: 0, Canceled: 0, InProgress: 0, Backlog: 5, Remaining: 5},
		{Date: day("2025-01-02"), Total: 3, Completed: 0, Canceled: 0, InProgress: 0, Backlog: 3, Remaining: 3},
	}
	if err := AssertInvariants(corruptMonotonic); err == nil {
		t.Error("expected invariant violation for non-monotonic Total, got nil")
	}
}

func TestCompute_CrossCheckAgainstCFD(t *testing.T) {
	issues := tier1Fixture()
	start, end := day("2025-01-01"), day("2025-01-10")

	res, err := Compute(issues, Options{Start: start, End: end})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	var normalized []cfd.NormalizedIssue
	for _, it := range issues {
		ni, ok := cfd.Normalize(cfd.Issue{
			CreatedAt: it.CreatedAt, StartedAt: it.StartedAt,
			CompletedAt: it.CompletedAt, CanceledAt: it.CanceledAt,
		})
		if ok {
			normalized = append(normalized, ni)
		}
	}
	cfdRows := cfd.BuildGrid(normalized, start, end)

	if len(cfdRows) != len(res.Rows) {
		t.Fatalf("row count mismatch: history %d, cfd %d", len(res.Rows), len(cfdRows))
	}
	for i := range res.Rows {
		h, c := res.Rows[i], cfdRows[i]
		if !h.Date.Equal(c.Date) {
			t.Fatalf("row %d: date mismatch: history %s, cfd %s", i, h.Date, c.Date)
		}
		if h.Total != c.Created {
			t.Errorf("%s: Total %d != cfd.Created %d", h.Date.Format("2006-01-02"), h.Total, c.Created)
		}
		if h.Completed != c.Completed {
			t.Errorf("%s: Completed %d != cfd.Completed %d", h.Date.Format("2006-01-02"), h.Completed, c.Completed)
		}
		if h.Backlog != c.Backlog {
			t.Errorf("%s: Backlog %d != cfd.Backlog %d", h.Date.Format("2006-01-02"), h.Backlog, c.Backlog)
		}
		if h.InProgress != c.InProgress {
			t.Errorf("%s: InProgress %d != cfd.InProgress %d", h.Date.Format("2006-01-02"), h.InProgress, c.InProgress)
		}
	}
}

func TestCompute_CrossCheckAgainstSimulateCountAsOf(t *testing.T) {
	// Mostly midnight-aligned, like tier1Fixture, plus one issue completed
	// mid-afternoon rather than at local midnight.
	issues := append(tier1Fixture(),
		// I6: created 1am, started 2 (2pm), completed 5 (6pm) — a
		// non-midnight completion, to exercise D4's day-truncation.
		Issue{CreatedAt: at("2025-01-01", 1, 0), StartedAt: at("2025-01-02", 14, 0), CompletedAt: at("2025-01-05", 18, 0)},
	)
	start, end := day("2025-01-01"), day("2025-01-10")

	res, err := Compute(issues, Options{Start: start, End: end})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	var btItems []simulate.BacktestItem
	for _, it := range issues {
		btItems = append(btItems, simulate.BacktestItem{
			CreatedAt: it.CreatedAt, StartedAt: it.StartedAt, CompletedAt: it.CompletedAt,
		})
	}

	for _, r := range res.Rows {
		wantCompleted, _ := simulate.CountAsOf(btItems, r.Date)
		date := r.Date.Format("2006-01-02")
		if date == "2025-01-05" {
			// Intended divergence (D4): CountAsOf compares the raw
			// (non-truncated) completed_at against local-midnight d, so I6's
			// 18:00 completion doesn't count until the 6th; history
			// day-truncates completed_at first, so it counts on the 5th.
			// Assert history's value, not CountAsOf's.
			if r.Completed != 2 {
				t.Errorf("%s: history Completed = %d, want 2 (I1 and I6, day-truncated)", date, r.Completed)
			}
			if wantCompleted != 1 {
				t.Errorf("%s: sanity check failed — expected CountAsOf to lag by one day (got %d)", date, wantCompleted)
			}
			continue
		}
		if r.Completed != wantCompleted {
			t.Errorf("%s: history Completed %d != simulate.CountAsOf %d", date, r.Completed, wantCompleted)
		}
	}
}

func TestCompute_Deltas(t *testing.T) {
	res, err := Compute(tier1Fixture(), Options{Start: day("2025-01-01"), End: day("2025-01-10")})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	last := res.Rows[len(res.Rows)-1]

	var createdSum, completedSum, canceledSum, startedSum int
	for _, r := range res.Rows {
		createdSum += r.CreatedDelta
		completedSum += r.CompletedDelta
		canceledSum += r.CanceledDelta
		startedSum += r.StartedDelta
	}
	if createdSum != last.Total {
		t.Errorf("sum(CreatedDelta) = %d, want %d", createdSum, last.Total)
	}
	if completedSum != last.Completed {
		t.Errorf("sum(CompletedDelta) = %d, want %d", completedSum, last.Completed)
	}
	if canceledSum != last.Canceled {
		t.Errorf("sum(CanceledDelta) = %d, want %d", canceledSum, last.Canceled)
	}
	wantStarted := last.InProgress + last.Completed + last.Canceled
	if startedSum != wantStarted {
		t.Errorf("sum(StartedDelta) = %d, want %d", startedSum, wantStarted)
	}
}

// rollingFixture gives five issues a known completion cadence: all created
// and started on day 1, completed every other day starting day 2.
func rollingFixture() []Issue {
	var out []Issue
	for i, completedDay := range []string{"2025-01-02", "2025-01-04", "2025-01-06", "2025-01-08", "2025-01-10"} {
		_ = i
		out = append(out, Issue{
			CreatedAt:   day("2025-01-01"),
			StartedAt:   day("2025-01-01"),
			CompletedAt: day(completedDay),
		})
	}
	return out
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestCompute_RollingMetrics(t *testing.T) {
	res, err := Compute(rollingFixture(), Options{Start: day("2025-01-01"), End: day("2025-01-12"), WindowDays: 8})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	rowByDate := map[string]DayRow{}
	for _, r := range res.Rows {
		rowByDate[r.Date.Format("2006-01-02")] = r
	}

	// Day 10: trailing 7d window (Jan3, Jan10] holds completions on 4,6,8,10 = 4.
	// Trailing 8d window (Jan2, Jan10] holds completions on 4,6,8,10 = 4.
	r10 := rowByDate["2025-01-10"]
	if !almostEqual(r10.Throughput7d, 4.0/7.0) {
		t.Errorf("Jan10 Throughput7d = %v, want %v", r10.Throughput7d, 4.0/7.0)
	}
	if !almostEqual(r10.ThroughputWindow, 4.0/8.0) {
		t.Errorf("Jan10 ThroughputWindow = %v, want %v", r10.ThroughputWindow, 4.0/8.0)
	}

	// Day 6: trailing 7d window (Dec30, Jan6] holds completions on 2,4,6 = 3.
	// Trailing 8d window (Dec29, Jan6] holds completions on 2,4,6 = 3.
	r6 := rowByDate["2025-01-06"]
	if !almostEqual(r6.Throughput7d, 3.0/7.0) {
		t.Errorf("Jan6 Throughput7d = %v, want %v", r6.Throughput7d, 3.0/7.0)
	}
	if !almostEqual(r6.ThroughputWindow, 3.0/8.0) {
		t.Errorf("Jan6 ThroughputWindow = %v, want %v", r6.ThroughputWindow, 3.0/8.0)
	}
}

func TestCompute_UndefinedValues(t *testing.T) {
	res, err := Compute(rollingFixture(), Options{Start: day("2025-01-01"), End: day("2025-01-12"), WindowDays: 8})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	rowByDate := map[string]DayRow{}
	for _, r := range res.Rows {
		rowByDate[r.Date.Format("2006-01-02")] = r
	}

	// Jan1: all five issues just started; no completions have happened yet
	// anywhere in the trailing window, so throughput/percentiles are NaN,
	// and the LittlesLawCT/DaysRemainingAtRate divisions by that NaN must
	// stay NaN rather than becoming +Inf despite InProgress/Remaining > 0.
	r1 := rowByDate["2025-01-01"]
	if r1.InProgress != 5 || r1.Remaining != 5 {
		t.Fatalf("sanity check failed: Jan1 InProgress=%d Remaining=%d, want 5, 5", r1.InProgress, r1.Remaining)
	}
	for name, v := range map[string]float64{
		"Throughput7d": r1.Throughput7d, "ThroughputWindow": r1.ThroughputWindow,
		"LeadTimeP50": r1.LeadTimeP50, "LeadTimeP85": r1.LeadTimeP85,
		"CycleTimeP50": r1.CycleTimeP50, "CycleTimeP85": r1.CycleTimeP85,
		"LittlesLawCT": r1.LittlesLawCT, "DaysRemainingAtRate": r1.DaysRemainingAtRate,
	} {
		if !math.IsNaN(v) {
			t.Errorf("Jan1 %s = %v, want NaN", name, v)
		}
	}

	// Jan12: every issue is long completed, so there is no WIP left.
	r12 := rowByDate["2025-01-12"]
	if r12.InProgress != 0 {
		t.Fatalf("sanity check failed: Jan12 InProgress=%d, want 0", r12.InProgress)
	}
	for name, v := range map[string]float64{
		"WIPAgeAvg": r12.WIPAgeAvg, "WIPAgeP85": r12.WIPAgeP85, "WIPAgeMax": r12.WIPAgeMax,
	} {
		if !math.IsNaN(v) {
			t.Errorf("Jan12 %s = %v, want NaN", name, v)
		}
	}
}

func TestCompute_LeadTimeExceedsCycleTime(t *testing.T) {
	issues := []Issue{
		{CreatedAt: day("2025-01-01"), StartedAt: day("2025-01-05"), CompletedAt: day("2025-01-06")},
	}
	res, err := Compute(issues, Options{Start: day("2025-01-06"), End: day("2025-01-06"), WindowDays: 28})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	r := res.Rows[0]
	if math.IsNaN(r.LeadTimeP50) || math.IsNaN(r.CycleTimeP50) {
		t.Fatalf("expected defined lead/cycle time, got %v / %v", r.LeadTimeP50, r.CycleTimeP50)
	}
	if !(r.LeadTimeP50 > r.CycleTimeP50) {
		t.Errorf("LeadTimeP50 (%v) should exceed CycleTimeP50 (%v) when created well before started", r.LeadTimeP50, r.CycleTimeP50)
	}
	if !almostEqual(r.LeadTimeP50, 5) {
		t.Errorf("LeadTimeP50 = %v, want 5", r.LeadTimeP50)
	}
	if !almostEqual(r.CycleTimeP50, 1) {
		t.Errorf("CycleTimeP50 = %v, want 1", r.CycleTimeP50)
	}
}

func TestCompute_NoEarlyExit(t *testing.T) {
	issues := []Issue{
		{CreatedAt: day("2025-01-01"), StartedAt: day("2025-01-01"), CompletedAt: day("2025-01-02")},
	}
	start, end := day("2025-01-01"), day("2025-01-20")
	res, err := Compute(issues, Options{Start: start, End: end})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	wantDays := int(end.Sub(start).Hours()/24) + 1
	if len(res.Rows) != wantDays {
		t.Fatalf("got %d rows, want %d (should not exit early once the issue set is fully complete)", len(res.Rows), wantDays)
	}
	last := res.Rows[len(res.Rows)-1]
	if !last.Date.Equal(end) {
		t.Errorf("last row date = %s, want %s", last.Date, end)
	}
}

func TestCompute_ValidatesOptions(t *testing.T) {
	if _, err := Compute(nil, Options{}); err == nil {
		t.Error("expected error for zero Start/End")
	}
	if _, err := Compute(nil, Options{Start: day("2025-01-05"), End: day("2025-01-01")}); err == nil {
		t.Error("expected error for End before Start")
	}
}

func TestCompute_SkipsIssuesWithNoCreatedAt(t *testing.T) {
	issues := []Issue{
		{CreatedAt: day("2025-01-01"), CompletedAt: day("2025-01-02")},
		{CompletedAt: day("2025-01-02")}, // no CreatedAt: dropped
	}
	res, err := Compute(issues, Options{Start: day("2025-01-01"), End: day("2025-01-02")})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if res.TotalIssues != 2 {
		t.Errorf("TotalIssues = %d, want 2", res.TotalIssues)
	}
	if res.SkippedIssues != 1 {
		t.Errorf("SkippedIssues = %d, want 1", res.SkippedIssues)
	}
}

func TestEarliestCreatedAt(t *testing.T) {
	issues := []Issue{
		{CreatedAt: day("2025-03-01")},
		{CreatedAt: day("2025-01-15")},
		{},
		{CreatedAt: day("2025-02-01")},
	}
	got := EarliestCreatedAt(issues)
	if !got.Equal(day("2025-01-15")) {
		t.Errorf("EarliestCreatedAt = %v, want %v", got, day("2025-01-15"))
	}
	if got := EarliestCreatedAt(nil); !got.IsZero() {
		t.Errorf("EarliestCreatedAt(nil) = %v, want zero", got)
	}
}
