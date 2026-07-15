// Package selfupdate implements the logic behind `forecast update`: looking
// up the latest GitHub release, picking the right asset for this OS/arch,
// verifying its checksum, extracting the binary, and atomically replacing
// the running executable. The network/file orchestration is split from pure
// helpers (asset naming, checksum parsing, archive extraction, version
// comparison) so the risky-but-pure logic is unit testable without hitting
// the network or the filesystem.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Owner and Repo identify the GitHub repository releases are published to.
const (
	Owner = "commondatageek"
	Repo  = "delivery-forecast"
)

// latestReleaseURL is the GitHub API endpoint for the newest non-prerelease
// release.
const latestReleaseURL = "https://api.github.com/repos/" + Owner + "/" + Repo + "/releases/latest"

// ChecksumsAssetName is the name of the checksums file attached to every
// release.
const ChecksumsAssetName = "checksums.txt"

// Release is the subset of GitHub's release API response this package needs.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is a single file attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// LatestRelease fetches the newest non-prerelease release from GitHub.
func LatestRelease(ctx context.Context, client *http.Client) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return Release{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Release{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return Release{}, fmt.Errorf("HTTP %d fetching latest release: %s", resp.StatusCode, snippet)
	}

	var rel Release
	if err := json.Unmarshal(body, &rel); err != nil {
		return Release{}, fmt.Errorf("unmarshal release: %w", err)
	}
	return rel, nil
}

// AssetName returns the release-asset archive name for the current
// platform, e.g. "forecast_v1.0.0_darwin_arm64.tar.gz" or
// "forecast_v1.0.0_windows_amd64.zip".
func AssetName(tag string) string {
	return assetNameFor(tag, runtime.GOOS, runtime.GOARCH)
}

func assetNameFor(tag, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("forecast_%s_%s_%s.%s", tag, goos, goarch, ext)
}

// BinaryNameInArchive returns the name of the forecast binary inside a
// release archive for the current platform ("forecast" or "forecast.exe").
func BinaryNameInArchive() string {
	return binaryNameFor(runtime.GOOS)
}

func binaryNameFor(goos string) string {
	if goos == "windows" {
		return "forecast.exe"
	}
	return "forecast"
}

// FindAsset returns the asset in r whose Name matches exactly.
func FindAsset(r Release, assetName string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == assetName {
			return a, true
		}
	}
	return Asset{}, false
}

// ParseChecksums parses a checksums.txt file (sha256sum format: one
// "<hex>  <filename>" line per entry) into a map of filename to lowercase
// hex sha256.
func ParseChecksums(data []byte) map[string]string {
	sums := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sums[fields[1]] = strings.ToLower(fields[0])
	}
	return sums
}

// VerifySHA256 reports an error unless sha256(data) equals wantHex
// (case-insensitive).
func VerifySHA256(data []byte, wantHex string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	want := strings.ToLower(wantHex)
	if got != want {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

// ExtractBinary returns the bytes of the binName entry inside archive, an
// in-memory .tar.gz or .zip file named assetName.
func ExtractBinary(archive []byte, assetName, binName string) ([]byte, error) {
	if strings.HasSuffix(assetName, ".zip") {
		return extractFromZip(archive, binName)
	}
	return extractFromTarGz(archive, binName)
}

func extractFromZip(archive []byte, binName string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	for _, f := range r.File {
		if filepath.Base(f.Name) != binName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("read zip entry %s: %w", f.Name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("binary %q not found in archive", binName)
}

func extractFromTarGz(archive []byte, binName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if filepath.Base(hdr.Name) != binName {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read tar entry %s: %w", hdr.Name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("binary %q not found in archive", binName)
}

// ReplaceExecutable atomically replaces targetPath with newBinary. It writes
// to a temp file in the same directory (so the rename is same-filesystem)
// before swapping it in.
func ReplaceExecutable(targetPath string, newBinary []byte) error {
	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, ".forecast-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(newBinary); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if runtime.GOOS == "windows" {
		oldPath := targetPath + ".old"
		if err := os.Rename(targetPath, oldPath); err != nil {
			os.Remove(tmpPath)
			return wrapRenameErr(err, targetPath)
		}
		if err := os.Rename(tmpPath, targetPath); err != nil {
			os.Remove(tmpPath)
			return wrapRenameErr(err, targetPath)
		}
		os.Remove(oldPath) // best-effort; fails while the old binary is still running
		return nil
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return wrapRenameErr(err, targetPath)
	}
	return nil
}

func wrapRenameErr(err error, targetPath string) error {
	if os.IsPermission(err) {
		return fmt.Errorf("permission denied replacing %s (try re-running with elevated privileges): %w", targetPath, err)
	}
	return fmt.Errorf("replace %s: %w", targetPath, err)
}

// SameVersion reports whether current and latestTag refer to the same
// release, ignoring a leading "v". This is a simple equality check, not
// semver ordering: /releases/latest is already the authoritative newest
// stable release.
func SameVersion(current, latestTag string) bool {
	return strings.TrimPrefix(current, "v") == strings.TrimPrefix(latestTag, "v")
}

// download performs a GET request and returns the response body, following
// redirects (the default http.Client behavior), which release asset
// download URLs rely on.
func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d downloading %s", resp.StatusCode, url)
	}
	return body, nil
}

// Download fetches the given URL's body via client. Exported for use by
// cmd/forecast to download both the release asset and checksums.txt.
func Download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	return download(ctx, client, url)
}
