package app

import (
	"errors"
	"reflect"
	"testing"
)

func TestSimDays_SingleDates(t *testing.T) {
	res, err := SimDays(DaysRequest{SimBase: constantBase(), TargetStart: horizonStart, Items: []int{3}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Groups != nil || res.Total != nil || len(res.Single) != 4 {
		t.Fatalf("want Single only, got %d single rows, groups=%v total=%v", len(res.Single), res.Groups, res.Total)
	}
	for _, r := range res.Single {
		if r.Days != 3 {
			t.Errorf("confidence %d: days = %d, want 3", r.Confidence, r.Days)
		}
		if got := r.Date.Format("2006-01-02"); got != "2025-03-05" {
			t.Errorf("confidence %d: date = %s, want 2025-03-05", r.Confidence, got)
		}
	}
}

func TestSimDays_TrajectoryInvariants(t *testing.T) {
	base := constantBase()
	base.Issues = append(base.Issues,
		// A noisy second engineer so the confidence levels can differ.
		constantIssues()[0], constantIssues()[0],
	)
	res, err := SimDays(DaysRequest{
		SimBase: base, TargetStart: horizonStart, Items: []int{4, 3, 5}, Confidences: []int{50, 95},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Single != nil || len(res.Groups) != 3 || res.Total == nil {
		t.Fatalf("want Groups+Total only, got single=%v groups=%d total=%v", res.Single, len(res.Groups), res.Total)
	}
	if res.Total.Items != 12 {
		t.Errorf("Total.Items = %d, want 12", res.Total.Items)
	}
	for pi, c := range []int{50, 95} {
		sum := 0
		for g, group := range res.Groups {
			cell := group.Cells[pi]
			if cell.Confidence != c || cell.MarginalDays < 0 {
				t.Errorf("group %d confidence %d: %+v", g, c, cell)
			}
			sum += cell.MarginalDays
			if want := horizonStart.AddDate(0, 0, cell.CumulativeDays); !cell.Date.Equal(want) {
				t.Errorf("group %d confidence %d: date %v, want %v", g, c, cell.Date, want)
			}
		}
		total := res.Total.Cells[pi]
		if sum != total.CumulativeDays || total.MarginalDays != total.CumulativeDays {
			t.Errorf("confidence %d: marginals sum to %d, total cell %+v", c, sum, total)
		}
	}
}

func TestSimDays_ConfidenceRangeChecked(t *testing.T) {
	for _, bad := range []int{0, 101, -5} {
		_, err := SimDays(DaysRequest{SimBase: constantBase(), TargetStart: horizonStart, Items: []int{3}, Confidences: []int{50, bad}})
		var fe *FieldError
		if !errors.As(err, &fe) || !reflect.DeepEqual(fe.Fields, []string{"confidence"}) {
			t.Errorf("confidence %d: err = %v, want a FieldError on confidence", bad, err)
		}
	}
}

func TestSimDays_ValidateItems(t *testing.T) {
	cases := []struct {
		items []int
		want  string
	}{
		{nil, "-items is required"},
		{[]int{3, 0}, "-items: group sizes must be positive, got 0"},
	}
	for _, c := range cases {
		err := DaysRequest{SimBase: constantBase(), TargetStart: horizonStart, Items: c.items}.Validate()
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Msg != c.want || !reflect.DeepEqual(fe.Fields, []string{"items"}) {
			t.Errorf("items %v: err = %v, want FieldError{items, %q}", c.items, err, c.want)
		}
	}
}
