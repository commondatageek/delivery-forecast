package web

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxListedFiles = 500

type fileEntry struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	var exts []string
	switch r.URL.Query().Get("kind") {
	case "input":
		exts = dataExts
	case "exclusions":
		exts = []string{".json"}
	case "db":
		exts = []string{".db", ".sqlite", ".sqlite3"}
	case "raw":
		exts = []string{".csv"}
	case "map":
		exts = mapExts
	default:
		writeError(w, http.StatusBadRequest, errors.New(`kind must be "input", "exclusions", "db", "raw" or "map"`), nil)
		return
	}

	wd, _ := os.Getwd()
	root := wd
	if s.opts.AllowRemote {
		root = s.opts.DataDir
	}

	display := func(p string) string {
		if rel, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
		return p
	}
	entry := func(p string, d fs.DirEntry) (fileEntry, bool) {
		info, err := d.Info()
		if err != nil {
			return fileEntry{}, false
		}
		return fileEntry{Path: display(p), Size: info.Size(), Modified: info.ModTime().Format(time.RFC3339)}, true
	}

	files := []fileEntry{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		depth := 0
		if rel, _ := filepath.Rel(root, p); rel != "." {
			depth = strings.Count(rel, string(os.PathSeparator))
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || depth >= 2) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxListedFiles {
			return filepath.SkipAll
		}
		if hasExt(d.Name(), exts) {
			if e, ok := entry(p, d); ok {
				files = append(files, e)
			}
		}
		return nil
	})

	uploads := []fileEntry{}
	if des, err := os.ReadDir(s.opts.UploadDir); err == nil {
		for _, d := range des {
			if !d.IsDir() && hasExt(d.Name(), exts) {
				if e, ok := entry(filepath.Join(s.opts.UploadDir, d.Name()), d); ok {
					uploads = append(uploads, e)
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"root": root, "files": files, "uploads": uploads})
}
