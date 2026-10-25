package source

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// Fingerprint identifies the exact DB file a run used: absolute path, size,
// modtime, and a streamed sha256. Hash/stat errors are recorded as a
// non-fatal Error field rather than aborting the run.
func Fingerprint(path string) app.DataFile {
	d := app.DataFile{Path: path}
	if abs, err := filepath.Abs(path); err == nil {
		d.Path = abs
	}
	info, err := os.Stat(d.Path)
	if err != nil {
		d.Error = err.Error()
		return d
	}
	d.Size = info.Size()
	d.ModTime = info.ModTime().UTC().Format(time.RFC3339)
	f, err := os.Open(d.Path)
	if err != nil {
		d.Error = err.Error()
		return d
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		d.Error = err.Error()
		return d
	}
	d.SHA256 = hex.EncodeToString(h.Sum(nil))
	return d
}
