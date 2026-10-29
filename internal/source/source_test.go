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

func TestLoad_TrackerExportHint(t *testing.T) {
	const path = "../../testdata/jira-export-sample.csv"
	_, err := Load(context.Background(), path, "", nil)
	if err == nil {
		t.Fatal("Load of a raw Jira export = nil error")
	}
	want := path + `: this looks like a Jira Cloud CSV export (template "jira-cloud-csv"), which needs converting first:
  forecast import -example jira-cloud-csv > mapping.yaml
  forecast import -from ` + path + ` -map mapping.yaml -out data.db`
	if err.Error() != want {
		t.Fatalf("error =\n%s\nwant\n%s", err, want)
	}
}

func TestLoad_TrackerExportHintQuotesPath(t *testing.T) {
	data, err := os.ReadFile("../../testdata/jira-export-sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Workload Export (Jira).csv")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Load(context.Background(), path, "", nil)
	if err == nil || !strings.Contains(err.Error(), "forecast import -from '"+path+"' -map") {
		t.Fatalf("error = %v, want the path single-quoted in the suggested command", err)
	}
}

func TestLoad_HeaderErrorWithoutTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.csv")
	if err := os.WriteFile(path, []byte("Summary,Status\nx,Done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(context.Background(), path, "", nil)
	want := path + `: no "identifier" column; expected the forecast column names (see DATA_REQUIREMENTS.md)`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"data/export.csv":      "data/export.csv",
		"My Export (1).csv":    "'My Export (1).csv'",
		"it's.csv":             `'it'\''s.csv'`,
		"C:/x/report-2026.csv": "C:/x/report-2026.csv",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
