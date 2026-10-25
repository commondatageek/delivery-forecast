package app

import (
	"fmt"
	"time"

	"github.com/commondatageek/delivery-forecast/simulate"
)

// ItemsRequest asks "how many items can N engineers complete in D days?".
type ItemsRequest struct {
	SimBase
	TargetStart time.Time
	Days        int // exactly one of Days / TargetEnd
	TargetEnd   time.Time
	Confidences []int // empty = DefaultConfidences()
}

// ItemsRow is one confidence level's answer.
type ItemsRow struct {
	Confidence int // e.g. 85
	Items      int // simulate.ItemsAtConfidence(dist, 85)
}

// ItemsResult is the outcome of SimItems.
type ItemsResult struct {
	Warnings      []string
	Run           SimRun
	TargetStart   time.Time
	TargetEnd     time.Time // inclusive, resolved
	EffectiveDays int
	Confidences   []int // effective (defaults applied)
	Rows          []ItemsRow
	Distribution  []int // sorted simulate.ItemsInDays output
}

// validateConfidences checks every confidence is in (0, 100].
func validateConfidences(confidences []int) error {
	for _, c := range confidences {
		if c <= 0 || c > 100 {
			return &FieldError{Fields: []string{"confidence"}, Msg: fmt.Sprintf("-confidence: values must be in (0, 100], got %d", c)}
		}
	}
	return nil
}

// Validate checks everything about the request that doesn't need Issues.
func (r ItemsRequest) Validate() error {
	if _, err := r.mode(); err != nil {
		return err
	}
	if err := r.validateWindow(); err != nil {
		return err
	}
	if _, _, err := resolveTargetWindow(r.TargetStart, r.Days, r.TargetEnd); err != nil {
		return err
	}
	return validateConfidences(r.Confidences)
}

// SimItems runs the Monte Carlo simulation for "how many items in D days".
func SimItems(req ItemsRequest) (*ItemsResult, error) {
	res := &ItemsResult{}
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
	confidences := req.Confidences
	if len(confidences) == 0 {
		confidences = DefaultConfidences()
	}
	res.TargetStart, res.TargetEnd, res.EffectiveDays, res.Confidences = req.TargetStart, targetEnd, effectiveDays, confidences

	run, warns, err := preparePool(req.SimBase, mode, false)
	res.Warnings, res.Run = warns, run
	if err != nil {
		return res, err
	}
	run.HorizonCalendar = simulate.NewCalendar(req.Exclusions, req.TargetStart)

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
	for _, c := range confidences {
		res.Rows = append(res.Rows, ItemsRow{Confidence: c, Items: simulate.ItemsAtConfidence(dist, float64(c))})
	}
	res.Distribution = dist
	return res, nil
}

// ManifestExtra returns the sim items-specific manifest fields.
func (r *ItemsResult) ManifestExtra() map[string]any {
	return map[string]any{
		"effective_confidence_levels": r.Confidences,
		"target_start_date":           r.TargetStart.Format("2006-01-02"),
		"target_end_date":             r.TargetEnd.Format("2006-01-02"),
		"effective_days":              r.EffectiveDays,
	}
}
