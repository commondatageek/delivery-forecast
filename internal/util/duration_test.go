package util

import (
	"testing"
	"time"
)

func TestParseFlexibleDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		// Standard Go durations pass through unchanged
		{"1h", time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"90s", 90 * time.Second, false},
		{"1h30m", 90 * time.Minute, false},

		// Day unit
		{"1d", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"0.5d", 12 * time.Hour, false},

		// Mixed day + standard units
		{"1d12h", 36 * time.Hour, false},
		{"2d30m", 2*24*time.Hour + 30*time.Minute, false},

		// Error cases
		{"", 0, true},
		{"bananas", 0, true},
		{"1x", 0, true},
		{"1d2x", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseFlexibleDuration(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseFlexibleDuration(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("ParseFlexibleDuration(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
