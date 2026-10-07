package simulate

import (
	"fmt"
	"sort"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
)

// WholeTeamKey is the SamplePool.PerEngineer key used in whole-team mode,
// where all engineers' daily counts are summed into a single series.
const WholeTeamKey = "__whole_team__"

// Completion is a normalized record of a completed unit of work: the engineer
// who completed it and when. The cmd layer converts source-specific records
// (e.g. issues.Issue) to Completion before building a pool.
type Completion struct {
	Engineer    string
	CompletedAt time.Time
}

// FilterInvalid removes Completion records with an empty Engineer or zero
// CompletedAt and returns the count of dropped records. CompletedBetween
// already filters these out in practice; this is belt-and-suspenders in case
// a future loader lets a bad record through.
func FilterInvalid(records []Completion) ([]Completion, int) {
	out := make([]Completion, 0, len(records))
	skipped := 0
	for _, r := range records {
		if r.Engineer == "" || r.CompletedAt.IsZero() {
			skipped++
			continue
		}
		out = append(out, r)
	}
	return out, skipped
}

// SamplePool holds per-engineer slices of daily completion counts, plus the
// precomputed flattened view (Combined) that anonymous-mode simulations draw
// from. Construct via NewSamplePool (or BuildPool) so Combined is always
// populated; a zero-value SamplePool leaves it nil.
type SamplePool struct {
	PerEngineer map[string][]int
	Combined    []int
}

// NewSamplePool builds a SamplePool from per-engineer sample slices,
// precomputing Combined so simulation code never has to derive it.
func NewSamplePool(perEngineer map[string][]int) *SamplePool {
	return &SamplePool{
		PerEngineer: perEngineer,
		Combined:    combineSamples(perEngineer),
	}
}

// Slots derives the simulation's slots from p: whole-team mode is one
// anonymous slot over the summed series; anonymous mode is p.Engineers slots
// over Combined, named after p.EngineerNames when given.
func (sp *SamplePool) Slots(p Params) []Slot {
	if p.Mode == ModeFullTeam {
		return []Slot{{Samples: sp.PerEngineer[WholeTeamKey]}}
	}
	slots := anonymousSlots(sp.Combined, p.Engineers)
	for i := range slots {
		if i < len(p.EngineerNames) {
			slots[i].Name = p.EngineerNames[i]
		}
	}
	return slots
}

// combineSamples concatenates all engineers' samples into a flat slice,
// ordered by engineer name so the result (and thus anything sampled from it
// under a pinned seed) is deterministic across runs.
func combineSamples(perEngineer map[string][]int) []int {
	names := make([]string, 0, len(perEngineer))
	for name := range perEngineer {
		names = append(names, name)
	}
	sort.Strings(names)

	var combined []int
	for _, name := range names {
		combined = append(combined, perEngineer[name]...)
	}
	return combined
}

// DaysBetween returns the number of per-day sample slots in [start, end).
// end is normally a calendar date at midnight, in which case that day is
// fully excluded. If end carries a time-of-day (e.g. it's "now"), the day
// it falls on is partially in range, so it gets one inclusive slot.
func DaysBetween(start, end time.Time) int {
	endDay := util.LocalDay(end)
	days := util.DayIndex(endDay, start)
	if !end.Equal(endDay) {
		days++
	}
	return days
}

// BuildPool bins completions into per-engineer daily completion counts over the
// half-open window [startDate, endDate), drops the days cal marks as
// non-working, and returns the resulting SamplePool. It is pure (no
// file/DB/clock access). cal must be anchored at startDate (or be nil, meaning
// every day works); a mismatch is a programming error and panics, since a
// shifted calendar would silently drop the wrong days.
//
// The pool deliberately preserves zero-completion days: each engineer's slice
// has one slot per non-excluded day in the window, so a day with no completions
// contributes a 0 sample. Dropping those would bias every forecast upward.
//
// The engineer set is derived solely from records: an engineer appears in the
// pool only if they have at least one completion inside the window. Completions
// outside the window are ignored entirely (neither counted nor do they create
// an engineer). In whole-team mode all engineers are summed into a single
// WholeTeamKey series, which honors only the calendar's global rules.
func BuildPool(records []Completion, cal *Calendar, startDate, endDate time.Time, wholeTeam bool) *SamplePool {
	if cal != nil && !cal.Anchor().Equal(util.LocalDay(startDate)) {
		panic(fmt.Sprintf("simulate.BuildPool: calendar anchored at %s but startDate is %s",
			cal.Anchor().Format("2006-01-02"), util.LocalDay(startDate).Format("2006-01-02")))
	}
	totalDays := DaysBetween(startDate, endDate)

	type engData struct {
		counts []int
	}
	engineers := make(map[string]*engData)
	for _, r := range records {
		t := util.LocalDay(r.CompletedAt)
		idx := util.DayIndex(t, startDate)
		if idx < 0 || idx >= totalDays {
			continue
		}
		eng, ok := engineers[r.Engineer]
		if !ok {
			eng = &engData{counts: make([]int, totalDays)}
			engineers[r.Engineer] = eng
		}
		eng.counts[idx]++
	}

	perEngineer := make(map[string][]int)

	if wholeTeam {
		teamCounts := make([]int, totalDays)
		for _, eng := range engineers {
			for i, count := range eng.counts {
				teamCounts[i] += count
			}
		}
		var teamSamples []int
		for i, count := range teamCounts {
			if cal.Working("", i) {
				teamSamples = append(teamSamples, count)
			}
		}
		perEngineer[WholeTeamKey] = teamSamples
	} else {
		for name, eng := range engineers {
			var engineerSamples []int
			for i, count := range eng.counts {
				if cal.Working(name, i) {
					engineerSamples = append(engineerSamples, count)
				}
			}
			perEngineer[name] = engineerSamples
		}
	}

	return NewSamplePool(perEngineer)
}
