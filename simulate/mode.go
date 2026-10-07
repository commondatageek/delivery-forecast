package simulate

import "fmt"

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

// ModeLabel returns the noun phrase describing the run,
// e.g. "whole-team throughput" or "3 equivalent engineers".
func ModeLabel(mode Mode, engineers int) string {
	switch mode {
	case ModeFullTeam:
		return "whole-team throughput"
	default:
		return fmt.Sprintf("%d equivalent engineers", engineers)
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
