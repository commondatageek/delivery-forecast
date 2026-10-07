package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
