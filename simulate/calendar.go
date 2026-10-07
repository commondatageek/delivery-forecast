package simulate

import (
	"sort"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
)

// Calendar answers "is this scope working on day d?", where d counts whole
// days from anchor (d = 0 is the anchor date). Scope "" is everyone; a name
// is that engineer and also inherits the global rules. A nil *Calendar means
// every day is a working day.
//
// It is rule-based and anchored to a real date on purpose: the explicit-date
// rules come from an Exclusions file, and weekdaysOff is the hook for a
// future "weekends off" flag (unused this round). Both the sample pool
// (anchored at the sample start) and the forecast engine (anchored at the
// target start) use the same type, so the two sides cannot define "working
// day" differently.
type Calendar struct {
	anchor      time.Time // local midnight
	weekdaysOff [7]bool   // indexed by time.Weekday; all false this round
	global      map[int]bool
	perName     map[string]map[int]bool
}

// NewCalendar builds a Calendar from exc anchored at anchor (normalized to
// local midnight via util.LocalDay).
func NewCalendar(exc Exclusions, anchor time.Time) *Calendar {
	c := &Calendar{
		anchor:  util.LocalDay(anchor),
		global:  make(map[int]bool),
		perName: make(map[string]map[int]bool),
	}
	for _, d := range exc.Days("") {
		c.global[util.DayIndex(d, c.anchor)] = true
	}
	for _, name := range exc.Scopes() {
		m := make(map[int]bool)
		for _, d := range exc.Days(name) {
			m[util.DayIndex(d, c.anchor)] = true
		}
		c.perName[name] = m
	}
	return c
}

// Anchor returns the local-midnight date that day index 0 refers to.
func (c *Calendar) Anchor() time.Time { return c.anchor }

// Working reports whether scope name works on day index d (d may be any
// int; indices before the anchor are answered honestly). nil receiver → true.
func (c *Calendar) Working(name string, d int) bool {
	if c == nil {
		return true
	}
	// Normalizing keeps the weekday lookup safe for negative d.
	if c.weekdaysOff[((int(c.anchor.Weekday())+d)%7+7)%7] {
		return false
	}
	if c.global[d] {
		return false
	}
	if name != "" && c.perName[name][d] {
		return false
	}
	return true
}

// Rebase returns an equivalent Calendar whose day 0 is newAnchor.
// nil receiver → nil.
func (c *Calendar) Rebase(newAnchor time.Time) *Calendar {
	if c == nil {
		return nil
	}
	anchor := util.LocalDay(newAnchor)
	shift := util.DayIndex(anchor, c.anchor) // old index = new index + shift
	out := &Calendar{
		anchor:      anchor,
		weekdaysOff: c.weekdaysOff,
		global:      make(map[int]bool, len(c.global)),
		perName:     make(map[string]map[int]bool, len(c.perName)),
	}
	for d := range c.global {
		out.global[d-shift] = true
	}
	for name, m := range c.perName {
		nm := make(map[int]bool, len(m))
		for d := range m {
			nm[d-shift] = true
		}
		out.perName[name] = nm
	}
	return out
}

// OffDays returns, sorted, the dates in [from, to) on which scope name does
// not work (global rules included). to < 0 means "no upper bound" and
// returns every explicitly excluded date at or after from. nil → nil.
func (c *Calendar) OffDays(name string, from, to int) []time.Time {
	if c == nil {
		return nil
	}
	var idx []int
	if to < 0 {
		// Unbounded: only the explicit dates are enumerable; the weekday mask
		// would contribute infinitely many days and is deliberately left out.
		seen := make(map[int]bool)
		for d := range c.global {
			seen[d] = true
		}
		if name != "" {
			for d := range c.perName[name] {
				seen[d] = true
			}
		}
		for d := range seen {
			if d >= from {
				idx = append(idx, d)
			}
		}
		sort.Ints(idx)
	} else {
		for d := from; d < to; d++ {
			if !c.Working(name, d) {
				idx = append(idx, d)
			}
		}
	}
	if len(idx) == 0 {
		return nil
	}
	days := make([]time.Time, len(idx))
	for i, d := range idx {
		days[i] = c.anchor.AddDate(0, 0, d)
	}
	return days
}
