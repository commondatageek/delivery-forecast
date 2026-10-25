package app

// Defaults shared by every adapter, so the CLI flags and a future GUI show the
// same values.
const (
	DefaultSimulations     = 10_000
	DefaultAgingPercentile = 85
	DefaultHistoryWindow   = 28
)

// DefaultConfidences returns a fresh copy of the default confidence levels
// for sim items/days (a fresh slice so callers can't mutate a shared one).
func DefaultConfidences() []int { return []int{50, 75, 85, 95} }
