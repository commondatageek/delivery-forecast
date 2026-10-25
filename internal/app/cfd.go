package app

import (
	"fmt"

	"github.com/commondatageek/delivery-forecast/cfd"
	"github.com/commondatageek/delivery-forecast/issues"
)

// CFDRequest asks for the cumulative flow diagram data.
type CFDRequest struct {
	Issues      []issues.Issue
	cfd.Options // Teams, Start, End
}

// CFDResult is the outcome of RunCFD.
type CFDResult struct {
	Warnings []string
	Rows     []cfd.DayRow
	Health   cfd.FlowHealth // TotalIssues and SkippedIssues already set
}

// Validate checks everything about the request that doesn't need Issues.
func (r CFDRequest) Validate() error {
	if !r.Start.Before(r.End) {
		return &FieldError{Fields: []string{"start", "end"}, Msg: "-start must be before -end"}
	}
	return nil
}

// RunCFD builds the CFD grid and flow-health stats for the filtered issues.
func RunCFD(req CFDRequest) (*CFDResult, error) {
	res := &CFDResult{}
	if err := req.Validate(); err != nil {
		return res, err
	}
	opts := req.Options

	if msg := BlendingTeamsWarning(opts.Teams, DistinctTeamKeys(req.Issues)); msg != "" {
		res.Warnings = append(res.Warnings, msg)
	}

	filtered := issues.Filter{Teams: opts.Teams}.Apply(req.Issues)
	if len(filtered) == 0 {
		res.Warnings = append(res.Warnings, "no issues found for the given team filter")
	}

	var normalized []cfd.NormalizedIssue
	skipped := 0
	for _, r := range toCFDIssues(filtered) {
		ni, ok := cfd.Normalize(r)
		if !ok {
			skipped++
			continue
		}
		normalized = append(normalized, ni)
	}

	rows := cfd.BuildGrid(normalized, opts.Start, opts.End)

	if err := cfd.AssertInvariants(rows); err != nil {
		return res, fmt.Errorf("CFD invariant violated: %w", err)
	}

	health := cfd.ComputeHealth(rows, normalized, opts.Start, opts.End)
	health.TotalIssues = len(filtered)
	health.SkippedIssues = skipped

	res.Rows, res.Health = rows, health
	return res, nil
}
