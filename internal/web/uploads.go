package web

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var dataExts = []string{".csv", ".json", ".jsonl", ".ndjson", ".db", ".sqlite", ".sqlite3"}

func hasExt(name string, exts []string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

func sanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		}
		return '_'
	}, name)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.opts.MaxUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("expected a multipart upload: %w", err), nil)
		return
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			writeError(w, http.StatusBadRequest, errors.New(`missing form field "file"`), nil)
			return
		}
		if err != nil {
			s.uploadReadError(w, err)
			return
		}
		if part.FormName() != "file" {
			continue
		}
		s.storeUpload(w, part.FileName(), part)
		return
	}
}

func (s *Server) uploadReadError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, errors.New("upload exceeds -max-upload-mb"), nil)
		return
	}
	writeError(w, http.StatusBadRequest, err, nil)
}

func (s *Server) storeUpload(w http.ResponseWriter, origName string, body io.Reader) {
	if !hasExt(origName, dataExts) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported file type %q", filepath.Ext(origName)), nil)
		return
	}
	tmp, err := os.CreateTemp(s.opts.UploadDir, "upload-*.tmp")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, nil)
		return
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		s.uploadReadError(w, err)
		return
	}

	final := filepath.Join(s.opts.UploadDir, hex.EncodeToString(h.Sum(nil))[:12]+"-"+sanitizeName(filepath.Base(origName)))
	if _, err := os.Stat(final); err != nil {
		if err := os.Rename(tmp.Name(), final); err != nil {
			writeError(w, http.StatusInternalServerError, err, nil)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": final, "name": origName, "size": n})
}
