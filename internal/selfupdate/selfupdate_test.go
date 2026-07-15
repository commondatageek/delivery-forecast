package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestAssetNameFor(t *testing.T) {
	cases := []struct {
		tag, goos, goarch, want string
	}{
		{"v1.0.0", "darwin", "arm64", "forecast_v1.0.0_darwin_arm64.tar.gz"},
		{"v1.0.0", "linux", "amd64", "forecast_v1.0.0_linux_amd64.tar.gz"},
		{"v1.0.0", "windows", "amd64", "forecast_v1.0.0_windows_amd64.zip"},
	}
	for _, c := range cases {
		got := assetNameFor(c.tag, c.goos, c.goarch)
		if got != c.want {
			t.Errorf("assetNameFor(%q, %q, %q) = %q, want %q", c.tag, c.goos, c.goarch, got, c.want)
		}
	}
}

func TestBinaryNameFor(t *testing.T) {
	if got := binaryNameFor("windows"); got != "forecast.exe" {
		t.Errorf("binaryNameFor(windows) = %q, want forecast.exe", got)
	}
	if got := binaryNameFor("linux"); got != "forecast" {
		t.Errorf("binaryNameFor(linux) = %q, want forecast", got)
	}
}

func TestFindAsset(t *testing.T) {
	r := Release{
		TagName: "v1.0.0",
		Assets: []Asset{
			{Name: "forecast_v1.0.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/a"},
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/c"},
		},
	}
	if a, ok := FindAsset(r, "forecast_v1.0.0_linux_amd64.tar.gz"); !ok || a.BrowserDownloadURL != "https://example.com/a" {
		t.Errorf("FindAsset did not find expected asset: %+v, %v", a, ok)
	}
	if _, ok := FindAsset(r, "nonexistent"); ok {
		t.Error("FindAsset found a nonexistent asset")
	}
}

func TestParseChecksums(t *testing.T) {
	data := []byte("ABCDEF0123456789  forecast_v1.0.0_linux_amd64.tar.gz\n" +
		"1234567890ABCDEF  checksums.txt\n" +
		"\n")
	sums := ParseChecksums(data)
	if sums["forecast_v1.0.0_linux_amd64.tar.gz"] != "abcdef0123456789" {
		t.Errorf("unexpected checksum: %q", sums["forecast_v1.0.0_linux_amd64.tar.gz"])
	}
	if sums["checksums.txt"] != "1234567890abcdef" {
		t.Errorf("unexpected checksum: %q", sums["checksums.txt"])
	}
	if len(sums) != 2 {
		t.Errorf("expected 2 entries, got %d", len(sums))
	}
}

func TestVerifySHA256(t *testing.T) {
	data := []byte("hello world")
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])

	if err := VerifySHA256(data, want); err != nil {
		t.Errorf("VerifySHA256 with correct hash failed: %v", err)
	}
	if err := VerifySHA256(data, strings.ToUpper(want)); err != nil {
		t.Errorf("VerifySHA256 case-insensitive check failed: %v", err)
	}
	if err := VerifySHA256(data, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Error("VerifySHA256 with wrong hash succeeded, want error")
	}
}

func TestSameVersion(t *testing.T) {
	cases := []struct {
		current, latest string
		want             bool
	}{
		{"v1.2.3", "1.2.3", true},
		{"1.2.3", "v1.2.3", true},
		{"v1.2.3", "v1.2.3", true},
		{"v1.2.3", "v1.2.4", false},
		{"", "v1.2.3", false},
	}
	for _, c := range cases {
		if got := SameVersion(c.current, c.latest); got != c.want {
			t.Errorf("SameVersion(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestExtractBinaryTarGz(t *testing.T) {
	content := []byte("fake forecast binary contents")
	archive := buildTarGz(t, map[string][]byte{
		"forecast":  content,
		"README.md": []byte("readme"),
	})
	got, err := ExtractBinary(archive, "forecast_v1.0.0_linux_amd64.tar.gz", "forecast")
	if err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted content = %q, want %q", got, content)
	}
}

func TestExtractBinaryZip(t *testing.T) {
	content := []byte("fake forecast.exe binary contents")
	archive := buildZip(t, map[string][]byte{
		"forecast.exe": content,
		"README.md":    []byte("readme"),
	})
	got, err := ExtractBinary(archive, "forecast_v1.0.0_windows_amd64.zip", "forecast.exe")
	if err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted content = %q, want %q", got, content)
	}
}

func TestExtractBinaryNotFound(t *testing.T) {
	archive := buildTarGz(t, map[string][]byte{"README.md": []byte("readme")})
	if _, err := ExtractBinary(archive, "forecast_v1.0.0_linux_amd64.tar.gz", "forecast"); err == nil {
		t.Error("ExtractBinary found a missing binary, want error")
	}
}

func buildTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write tar content: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("write zip content: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}
