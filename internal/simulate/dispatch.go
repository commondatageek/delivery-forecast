package simulate

import "math/rand"

// Params bundles the resolved simulation parameters for the mode-aware
// dispatch functions. Days is used by ItemsInDays; Items by DaysToComplete.
// Progress is optional (nil = no progress reporting).
type Params struct {
	Mode        Mode
	Team        []string
	Engineers   int
	Days        int
	Items       int
	Simulations int
	Workers     int
	Seed        int64
	Progress    func(done, total int)
}

// ItemsInDays answers "how many items in Days days?" dispatching to the
// appropriate engine based on p.Mode and forwarding p.Progress to RunSimulations.
//
// The trial logic is inlined here (rather than delegating to SimulateItemsInDays)
// so that p.Progress reaches RunSimulations on every call.
func ItemsInDays(pool *SamplePool, p Params) []int {
	switch p.Mode {
	case ModeNamedTeam:
		return RunSimulations(p.Simulations, p.Workers, p.Seed, func(rng *rand.Rand) int {
			total := 0
			for _, eng := range p.Team {
				for range p.Days {
					total += pool.DrawFromEngineer(eng, rng)
				}
			}
			return total
		}, p.Progress)
	case ModeFullTeam:
		samples := pool.PerEngineer[WholeTeamKey]
		return RunSimulations(p.Simulations, p.Workers, p.Seed, func(rng *rand.Rand) int {
			total := 0
			for range p.Days {
				total += samples[rng.Intn(len(samples))]
			}
			return total
		}, p.Progress)
	default: // ModeAnonymous
		samples := pool.GetCombinedSamples()
		engineers := p.Engineers
		return RunSimulations(p.Simulations, p.Workers, p.Seed, func(rng *rand.Rand) int {
			total := 0
			for range engineers {
				for range p.Days {
					total += samples[rng.Intn(len(samples))]
				}
			}
			return total
		}, p.Progress)
	}
}

// DaysToComplete answers "how many days to finish Items items?" dispatching to
// the appropriate engine based on p.Mode and forwarding p.Progress to RunSimulations.
//
// The trial logic is inlined here (rather than delegating to SimulateDaysToComplete)
// so that p.Progress reaches RunSimulations on every call.
func DaysToComplete(pool *SamplePool, p Params) []int {
	switch p.Mode {
	case ModeNamedTeam:
		return RunSimulations(p.Simulations, p.Workers, p.Seed, func(rng *rand.Rand) int {
			completed := 0
			days := 0
			for completed < p.Items {
				days++
				for _, eng := range p.Team {
					completed += pool.DrawFromEngineer(eng, rng)
				}
			}
			return days
		}, p.Progress)
	case ModeFullTeam:
		samples := pool.PerEngineer[WholeTeamKey]
		return RunSimulations(p.Simulations, p.Workers, p.Seed, func(rng *rand.Rand) int {
			completed := 0
			days := 0
			for completed < p.Items {
				days++
				completed += samples[rng.Intn(len(samples))]
			}
			return days
		}, p.Progress)
	default: // ModeAnonymous
		samples := pool.GetCombinedSamples()
		engineers := p.Engineers
		items := p.Items
		return RunSimulations(p.Simulations, p.Workers, p.Seed, func(rng *rand.Rand) int {
			completed := 0
			days := 0
			for completed < items {
				days++
				for range engineers {
					completed += samples[rng.Intn(len(samples))]
				}
			}
			return days
		}, p.Progress)
	}
}
