package aging

import (
	"bytes"
	"math"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

func mustTime(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t.UTC()
}

func TestCycleTimesFiltering(t *testing.T) {
	issues := []Issue{
		{StartedAt: mustTime("2024-01-01"), CompletedAt: mustTime("2024-01-11")}, // 10 days
		{StartedAt: mustTime("2024-01-01"), CompletedAt: mustTime("2024-01-02")}, // 1 day
		{StartedAt: mustTime("2024-01-01"), CompletedAt: mustTime("2024-01-06")}, // 5 days
		{CompletedAt: mustTime("2024-01-10")},                                    // no StartedAt → skip
		{StartedAt: mustTime("2024-01-01")},                                      // no CompletedAt → skip
	}

	// No min cycle time: all three valid issues pass.
	got := CycleTimes(issues, 0)
	if len(got) != 3 {
		t.Errorf("no min: got %d cycle times, want 3", len(got))
	}

	// Min 3 days: 1-day issue filtered out.
	got = CycleTimes(issues, 3*24*time.Hour)
	if len(got) != 2 {
		t.Errorf("min 3d: got %d cycle times, want 2", len(got))
	}

	// Min 6 days: only the 10-day issue passes.
	got = CycleTimes(issues, 6*24*time.Hour)
	if len(got) != 1 {
		t.Fatalf("min 6d: got %d cycle times, want 1", len(got))
	}
	if got[0] != 10.0 {
		t.Errorf("expected 10.0 days, got %v", got[0])
	}
}

func TestInProgressItemsAgeDays(t *testing.T) {
	today := mustTime("2024-03-01")
	issues := []Issue{
		{Identifier: "ENG-1", StartedAt: mustTime("2024-02-20")}, // 10 days ago
		{Identifier: "ENG-2", StartedAt: mustTime("2024-02-29")}, // 1 day ago
		{Identifier: "ENG-3"},                                     // no StartedAt → skip
	}

	items := InProgressItems(issues, today)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].AgeDays != 10.0 {
		t.Errorf("ENG-1 AgeDays: got %v, want 10.0", items[0].AgeDays)
	}
	if items[1].AgeDays != 1.0 {
		t.Errorf("ENG-2 AgeDays: got %v, want 1.0", items[1].AgeDays)
	}
}

func TestCompletedItemsFiltering(t *testing.T) {
	issues := []Issue{
		{Identifier: "ENG-1", StartedAt: mustTime("2024-01-01"), CompletedAt: mustTime("2024-01-11")}, // 10 days
		{Identifier: "ENG-2", StartedAt: mustTime("2024-01-01"), CompletedAt: mustTime("2024-01-02")}, // 1 day
		{Identifier: "ENG-3", CompletedAt: mustTime("2024-01-10")},                                    // no StartedAt → skip
		{Identifier: "ENG-4", StartedAt: mustTime("2024-01-01")},                                      // no CompletedAt → skip
	}

	items := CompletedItems(issues, 0)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Identifier != "ENG-1" || items[0].AgeDays != 10.0 {
		t.Errorf("ENG-1: got %+v", items[0])
	}
	if items[1].Identifier != "ENG-2" || items[1].AgeDays != 1.0 {
		t.Errorf("ENG-2: got %+v", items[1])
	}

	// Min 3 days: 1-day issue filtered out.
	items = CompletedItems(issues, 3*24*time.Hour)
	if len(items) != 1 {
		t.Fatalf("min 3d: got %d items, want 1", len(items))
	}
	if items[0].Identifier != "ENG-1" {
		t.Errorf("min 3d: got %+v, want ENG-1", items[0])
	}
}

func TestRankItems(t *testing.T) {
	cycleTimes := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	sort.Float64s(cycleTimes)

	items := []Item{
		{AgeDays: 5.0},  // 50th percentile
		{AgeDays: 10.0}, // 100th percentile
		{AgeDays: 0.5},  // 0th percentile (below all)
	}

	// util.PercentileValue(cycleTimes, 85) = sorted[round(0.85*9)] = sorted[8] = 9.0
	const threshold = 9.0
	RankItems(items, cycleTimes, threshold)

	if items[0].Percentile != 50 {
		t.Errorf("5.0 days: got %d%%, want 50%%", items[0].Percentile)
	}
	if items[1].Percentile != 100 {
		t.Errorf("10.0 days: got %d%%, want 100%%", items[1].Percentile)
	}
	if items[2].Percentile != 0 {
		t.Errorf("0.5 days: got %d%%, want 0%%", items[2].Percentile)
	}

	wantMult := []float64{5.0 / 9.0, 10.0 / 9.0, 0.5 / 9.0}
	for i, want := range wantMult {
		if math.Abs(items[i].Multiplier-want) > 1e-9 {
			t.Errorf("items[%d].Multiplier: got %v, want %v", i, items[i].Multiplier, want)
		}
	}
}

func TestRankItemsZeroThreshold(t *testing.T) {
	cycleTimes := []float64{1, 2, 3}
	items := []Item{{AgeDays: 5.0}, {AgeDays: 0.0}}

	RankItems(items, cycleTimes, 0)

	for i, item := range items {
		if item.Multiplier != 0 {
			t.Errorf("items[%d].Multiplier: got %v, want 0", i, item.Multiplier)
		}
		if math.IsNaN(item.Multiplier) || math.IsInf(item.Multiplier, 0) {
			t.Errorf("items[%d].Multiplier: got %v, want a finite non-NaN value", i, item.Multiplier)
		}
	}
}

func TestAgeClassFromMultiplier(t *testing.T) {
	tests := []struct {
		mult         float64
		hasThreshold bool
		want         string
	}{
		{1.00, true, "high"},
		{1.01, true, "high"},
		{0.99, true, "medium"},
		{0.85, true, "medium"},
		{0.84, true, "normal"},
		{0.0, true, "normal"},
		{1.50, false, "normal"},
	}
	for _, tt := range tests {
		got := ageClass(tt.mult, tt.hasThreshold)
		if got != tt.want {
			t.Errorf("ageClass(%v, %v) = %q, want %q", tt.mult, tt.hasThreshold, got, tt.want)
		}
	}
}

func TestRenderTextMultiplierColumn(t *testing.T) {
	item := Item{Identifier: "ENG-1", AgeDays: 6.0, Multiplier: 1.5}

	var buf bytes.Buffer
	meta := Meta{Percentile: 90, Threshold: 4}
	if err := RenderText(&buf, []Item{item}, nil, false, meta); err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "MULTIPLIER") {
		t.Errorf("expected header to contain MULTIPLIER, got:\n%s", out)
	}
	if !strings.Contains(out, "1.50x") {
		t.Errorf("expected row to contain 1.50x, got:\n%s", out)
	}
	if !strings.Contains(out, "P90: 4.0 days") {
		t.Errorf("expected summary line to contain P90: 4.0 days, got:\n%s", out)
	}

	buf.Reset()
	zeroMeta := Meta{Percentile: 90, Threshold: 0}
	if err := RenderText(&buf, []Item{item}, nil, false, zeroMeta); err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "—") {
		t.Errorf("expected degenerate output to contain an em dash, got:\n%s", out)
	}
	if strings.Contains(out, "1.50x") {
		t.Errorf("expected degenerate output to have no multiplier value, got:\n%s", out)
	}
}

// TestRenderTextEmojiAlignment guards against a wide rune (e.g. an emoji) in
// a title throwing off the columns that follow it: tabwriter, which
// RenderText used to rely on, pads by rune count, and an emoji is one rune
// but two terminal cells wide, so a title's actual on-screen width used to
// silently drift from what tabwriter assumed.
func TestRenderTextEmojiAlignment(t *testing.T) {
	items := []Item{
		{Identifier: "ENG-1", Title: "🚀🎉 Ship the launch", AgeDays: 9.0},
		{Identifier: "ENG-2", Title: "Plain ascii title", AgeDays: 9.0},
	}
	meta := Meta{Percentile: 85, Threshold: 0}

	var buf bytes.Buffer
	if err := RenderText(&buf, items, nil, false, meta); err != nil {
		t.Fatalf("RenderText: %v", err)
	}

	var emojiLine, plainLine string
	for _, l := range strings.Split(buf.String(), "\n") {
		switch {
		case strings.Contains(l, "ENG-1"):
			emojiLine = l
		case strings.Contains(l, "ENG-2"):
			plainLine = l
		}
	}
	if emojiLine == "" || plainLine == "" {
		t.Fatalf("expected both rows in output, got:\n%s", buf.String())
	}

	idx1 := strings.Index(emojiLine, "9.0")
	idx2 := strings.Index(plainLine, "9.0")
	if idx1 < 0 || idx2 < 0 {
		t.Fatalf("expected both rows to contain the DAYS value, got:\n%s\n%s", emojiLine, plainLine)
	}
	w1 := runewidth.StringWidth(emojiLine[:idx1])
	w2 := runewidth.StringWidth(plainLine[:idx2])
	if w1 != w2 {
		t.Errorf("DAYS column starts at different display widths (emoji row %d, plain row %d):\n%s\n%s", w1, w2, emojiLine, plainLine)
	}
}

func TestTruncateTitleValidUTF8(t *testing.T) {
	// Multi-byte runes positioned right where the old byte-slicing
	// truncation used to cut through mid-rune.
	title := strings.Repeat("é", 60)
	got := truncateTitle(title)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateTitle produced invalid UTF-8: %q", got)
	}
	if w := runewidth.StringWidth(got); w > maxTitleWidth {
		t.Errorf("truncateTitle width = %d, want <= %d", w, maxTitleWidth)
	}
}

func TestTruncateTitleWideRunes(t *testing.T) {
	// 40 emoji is only 40 runes but 80 display columns — well over budget
	// even though rune count alone wouldn't say so.
	title := strings.Repeat("🚀", 40)
	got := truncateTitle(title)
	if w := runewidth.StringWidth(got); w > maxTitleWidth {
		t.Errorf("truncateTitle width = %d, want <= %d", w, maxTitleWidth)
	}
}
