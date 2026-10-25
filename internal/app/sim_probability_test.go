package app

import (
	"testing"

	"github.com/commondatageek/delivery-forecast/simulate"
)

func TestSimProbability_TableEndsAtFirstZero(t *testing.T) {
	res, err := SimProbability(ProbabilityRequest{SimBase: constantBase(), TargetStart: horizonStart, Days: 3, Items: -1})
	if err != nil {
		t.Fatal(err)
	}
	// Constant 1/day for 3 days: 1..3 items are certain, 4 is impossible.
	if len(res.Table) != 4 {
		t.Fatalf("table = %+v, want 4 rows", res.Table)
	}
	for i, row := range res.Table {
		wantProb := 100.0
		if i == 3 {
			wantProb = 0
		}
		if row.Items != i+1 || row.Probability != wantProb {
			t.Errorf("row %d = %+v, want items=%d probability=%v", i, row, i+1, wantProb)
		}
	}
}

func TestSimProbability_SingleMatchesProbabilityAtLeast(t *testing.T) {
	res, err := SimProbability(ProbabilityRequest{SimBase: constantBase(), TargetStart: horizonStart, Days: 3, Items: 2})
	if err != nil {
		t.Fatal(err)
	}
	if want := simulate.ProbabilityAtLeast(res.Distribution, 2); res.Probability != want {
		t.Errorf("Probability = %v, want %v", res.Probability, want)
	}
	if res.Table != nil || res.Items != 2 {
		t.Errorf("single mode should leave Table nil and echo Items, got %+v", res)
	}
}
