package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/aging"
)

// Zero-valued dates are never meaningful to a caller that builds a request by
// hand (a web adapter); each Validate must name them instead of simulating
// against year 1.
func TestValidate_UnsetDates(t *testing.T) {
	end := localDay(2025, 3, 10)
	unsetSample := constantBase()
	unsetSample.SampleStart = time.Time{}

	cases := []struct {
		name       string
		err        error
		wantFields []string
	}{
		{"items sample", ItemsRequest{SimBase: unsetSample, TargetStart: horizonStart, Days: 5}.Validate(), []string{"sample-start", "sample-end"}},
		{"items target start", ItemsRequest{SimBase: constantBase(), Days: 5}.Validate(), []string{"target-start-date"}},
		{"probability target start", ProbabilityRequest{SimBase: constantBase(), TargetEnd: end}.Validate(), []string{"target-start-date"}},
		{"days target start", DaysRequest{SimBase: constantBase(), Items: []int{5}}.Validate(), []string{"target-start-date"}},
		{"backtest today", BacktestRequest{SimBase: constantBase(), Project: "p", TargetEnd: end}.Validate(), []string{"today"}},
		{"aging today", AgingRequest{Options: aging.Options{Percentile: 85, SampleStart: localDay(2025, 1, 1), SampleEnd: end}}.Validate(), []string{"today"}},
	}
	for _, c := range cases {
		var fe *FieldError
		if !errors.As(c.err, &fe) || !slices.Equal(fe.Fields, c.wantFields) {
			t.Errorf("%s: got %v, want FieldError on %v", c.name, c.err, c.wantFields)
		}
	}
}
