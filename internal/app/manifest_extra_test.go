package app

import (
	"reflect"
	"testing"
)

func TestManifestExtra(t *testing.T) {
	start, end := localDay(2026, 3, 2), localDay(2026, 3, 31)

	items := (&ItemsResult{TargetStart: start, TargetEnd: end, EffectiveDays: 30, Confidences: []int{50, 85}}).ManifestExtra()
	wantItems := map[string]any{
		"effective_confidence_levels": []int{50, 85},
		"target_start_date":           "2026-03-02",
		"target_end_date":             "2026-03-31",
		"effective_days":              30,
	}
	if !reflect.DeepEqual(items, wantItems) {
		t.Errorf("items extra = %v, want %v", items, wantItems)
	}

	days := (&DaysResult{TargetStart: start, Confidences: []int{75}}).ManifestExtra()
	wantDays := map[string]any{
		"effective_confidence_levels": []int{75},
		"target_start_date":           "2026-03-02",
	}
	if !reflect.DeepEqual(days, wantDays) {
		t.Errorf("days extra = %v, want %v", days, wantDays)
	}

	prob := (&ProbabilityResult{TargetStart: start, TargetEnd: end, EffectiveDays: 30}).ManifestExtra()
	wantProb := map[string]any{
		"target_start_date": "2026-03-02",
		"target_end_date":   "2026-03-31",
		"effective_days":    30,
	}
	if !reflect.DeepEqual(prob, wantProb) {
		t.Errorf("probability extra = %v, want %v", prob, wantProb)
	}
}
