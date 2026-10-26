package app

import (
	"time"

	"github.com/commondatageek/delivery-forecast/simulate"
)

// ProbabilityRequest asks "what is the probability of completing I items in
// D days?".
type ProbabilityRequest struct {
	SimBase
	TargetStart time.Time
	Days        int
	TargetEnd   time.Time
	Items       int // < 0 = report the full table instead of one probability
}

// ProbabilityRow is one line of the full probability table.
type ProbabilityRow struct {
	Items       int
	Probability float64 // percent, 0–100
}

// ProbabilityResult is the outcome of SimProbability.
type ProbabilityResult struct {
	Warnings      []string
	Run           SimRun
	TargetStart   time.Time
	TargetEnd     time.Time
	EffectiveDays int
	Items         int              // echoes the request
	Probability   float64          // set when Items >= 0
	Table         []ProbabilityRow // set when Items < 0: n = 1, 2, ... up to and including the first row with Probability == 0
	Distribution  []int
}

// Validate checks everything about the request that doesn't need Issues.
func (r ProbabilityRequest) Validate() error {
	if _, err := r.mode(); err != nil {
		return err
	}
	if err := r.validateWindow(); err != nil {
		return err
	}
	_, _, err := resolveTargetWindow(r.TargetStart, r.Days, r.TargetEnd)
	return err
}

// SimProbability runs the Monte Carlo simulation for "probability of
// completing I items in D days", or the full table when Items < 0.
func SimProbability(req ProbabilityRequest) (*ProbabilityResult, error) {
	res := &ProbabilityResult{Items: req.Items}
	if err := req.Validate(); err != nil {
		return res, err
	}
	mode, err := req.mode()
	if err != nil {
		return res, err
	}
	targetEnd, effectiveDays, err := resolveTargetWindow(req.TargetStart, req.Days, req.TargetEnd)
	if err != nil {
		return res, err
	}
	res.TargetStart, res.TargetEnd, res.EffectiveDays = req.TargetStart, targetEnd, effectiveDays

	run, warns, err := preparePool(req.SimBase, mode, false)
	res.Warnings, res.Run = warns, run
	if err != nil {
		return res, err
	}
	run.HorizonCalendar = simulate.NewCalendar(req.Exclusions, req.TargetStart)
	res.Run = run // res.Run is a copy taken above; refresh it so the manifest sees the horizon

	dist := simulate.ItemsInDays(run.Pool, simulate.Params{
		Mode:          mode,
		Engineers:     req.Engineers,
		EngineerNames: req.EngineerNames,
		Calendar:      run.HorizonCalendar,
		Days:          effectiveDays,
		Simulations:   req.Simulations,
		Workers:       req.Workers,
		Seed:          req.Seed,
		Progress:      req.Progress,
	})
	if req.Items >= 0 {
		res.Probability = simulate.ProbabilityAtLeast(dist, req.Items)
	} else {
		for n := 1; ; n++ {
			p := simulate.ProbabilityAtLeast(dist, n)
			res.Table = append(res.Table, ProbabilityRow{Items: n, Probability: p})
			if p == 0 {
				break
			}
		}
	}
	res.Distribution = dist
	return res, nil
}

// ManifestExtra returns the sim probability-specific manifest fields.
func (r *ProbabilityResult) ManifestExtra() map[string]any {
	return map[string]any{
		"target_start_date": r.TargetStart.Format("2006-01-02"),
		"target_end_date":   r.TargetEnd.Format("2006-01-02"),
		"effective_days":    r.EffectiveDays,
	}
}
