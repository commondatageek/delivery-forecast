package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/simulate"
)

func TestLoadExclusions_EmptyPathIsNone(t *testing.T) {
	exc, err := LoadExclusions("")
	if err != nil {
		t.Fatalf("LoadExclusions(\"\"): %v", err)
	}
	if !exc.IsEmpty() {
		t.Errorf("exclusions = %+v, want none", exc)
	}
}

func TestLoadExclusions_MissingFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	_, err := LoadExclusions(path)
	if err == nil {
		t.Fatal("LoadExclusions of a missing file = nil error")
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
	exc, err := LoadExclusions(path)
	if err != nil {
		t.Fatalf("LoadExclusions: %v", err)
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
	if _, err := LoadExclusions(path); err == nil {
		t.Fatal("LoadExclusions of a malformed file = nil error")
	}
}

func TestLoad_StdinRequiresFormat(t *testing.T) {
	_, err := Load(context.Background(), "-", "", strings.NewReader("identifier\nENG-1\n"))
	if err == nil {
		t.Fatal("expected an error when path is - without a format")
	}
	if !strings.Contains(err.Error(), "-stdin-format") {
		t.Errorf("error %q should name the -stdin-format flag", err)
	}
}

func TestLoad_StdinCSV(t *testing.T) {
	const csv = "identifier,created_at\nENG-1,2025-01-01\nENG-2,2025-01-02\n"
	got, err := Load(context.Background(), "-", "csv", strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 || got[0].Identifier != "ENG-1" {
		t.Errorf("got %+v, want 2 issues starting with ENG-1", got)
	}
}

func TestReadExclusions(t *testing.T) {
	dir := t.TempDir()

	if _, err := ReadExclusions(filepath.Join(dir, "missing.json")); !os.IsNotExist(err) {
		t.Errorf("missing file: err = %v, want a not-exist error", err)
	}

	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{"global": ["not-a-date"]}`), 0o644)
	data, _ := os.ReadFile(bad)
	_, wantErr := simulate.ParseExclusions(data)
	if _, err := ReadExclusions(bad); err == nil || err.Error() != wantErr.Error() {
		t.Errorf("invalid file: err = %v, want %v", err, wantErr)
	}

	good := filepath.Join(dir, "good.json")
	os.WriteFile(good, []byte(`{"global": ["2026-12-25"]}`), 0o644)
	if _, err := ReadExclusions(good); err != nil {
		t.Errorf("valid file: %v", err)
	}
}
