package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/params"
)

// runContext carries one run request through preparation.
type runContext struct {
	ctx      context.Context
	s        *Server
	def      *commandDef
	fs       *flag.FlagSet
	now      time.Time
	warnings []string // pre-run warnings (Resolve's), prepended to the result's
}

// loadInput resolves and loads an input-path parameter (field "input" or "db").
// path is exactly what the user typed, so error text matches the CLI's.
func (rc *runContext) loadInput(field, path string) (*dataset, error) {
	abs, err := rc.s.resolvePath(field, path)
	if err != nil {
		return nil, err
	}
	ds, err := rc.s.datasets.get(rc.ctx, abs)
	if err != nil {
		return nil, &app.FieldError{Fields: []string{field}, Msg: "load issues: " + strings.ReplaceAll(err.Error(), abs, path)}
	}
	return ds, nil
}

// runner executes a prepared run.
type runner struct {
	run func(ev events) (*runOutput, error)
}

// events is how a running command reports progress and log lines.
type events interface {
	Progress(done, total int)
	Log(msg string)
}

// runOutput is a finished run. Non-nil even alongside an error (Warnings valid).
type runOutput struct {
	Warnings []string
	Result   any    // JSON DTO (dto.go)
	Text     string // the first export's output; "" if that export is html
	Exports  map[string]export
}

// export renders one download from a stored result.
type export struct {
	ContentType string
	Ext         string // file extension, e.g. ".txt"
	Write       func(w io.Writer) error
}

const progressInterval = 100 * time.Millisecond

// streamEvents writes NDJSON events to a response. Progress callbacks arrive
// from several goroutines, so everything is guarded by a mutex.
type streamEvents struct {
	mu   sync.Mutex
	w    http.ResponseWriter
	fl   http.Flusher
	last time.Time
}

func (e *streamEvents) emit(v any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emitLocked(v)
}

func (e *streamEvents) emitLocked(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	e.w.Write(append(b, '\n'))
	if e.fl != nil {
		e.fl.Flush()
	}
}

func (e *streamEvents) Progress(done, total int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if done != total && time.Since(e.last) < progressInterval {
		return
	}
	e.last = time.Now()
	e.emitLocked(map[string]any{"type": "progress", "done": done, "total": total})
}

func (e *streamEvents) Log(msg string) {
	e.emit(map[string]any{"type": "log", "message": msg})
}

// discardEvents ignores everything.
type discardEvents struct{}

func (discardEvents) Progress(int, int) {}
func (discardEvents) Log(string)        {}

type flagRecordDTO struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Default string `json:"default"`
	Set     bool   `json:"set"`
}

// applyParams fills fs from a request's params (D3), in sorted key order,
// skipping empty values.
func (s *Server) applyParams(def *commandDef, fs *flag.FlagSet, p map[string]string) error {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := p[k]
		if v == "" {
			continue
		}
		f := fs.Lookup(k)
		m, hasMeta := metaFor(def.ID, k)
		switch {
		case f != nil && hasMeta && m.Hidden != "":
			return &app.FieldError{Fields: []string{k}, Msg: fmt.Sprintf("-%s is not available in the web UI", k)}
		case f == nil || !hasMeta:
			return &app.FieldError{Fields: []string{k}, Msg: fmt.Sprintf("unknown parameter -%s", k)}
		}
		if err := fs.Set(k, v); err != nil {
			return &app.FieldError{Fields: []string{k}, Msg: fmt.Sprintf("invalid value %q for -%s: %v", v, k, err)}
		}
		if isPathKind(m.Kind) {
			if _, err := s.resolvePath(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// ParamsFromArgs parses CLI-style args for command id and returns the flags
// that were explicitly set, as name → value strings (D3).
func ParamsFromArgs(id string, args []string, now time.Time) (map[string]string, error) {
	def := commandByID(id)
	if def == nil {
		return nil, fmt.Errorf("unknown command %q", id)
	}
	fs := newDiscardFlagSet(def.Name)
	def.newFlags(fs, now)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	out := map[string]string{}
	fs.Visit(func(f *flag.Flag) { out[f.Name] = f.Value.String() })
	return out, nil
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	def := commandByID(id)
	if def == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown command %q", id), nil)
		return
	}
	var body struct {
		Params map[string]string `json:"params"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err), nil)
		return
	}

	now := s.opts.Now()
	fs := newDiscardFlagSet(def.Name)
	bundle := def.newFlags(fs, now)
	if err := s.applyParams(def, fs, body.Params); err != nil {
		writeError(w, http.StatusBadRequest, err, nil)
		return
	}

	rc := &runContext{ctx: r.Context(), s: s, def: def, fs: fs, now: now}
	rn, err := def.prepare(rc, bundle)
	if err != nil {
		writeError(w, http.StatusBadRequest, err, rc.warnings)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	ev := &streamEvents{w: w, fl: fl}

	select {
	case s.runSlots <- struct{}{}:
	default:
		ev.emit(map[string]any{"type": "queued"})
		select {
		case s.runSlots <- struct{}{}:
		case <-r.Context().Done():
			return
		}
	}
	out, err := func() (*runOutput, error) {
		defer func() { <-s.runSlots }()
		return rn.run(ev)
	}()
	if out == nil {
		out = &runOutput{}
	}
	warnings := append(append([]string{}, rc.warnings...), out.Warnings...)

	if err != nil {
		var fe *app.FieldError
		e := errorBody{Message: err.Error()}
		if errors.As(err, &fe) {
			e.Fields = fe.Fields
		}
		ev.emit(map[string]any{"type": "error", "warnings": nonNil(warnings), "error": e})
		return
	}

	if first := firstExport(def); out.Text == "" && first != "" && first != "html" {
		var buf bytes.Buffer
		if err := out.Exports[first].Write(&buf); err == nil {
			out.Text = buf.String()
		}
	}
	runID := s.runs.add(def.ID, now, out)

	var formats []string
	for _, f := range def.Exports {
		if _, ok := out.Exports[f]; ok {
			formats = append(formats, f)
		}
	}
	cfgYAML, _ := params.ConfigYAML(fs)
	var flags []flagRecordDTO
	for _, fr := range params.FlagRecords(fs) {
		if m, ok := metaFor(def.ID, fr.Name); ok && m.Hidden != "" {
			continue
		}
		flags = append(flags, flagRecordDTO{fr.Name, fr.Value, fr.Default, fr.Set})
	}
	ev.emit(map[string]any{
		"type":     "result",
		"run_id":   runID,
		"command":  def.ID,
		"warnings": nonNil(warnings),
		"result":   out.Result,
		"text":     out.Text,
		"exports":  nonNil(formats),
		"cli":      params.ShellJoin(params.CommandLine(def.Name, fs)),
		"config":   cfgYAML,
		"flags":    flags,
	})
}

// firstExport names the export whose output is shown as "Text output", or ""
// for a command with no exports.
func firstExport(def *commandDef) string {
	if len(def.Exports) == 0 {
		return ""
	}
	return def.Exports[0]
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
