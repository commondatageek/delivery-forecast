package main

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/simulate"
)

func TestLoadExclusions_EmptyPathIsNone(t *testing.T) {
	exc, err := loadExclusions("")
	if err != nil {
		t.Fatalf("loadExclusions(\"\"): %v", err)
	}
	if !exc.IsEmpty() {
		t.Errorf("exclusions = %+v, want none", exc)
	}
}

func TestLoadExclusions_MissingFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	_, err := loadExclusions(path)
	if err == nil {
		t.Fatal("loadExclusions of a missing file = nil error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q should name the path", err)
	}
}

func TestLoadExclusions_ParsesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exclusions.json")
	body := `{"global": ["2025-12-25", {"from": "2026-01-01", "to": "2026-01-02", "reason": "New Year"}],
	          "engineers": {"alice": ["2026-03-02"]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	exc, err := loadExclusions(path)
	if err != nil {
		t.Fatalf("loadExclusions: %v", err)
	}
	if got := len(exc.Days("")); got != 3 {
		t.Errorf("global days = %d, want 3", got)
	}
	if got := len(exc.Days("alice")); got != 1 {
		t.Errorf("alice days = %d, want 1", got)
	}
}

func TestLoadExclusions_BadFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exclusions.json")
	if err := os.WriteFile(path, []byte(`{"global": ["2025-13-45"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadExclusions(path); err == nil {
		t.Fatal("loadExclusions of a malformed file = nil error")
	}
}

func TestUnmatchedExclusionNames(t *testing.T) {
	exc, err := simulate.ParseExclusions([]byte(`{
		"global": ["2025-12-25"],
		"engineers": {
			"alice": ["2026-01-02"],
			"bob": ["2026-01-02"],
			"carol": ["2026-01-02"],
			"zed": ["2026-01-02"],
			"yan": ["2026-01-02"],
			"empty": []
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	assignees := map[string]bool{"alice": true}
	got := unmatchedExclusionNames(exc, assignees, []string{"bob", "dave"})
	want := []string{"carol", "yan", "zed"} // sorted; alice (data) and bob (slot) match; "empty" has no entries
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unmatched = %v, want %v", got, want)
	}

	if got := unmatchedExclusionNames(simulate.Exclusions{}, assignees, nil); got != nil {
		t.Errorf("no exclusions: got %v, want nil", got)
	}
}

func TestResolveTargetWindow(t *testing.T) {
	now := time.Date(2025, 3, 1, 14, 30, 0, 0, time.Local)
	cases := []struct {
		name      string
		args      []string
		wantStart string
		wantEnd   string
		wantDays  int
		wantErr   string
	}{
		{"days only", []string{"-days", "10"}, "2025-03-02", "2025-03-11", 10, ""},
		{"days one", []string{"-days", "1"}, "2025-03-02", "2025-03-02", 1, ""},
		{"end only", []string{"-target-end-date", "2025-03-11"}, "2025-03-02", "2025-03-11", 10, ""},
		{"explicit start and end", []string{"-target-start-date", "2025-03-02", "-target-end-date", "2025-03-11"}, "2025-03-02", "2025-03-11", 10, ""},
		{"explicit start with days", []string{"-target-start-date", "2025-04-01", "-days", "30"}, "2025-04-01", "2025-04-30", 30, ""},
		{"both", []string{"-days", "5", "-target-end-date", "2025-03-11"}, "", "", 0, "mutually exclusive"},
		{"neither", nil, "", "", 0, "one of -days or -target-end-date must be provided"},
		{"non-positive days", []string{"-days", "0"}, "", "", 0, "-days must be positive"},
		{"end before start", []string{"-target-end-date", "2025-03-01"}, "", "", 0, "must be after"},
		{"end equals start", []string{"-target-start-date", "2025-03-05", "-target-end-date", "2025-03-05"}, "", "", 0, "must be after"},
		{"bad start", []string{"-target-start-date", "soon", "-days", "3"}, "", "", 0, "invalid -target-start-date"},
		{"bad end", []string{"-target-end-date", "later"}, "", "", 0, "invalid -target-end-date"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := flag.NewFlagSet("t", flag.ContinueOnError)
			days := fs.Int("days", 0, "")
			startStr := fs.String("target-start-date", "tomorrow", "")
			endStr := fs.String("target-end-date", "", "")
			if err := fs.Parse(c.args); err != nil {
				t.Fatal(err)
			}
			start, end, n, err := resolveTargetWindow(fs, *days, *startStr, *endStr, now)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveTargetWindow: %v", err)
			}
			if got := start.Format("2006-01-02"); got != c.wantStart {
				t.Errorf("start = %s, want %s", got, c.wantStart)
			}
			if got := end.Format("2006-01-02"); got != c.wantEnd {
				t.Errorf("end = %s, want %s", got, c.wantEnd)
			}
			if n != c.wantDays {
				t.Errorf("effectiveDays = %d, want %d", n, c.wantDays)
			}
		})
	}
}

func TestDescribeWindow(t *testing.T) {
	start := time.Date(2026, 10, 8, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, 11, 6, 0, 0, 0, 0, time.Local)
	if got, want := describeWindow(start, end, 30), "2026-10-08 to 2026-11-06 (30 days)"; got != want {
		t.Errorf("describeWindow = %q, want %q", got, want)
	}
}
