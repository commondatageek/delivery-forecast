package util

import (
	"math"
	"sort"
)

// ComputePercentile returns what percentile v falls at in a slice sorted in
// ascending order. The result is the cumulative percentile rank: the percentage
// of values in sorted that are less than or equal to v, rounded to the nearest
// integer (0–100). An empty slice returns 0.
func ComputePercentile(sorted []float64, v float64) int {
	if len(sorted) == 0 {
		return 0
	}
	rank := sort.Search(len(sorted), func(i int) bool { return sorted[i] > v })
	return int(math.Round(float64(rank) / float64(len(sorted)) * 100))
}

// PercentileValue returns the value at percentile p (0–100) from a slice sorted
// in ascending order. It is the inverse of [ComputePercentile]: it uses the
// nearest-rank method, mapping p to index round(p/100·(len-1)). p is clamped to
// [0, 100]. An empty slice returns the zero value of T.
func PercentileValue[T any](sorted []T, p float64) T {
	if len(sorted) == 0 {
		var zero T
		return zero
	}
	switch {
	case p < 0:
		p = 0
	case p > 100:
		p = 100
	}
	idx := int(math.Round(p / 100 * float64(len(sorted)-1)))
	return sorted[idx]
}
