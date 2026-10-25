package app

import (
	"fmt"
	"time"

	"github.com/commondatageek/delivery-forecast/simulate"
)

// DaysRequest asks "how many days for N engineers to complete I items?".
type DaysRequest struct {
	SimBase
	TargetStart time.Time
	Items       []int // one value = single report; several = grouped trajectory
	Confidences []int // empty = DefaultConfidences()
}

// DaysRow is one confidence level's answer for a single item count.
type DaysRow struct {
	Confidence int
	Days       int       // simulate.PercentileValue(dist, c)
	Date       time.Time // TargetStart + Days
}

// TrajectoryCell is one group's answer at one confidence level.
type TrajectoryCell struct {
	Confidence     int
	MarginalDays   int
	CumulativeDays int
	Date           time.Time // TargetStart + CumulativeDays
}

// TrajectoryGroup is one row of the grouped trajectory report.
type TrajectoryGroup struct {
	Items int              // this group's own size (not cumulative)
	Cells []TrajectoryCell // one per confidence, in order
}

// TrajectoryTotal is the Total row of the grouped trajectory report.
type TrajectoryTotal struct {
	Items int              // sum of all groups
	Cells []TrajectoryCell // MarginalDays == CumulativeDays == totals[pi]
}

// DaysResult is the outcome of SimDays.
type DaysResult struct {
	Warnings    []string
	Run         SimRun
	TargetStart time.Time
	Confidences []int
	// Exactly one of Single / Groups is set, depending on len(Items).
	Single       []DaysRow
	Distribution []int // single mode only
	Groups       []TrajectoryGroup
	Total        *TrajectoryTotal
}

// Validate checks everything about the request that doesn't need Issues.
func (r DaysRequest) Validate() error {
	if _, err := r.mode(); err != nil {
		return err
	}
	if err := r.validateWindow(); err != nil {
		return err
	}
	if r.TargetStart.IsZero() {
		return &FieldError{Fields: []string{"target-start-date"}, Msg: "target start date must be set"}
	}
	if len(r.Items) == 0 {
		return &FieldError{Fields: []string{"items"}, Msg: "-items is required"}
	}
	for _, n := range r.Items {
		if n <= 0 {
			return &FieldError{Fields: []string{"items"}, Msg: fmt.Sprintf("-items: group sizes must be positive, got %d", n)}
		}
	}
	return validateConfidences(r.Confidences)
}

// SimDays runs the Monte Carlo simulation for "how many days to finish I
// items", or the grouped trajectory when several item counts are given.
func SimDays(req DaysRequest) (*DaysResult, error) {
	res := &DaysResult{}
	if err := req.Validate(); err != nil {
		return res, err
	}
	mode, err := req.mode()
	if err != nil {
		return res, err
	}
	confidences := req.Confidences
	if len(confidences) == 0 {
		confidences = DefaultConfidences()
	}
	res.TargetStart, res.Confidences = req.TargetStart, confidences

	run, warns, err := preparePool(req.SimBase, mode, true)
	res.Warnings, res.Run = warns, run
	if err != nil {
		return res, err
	}
	run.HorizonCalendar = simulate.NewCalendar(req.Exclusions, req.TargetStart)

	params := simulate.Params{
		Mode:          mode,
		Engineers:     req.Engineers,
		EngineerNames: req.EngineerNames,
		Calendar:      run.HorizonCalendar,
		Simulations:   req.Simulations,
		Workers:       req.Workers,
		Seed:          req.Seed,
	}

	if len(req.Items) > 1 {
		cum := make([]int, len(req.Items))
		total := 0
		for g, n := range req.Items {
			total += n
			cum[g] = total
		}

		dists := make([][]int, len(req.Items))
		for g, threshold := range cum {
			p := params
			p.Items = threshold
			dists[g] = simulate.DaysToComplete(run.Pool, p)
		}
		cells, totals := simulate.ComputeTrajectoryTable(dists, confidences)

		for g, n := range req.Items {
			group := TrajectoryGroup{Items: n}
			for pi, c := range confidences {
				cell := cells[g][pi]
				group.Cells = append(group.Cells, TrajectoryCell{
					Confidence:     c,
					MarginalDays:   cell.MarginalDays,
					CumulativeDays: cell.CumulativeDays,
					Date:           req.TargetStart.AddDate(0, 0, cell.CumulativeDays),
				})
			}
			res.Groups = append(res.Groups, group)
		}
		tot := &TrajectoryTotal{Items: total}
		for pi, c := range confidences {
			days := totals[pi]
			tot.Cells = append(tot.Cells, TrajectoryCell{
				Confidence:     c,
				MarginalDays:   days,
				CumulativeDays: days,
				Date:           req.TargetStart.AddDate(0, 0, days),
			})
		}
		res.Total = tot
		return res, nil
	}

	params.Items = req.Items[0]
	params.Progress = req.Progress
	dist := simulate.DaysToComplete(run.Pool, params)
	for _, c := range confidences {
		days := simulate.PercentileValue(dist, float64(c))
		res.Single = append(res.Single, DaysRow{Confidence: c, Days: days, Date: req.TargetStart.AddDate(0, 0, days)})
	}
	res.Distribution = dist
	return res, nil
}

// ManifestExtra returns the sim days-specific manifest fields.
func (r *DaysResult) ManifestExtra() map[string]any {
	return map[string]any{
		"effective_confidence_levels": r.Confidences,
		"target_start_date":           r.TargetStart.Format("2006-01-02"),
	}
}
