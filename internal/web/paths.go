package web

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// resolvePath validates a user-supplied path parameter (field names the flag,
// for FieldError). "-" is rejected. Relative paths are resolved against the
// server's working directory, exactly as the CLI resolves them. With
// AllowRemote, the cleaned, symlink-evaluated path must lie inside DataDir or
// UploadDir. Returns the absolute path.
func (s *Server) resolvePath(field, p string) (string, error) {
	if p == "-" {
		return "", &app.FieldError{Fields: []string{field}, Msg: fmt.Sprintf("-%s -: stdin is not available in the web UI; upload the file instead", field)}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", &app.FieldError{Fields: []string{field}, Msg: err.Error()}
	}
	if !s.opts.AllowRemote {
		return abs, nil
	}
	real := evalExisting(abs)
	for _, root := range []string{s.opts.DataDir, s.opts.UploadDir} {
		if within(evalExisting(root), real) {
			return abs, nil
		}
	}
	return "", &app.FieldError{Fields: []string{field}, Msg: fmt.Sprintf("-%s: path is outside -data-dir", field)}
}

// evalExisting resolves symlinks in p, falling back to the deepest existing
// ancestor for paths that don't exist yet.
func evalExisting(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// within reports whether path is root or inside it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
