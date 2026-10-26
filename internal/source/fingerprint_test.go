package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDBFingerprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := Fingerprint(path)
	if d.Error != "" {
		t.Fatalf("Fingerprint error = %q, want none", d.Error)
	}
	if d.Size != 5 {
		t.Fatalf("Size = %d, want 5", d.Size)
	}
	// sha256("hello")
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if d.SHA256 != want {
		t.Fatalf("SHA256 = %q, want %q", d.SHA256, want)
	}
	if !filepath.IsAbs(d.Path) {
		t.Fatalf("Path = %q, want absolute", d.Path)
	}
}

func TestDBFingerprint_MissingFile(t *testing.T) {
	d := Fingerprint(filepath.Join(t.TempDir(), "missing.db"))
	if d.Error == "" {
		t.Fatal("Fingerprint on a missing file: want a non-empty Error, got none")
	}
}
