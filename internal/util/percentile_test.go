package util

import "testing"

func TestComputePercentile(t *testing.T) {
	tests := []struct {
		name   string
		sorted []float64
		v      float64
		want   int
	}{
		{"empty", nil, 5, 0},
		{"below min", []float64{10, 20, 30, 40}, 5, 0},
		{"at min", []float64{10, 20, 30, 40}, 10, 25},
		{"at max", []float64{10, 20, 30, 40}, 40, 100},
		{"above max", []float64{10, 20, 30, 40}, 100, 100},
		{"middle exact", []float64{10, 20, 30, 40}, 20, 50},
		{"between values", []float64{10, 20, 30, 40}, 25, 50},
		{"single element below", []float64{42}, 0, 0},
		{"single element equal", []float64{42}, 42, 100},
		// Ties count toward the rank: both 20s are <= v, so 3 of 5 = 60.
		{"ties", []float64{10, 20, 20, 30, 40}, 20, 60},
		{"rounding down", []float64{1, 2, 3}, 1, 33},
		{"rounding up", []float64{1, 2, 3}, 2, 67},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComputePercentile(tt.sorted, tt.v); got != tt.want {
				t.Errorf("ComputePercentile(%v, %v) = %d, want %d", tt.sorted, tt.v, got, tt.want)
			}
		})
	}
}

func TestPercentileValueFloat(t *testing.T) {
	tests := []struct {
		name   string
		sorted []float64
		p      float64
		want   float64
	}{
		{"empty", nil, 85, 0},
		{"single", []float64{42}, 85, 42},
		{"min", []float64{10, 20, 30, 40}, 0, 10},
		{"max", []float64{10, 20, 30, 40}, 100, 40},
		// idx = round(0.5 * 3) = round(1.5) = 2 (round half away from zero).
		{"median rounds up", []float64{10, 20, 30, 40}, 50, 30},
		// idx = round(0.85 * 3) = round(2.55) = 3.
		{"p85", []float64{10, 20, 30, 40}, 85, 40},
		// idx = round(0.85 * 9) = round(7.65) = 8 -> value 80.
		{"p85 ten elements", []float64{0, 10, 20, 30, 40, 50, 60, 70, 80, 90}, 85, 80},
		{"clamp below zero", []float64{10, 20, 30}, -5, 10},
		{"clamp above 100", []float64{10, 20, 30}, 150, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PercentileValue(tt.sorted, tt.p); got != tt.want {
				t.Errorf("PercentileValue(%v, %v) = %v, want %v", tt.sorted, tt.p, got, tt.want)
			}
		})
	}
}

func TestPercentileValueInt(t *testing.T) {
	sorted := make([]int, 100) // 0..99
	for i := range sorted {
		sorted[i] = i
	}
	cases := []struct {
		p    float64
		want int
	}{
		{0, 0},
		{50, 50}, // round(0.5 * 99) = round(49.5) = 50
		{85, 84}, // round(0.85 * 99) = round(84.15) = 84
		{100, 99},
	}
	for _, c := range cases {
		if got := PercentileValue(sorted, c.p); got != c.want {
			t.Errorf("PercentileValue(p=%v) = %d, want %d", c.p, got, c.want)
		}
	}
	if got := PercentileValue([]int(nil), 50); got != 0 {
		t.Errorf("PercentileValue(empty) = %d, want 0", got)
	}
}
