// Package web is the forecast CLI's second adapter: an HTTP server exposing
// every analysis command as a JSON API and serving an embedded browser UI.
package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// Options configures a Server. Zero values get the defaults noted.
type Options struct {
	DefaultInput      string
	DefaultExclusions string
	UploadDir         string // required
	MaxUploadBytes    int64  // default 512 MiB
	AllowRemote       bool
	DataDir           string // required when AllowRemote
	Binary            app.BinaryInfo
	Version           string
	Now               func() time.Time                 // default time.Now
	Logf              func(format string, args ...any) // default: discard
}

// Server is the forecast web UI's HTTP server. Create it with New.
type Server struct {
	opts     Options
	mux      *http.ServeMux
	datasets *datasetCache
	runs     *runStore
	runSlots chan struct{} // capacity 1: one simulation at a time (D10)
}

const defaultMaxUpload = 512 << 20

// New validates opts, creates the upload directory and builds the routes.
func New(opts Options) (*Server, error) {
	if opts.UploadDir == "" {
		return nil, errors.New("web: UploadDir is required")
	}
	if opts.AllowRemote && opts.DataDir == "" {
		return nil, errors.New("web: DataDir is required with AllowRemote")
	}
	if opts.MaxUploadBytes <= 0 {
		opts.MaxUploadBytes = defaultMaxUpload
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	uploadDir, err := filepath.Abs(opts.UploadDir)
	if err != nil {
		return nil, err
	}
	opts.UploadDir = uploadDir
	if err := os.MkdirAll(opts.UploadDir, 0o700); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	if opts.DataDir != "" {
		dataDir, err := filepath.Abs(opts.DataDir)
		if err != nil {
			return nil, err
		}
		opts.DataDir = dataDir
	}

	s := &Server{opts: opts, mux: http.NewServeMux(), datasets: newDatasetCache(4), runs: newRunStore(), runSlots: make(chan struct{}, 1)}
	s.routes()
	return s, nil
}

// Handler returns the server's root handler with the security middleware applied.
func (s *Server) Handler() http.Handler { return s.secure(s.mux) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/version", s.handleVersion)
	s.mux.HandleFunc("GET /api/defaults", s.handleDefaults)
	s.mux.HandleFunc("GET /api/files", s.handleFiles)
	s.mux.HandleFunc("POST /api/uploads", s.handleUpload)
	s.mux.HandleFunc("GET /api/datasets/summary", s.handleSummary)
	s.mux.HandleFunc("GET /api/commands", s.handleCommands)
	s.mux.HandleFunc("POST /api/run/{id}", s.handleRun)
	s.mux.HandleFunc("GET /api/runs/{run_id}/export/{format}", s.handleExport)
	s.mux.Handle("/", staticHandler())
}

// writeJSON writes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Message string   `json:"message"`
	Fields  []string `json:"fields,omitempty"`
}

// writeError writes the error body; a *app.FieldError contributes "fields".
func writeError(w http.ResponseWriter, status int, err error, warnings []string) {
	body := errorBody{Message: err.Error()}
	var fe *app.FieldError
	if errors.As(err, &fe) {
		body.Fields = fe.Fields
		if body.Fields == nil {
			body.Fields = []string{}
		}
	}
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, status, struct {
		Error    errorBody `json:"error"`
		Warnings []string  `json:"warnings"`
	}{body, warnings})
}
