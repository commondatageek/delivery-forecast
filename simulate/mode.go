package simulate

import (
	"fmt"
	"strconv"
	"strings"
)

// Mode is which of the two mutually-exclusive sampling strategies a simulation uses.
type Mode int

const (
	ModeAnonymous Mode = iota // pooled anonymous engineers
	ModeFullTeam              // summed whole-team series
)

// ResolveMode enforces that exactly one of -engineers and -whole-team is given
// and reports the selected mode. engineersSet must report whether -engineers
// was explicitly passed (its default value is otherwise indistinguishable from
// an unset flag). There is no implicit default mode: an anonymous-engineers
// run silently assuming some fixed team size would produce a plausible-looking
// but potentially wrong forecast, so the caller must state one explicitly.
func ResolveMode(engineersSet, wholeTeam bool) (Mode, error) {
	switch {
	case wholeTeam && engineersSet:
		return 0, fmt.Errorf("-whole-team and -engineers are mutually exclusive")
	case wholeTeam:
		return ModeFullTeam, nil
	case engineersSet:
		return ModeAnonymous, nil
	default:
		return 0, fmt.Errorf("one of -engineers or -whole-team must be specified")
	}
}

// ModeLabel returns the noun phrase describing the run, e.g. "whole-team
// throughput", "3 equivalent engineers", or "3 equivalent engineers [alice,
// bob, carol]" when the engineers were named.
func ModeLabel(mode Mode, engineers int, names []string) string {
	switch mode {
	case ModeFullTeam:
		return "whole-team throughput"
	default:
		label := fmt.Sprintf("%d equivalent engineers", engineers)
		if len(names) > 0 {
			label += " [" + strings.Join(names, ", ") + "]"
		}
		return label
	}
}

// ValidatePool ensures the chosen mode has samples to draw from before any
// simulation runs, turning what would otherwise be a rng.Intn(0) panic into a
// clear, actionable error. Both modes need a non-empty series.
//
// requireProgress must be true for callers whose simulation loop runs until a
// target item count is reached (SimulateDaysToComplete) rather than for a
// fixed number of days. For those, a pool that sums to zero is a guaranteed
// infinite loop, so it's rejected outright. Fixed-day callers pass false: an
// all-zero pool is a legitimate "0 items" / "0% probability" answer, not an
// error.
func ValidatePool(pool *SamplePool, mode Mode, requireProgress bool) error {
	switch mode {
	case ModeFullTeam:
		samples := pool.PerEngineer[WholeTeamKey]
		if len(samples) == 0 {
			return fmt.Errorf("no sample days in the selected window (try a different -sample-start/-sample-end)")
		}
		if requireProgress && Sum(samples) == 0 {
			return fmt.Errorf("whole-team throughput was 0 in the selected window; days-to-complete is undefined (it would never finish)")
		}
	default: // ModeAnonymous
		samples := pool.Combined
		if len(samples) == 0 {
			return fmt.Errorf("no completed items in the selected window (try a different -sample-start/-sample-end)")
		}
		if requireProgress && Sum(samples) == 0 {
			return fmt.Errorf("0 items completed in the selected window; days-to-complete is undefined (it would never finish)")
		}
	}
	return nil
}

// ParseEngineers parses an -engineers value: a positive integer is a count
// of anonymous equivalent engineers; anything else is a comma-separated list
// of unique names (count = len(names)). It is the parsing rule formerly
// inside cmd/forecast's engineerSpec.Set, moved verbatim with its error
// messages.
func ParseEngineers(v string) (count int, names []string, err error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil, fmt.Errorf("-engineers: empty value")
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n <= 0 {
			return 0, nil, fmt.Errorf("-engineers: count must be positive, got %d", n)
		}
		return n, nil, nil
	}

	seen := make(map[string]bool)
	for part := range strings.SplitSeq(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if isAllDigits(part) {
			return 0, nil, fmt.Errorf("-engineers: %q looks like a count inside a name list", part)
		}
		if seen[part] {
			return 0, nil, fmt.Errorf("-engineers: duplicate name %q", part)
		}
		seen[part] = true
		names = append(names, part)
	}
	if len(names) == 0 {
		return 0, nil, fmt.Errorf("-engineers: no names in %q", v)
	}
	return len(names), names, nil
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
