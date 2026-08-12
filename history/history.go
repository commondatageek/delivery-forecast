package history

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/commondatageek/delivery-forecast/cfd"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

// defaultWindowDays is used when Options.WindowDays is zero.
const defaultWindowDays = 28

// Issue is the neutral per-issue input: the four lifecycle timestamps. It
// mirrors cfd.Issue minus StateType, which history does not need.
type Issue struct {
	CreatedAt   time.Time
	StartedAt   time.Time
	CompletedAt time.Time
	CanceledAt  time.Time
}

// Options controls the emitted window and the rolling-metric window.
type Options struct {
	// Start is the first day emitted, inclusive.
	Start time.Time
	// End is the last day emitted, inclusive.
	End time.Time
	// WindowDays is the trailing window for rolling metrics. Zero means 28.
	WindowDays int
}

// DayRow is one calendar day's metrics. See the package doc for exact
// definitions. Float fields are NaN when undefined.
type DayRow struct {
	Date time.Time

	Total      int
	Completed  int
	Canceled   int
	Backlog    int
	InProgress int
	Remaining  int

	CreatedDelta   int
	StartedDelta   int
	CompletedDelta int
	CanceledDelta  int

	Throughput7d        float64
	ThroughputWindow    float64
	ScopeGrowthWindow   float64
	NetFlowWindow       float64
	LeadTimeP50         float64
	LeadTimeP85         float64
	CycleTimeP50        float64
	CycleTimeP85        float64
	WIPAgeAvg           float64
	WIPAgeP85           float64
	WIPAgeMax           float64
	LittlesLawCT        float64
	DaysRemainingAtRate float64
}

// Result bundles the series with the metadata a renderer needs in its header.
type Result struct {
	Rows          []DayRow
	WindowDays    int
	TotalIssues   int // issues in the input set
	SkippedIssues int // dropped for having no created_at
}

// normalize converts Issues into cfd.NormalizedIssues, reusing cfd's
// clamping logic (see the package doc) rather than duplicating it. Issues
// with no CreatedAt are dropped and counted.
func normalize(items []Issue) ([]cfd.NormalizedIssue, int) {
	var out []cfd.NormalizedIssue
	skipped := 0
	for _, it := range items {
		ni, ok := cfd.Normalize(cfd.Issue{
			CreatedAt:   it.CreatedAt,
			StartedAt:   it.StartedAt,
			CompletedAt: it.CompletedAt,
			CanceledAt:  it.CanceledAt,
		})
		if !ok {
			skipped++
			continue
		}
		out = append(out, ni)
	}
	return out, skipped
}

// Compute builds the day series. Issues with no CreatedAt are skipped and
// counted in Result.SkippedIssues.
func Compute(items []Issue, opts Options) (Result, error) {
	if opts.Start.IsZero() || opts.End.IsZero() {
		return Result{}, fmt.Errorf("history: Start and End must both be set")
	}
	if opts.End.Before(opts.Start) {
		return Result{}, fmt.Errorf("history: End (%s) is before Start (%s)",
			opts.End.Format("2006-01-02"), opts.Start.Format("2006-01-02"))
	}

	windowDays := opts.WindowDays
	if windowDays == 0 {
		windowDays = defaultWindowDays
	}

	normalized, skipped := normalize(items)

	var rows []DayRow
	var prevTotal, prevCompleted, prevCanceled, prevStartedCum int
	for d, first := opts.Start, true; !d.After(opts.End); d, first = d.AddDate(0, 0, 1), false {
		var total, completed, canceled, startedCum int
		for _, ni := range normalized {
			if !ni.Arrival.After(d) {
				total++
			}
			if !ni.LeftBacklog.IsZero() && !ni.LeftBacklog.After(d) {
				startedCum++
			}
			if !ni.Exit.IsZero() && !ni.Exit.After(d) {
				switch ni.ExitType {
				case "completed":
					completed++
				case "canceled":
					canceled++
				}
			}
		}

		backlog := total - startedCum
		inProgress := startedCum - completed - canceled
		remaining := backlog + inProgress

		row := DayRow{
			Date:       d,
			Total:      total,
			Completed:  completed,
			Canceled:   canceled,
			Backlog:    backlog,
			InProgress: inProgress,
			Remaining:  remaining,
		}

		if first {
			row.CreatedDelta = total
			row.StartedDelta = startedCum
			row.CompletedDelta = completed
			row.CanceledDelta = canceled
		} else {
			row.CreatedDelta = total - prevTotal
			row.StartedDelta = startedCum - prevStartedCum
			row.CompletedDelta = completed - prevCompleted
			row.CanceledDelta = canceled - prevCanceled
		}
		prevTotal, prevCompleted, prevCanceled, prevStartedCum = total, completed, canceled, startedCum

		computeRolling(&row, normalized, d, windowDays, inProgress, remaining)

		rows = append(rows, row)
	}

	return Result{
		Rows:          rows,
		WindowDays:    windowDays,
		TotalIssues:   len(items),
		SkippedIssues: skipped,
	}, nil
}

// computeRolling fills in row's Tier 2 rolling-metric fields as of day d,
// using the full normalized issue set (not just issues within [Start, End])
// so the window can look back before the emitted range.
func computeRolling(row *DayRow, normalized []cfd.NormalizedIssue, d time.Time, windowDays, inProgress, remaining int) {
	window7Start := d.AddDate(0, 0, -7)
	windowNStart := d.AddDate(0, 0, -windowDays)

	var completions7, completionsN, arrivalsN, departuresN int
	var leadTimes, cycleTimes, wipAges []float64

	inWindow := func(t, start time.Time) bool {
		return !t.IsZero() && t.After(start) && !t.After(d)
	}

	for _, ni := range normalized {
		if ni.ExitType == "completed" && inWindow(ni.Exit, window7Start) {
			completions7++
		}
		if ni.ExitType == "completed" && inWindow(ni.Exit, windowNStart) {
			completionsN++
			leadTimes = append(leadTimes, ni.Exit.Sub(ni.Arrival).Hours()/24)
			cycleTimes = append(cycleTimes, ni.Exit.Sub(ni.LeftBacklog).Hours()/24)
		}
		if inWindow(ni.Arrival, windowNStart) {
			arrivalsN++
		}
		if inWindow(ni.Exit, windowNStart) {
			departuresN++
		}
		if !ni.LeftBacklog.IsZero() && !ni.LeftBacklog.After(d) && (ni.Exit.IsZero() || ni.Exit.After(d)) {
			wipAges = append(wipAges, d.Sub(ni.LeftBacklog).Hours()/24)
		}
	}

	row.Throughput7d = rateOrNaN(completions7, 7)
	row.ThroughputWindow = rateOrNaN(completionsN, windowDays)
	row.ScopeGrowthWindow = float64(arrivalsN) / float64(windowDays)
	row.NetFlowWindow = float64(arrivalsN-departuresN) / float64(windowDays)

	sort.Float64s(leadTimes)
	sort.Float64s(cycleTimes)
	row.LeadTimeP50 = percentileOrNaN(leadTimes, 50)
	row.LeadTimeP85 = percentileOrNaN(leadTimes, 85)
	row.CycleTimeP50 = percentileOrNaN(cycleTimes, 50)
	row.CycleTimeP85 = percentileOrNaN(cycleTimes, 85)

	sort.Float64s(wipAges)
	row.WIPAgeAvg = meanOrNaN(wipAges)
	row.WIPAgeP85 = percentileOrNaN(wipAges, 85)
	row.WIPAgeMax = maxOrNaN(wipAges)

	// NaN in ThroughputWindow (zero completions in the window) propagates
	// through these divisions automatically, yielding NaN rather than +Inf.
	row.LittlesLawCT = float64(inProgress) / row.ThroughputWindow
	row.DaysRemainingAtRate = float64(remaining) / row.ThroughputWindow
}

// rateOrNaN returns count/days, or NaN if count is zero: a rate with no
// underlying events is treated as undefined, not a measured zero, so that
// LittlesLawCT/DaysRemainingAtRate divide by NaN (producing NaN) rather than
// by a real zero (producing +Inf).
func rateOrNaN(count, days int) float64 {
	if count == 0 {
		return math.NaN()
	}
	return float64(count) / float64(days)
}

// percentileOrNaN returns util.PercentileValue(sorted, p), or NaN for an
// empty sample (PercentileValue would otherwise return 0, indistinguishable
// from a real zero-day percentile).
func percentileOrNaN(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	return util.PercentileValue(sorted, p)
}

// meanOrNaN returns the mean of vals, or NaN if vals is empty.
func meanOrNaN(vals []float64) float64 {
	if len(vals) == 0 {
		return math.NaN()
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// maxOrNaN returns the last element of sorted (ascending), or NaN if empty.
func maxOrNaN(sorted []float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	return sorted[len(sorted)-1]
}

// EarliestCreatedAt returns the minimum non-zero CreatedAt, or the zero time.
// Used by the CLI to default Options.Start.
func EarliestCreatedAt(items []Issue) time.Time {
	var earliest time.Time
	for _, it := range items {
		if it.CreatedAt.IsZero() {
			continue
		}
		if earliest.IsZero() || it.CreatedAt.Before(earliest) {
			earliest = it.CreatedAt
		}
	}
	return earliest
}

// AssertInvariants checks the identities in the package doc and returns a
// descriptive error on the first violation. Mirrors cfd.AssertInvariants.
func AssertInvariants(rows []DayRow) error {
	for i, r := range rows {
		if i > 0 {
			p := rows[i-1]
			if r.Total < p.Total {
				return fmt.Errorf("day %s: Total not monotonic (%d < %d)", r.Date.Format("2006-01-02"), r.Total, p.Total)
			}
			if r.Completed < p.Completed {
				return fmt.Errorf("day %s: Completed not monotonic (%d < %d)", r.Date.Format("2006-01-02"), r.Completed, p.Completed)
			}
			if r.Canceled < p.Canceled {
				return fmt.Errorf("day %s: Canceled not monotonic (%d < %d)", r.Date.Format("2006-01-02"), r.Canceled, p.Canceled)
			}
		}
		if sum := r.Completed + r.Canceled + r.InProgress + r.Backlog; sum != r.Total {
			return fmt.Errorf("day %s: band sum %d != Total %d", r.Date.Format("2006-01-02"), sum, r.Total)
		}
		if want := r.Total - r.Completed - r.Canceled; want != r.Remaining {
			return fmt.Errorf("day %s: Remaining %d != Total-Completed-Canceled %d", r.Date.Format("2006-01-02"), r.Remaining, want)
		}
	}
	return nil
}
