package simulate

// Params bundles the resolved simulation parameters for the mode-aware
// dispatch functions. Days is used by ItemsInDays; Items by DaysToComplete.
// Progress is optional (nil = no progress reporting). Every field mirrors a
// cmd/forecast flag (and thus a `-config` YAML key of the same name) once
// resolved to its typed, presence-checked value.
type Params struct {
	// Mode is resolved from the `-engineers`/`-whole-team` flags via
	// ResolveMode; there is no single `mode` flag or YAML key.
	Mode Mode
	// Engineers is the `-engineers` flag (ModeAnonymous only).
	Engineers int
	// EngineerNames is the `-engineers` names, if the flag was given names
	// rather than a count; len == Engineers when set (ModeAnonymous only).
	EngineerNames []string
	// Calendar is the horizon calendar anchored at the target start date; nil
	// means every day is a working day.
	Calendar *Calendar
	// Days is the `-days` flag (sim items; ignored by DaysToComplete).
	Days int
	// Items is the `-items` flag (sim days/probability; ignored by ItemsInDays).
	Items int
	// Simulations is the `-simulations` flag.
	Simulations int
	// Workers is the `-goroutines` flag.
	Workers int
	// Seed is the `-random-seed` flag, resolved to a concrete value
	// (time-based when the flag was left unset) via resolveSeed.
	Seed int64
	// Progress reports trial completion to RunSimulations; not flag-backed.
	Progress func(done, total int)
}

// ItemsInDays answers "how many items in Days days?", forwarding p.Progress
// to RunSimulations. The mode only decides which slots draw (see
// SamplePool.Slots); the engine itself is mode-agnostic.
func ItemsInDays(pool *SamplePool, p Params) []int {
	return simulateItems(pool.Slots(p), p.Calendar, p.Days, p.Simulations, p.Workers, p.Seed, p.Progress)
}

// DaysToComplete answers "how many days to finish Items items?", forwarding
// p.Progress to RunSimulations. See ItemsInDays.
func DaysToComplete(pool *SamplePool, p Params) []int {
	return simulateDays(pool.Slots(p), p.Calendar, p.Items, p.Simulations, p.Workers, p.Seed, p.Progress)
}
