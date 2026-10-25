package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err), nil)
		return false
	}
	return true
}

// splitHidden partitions flag values for def into usable params and the
// names of CLI-only flags that were dropped. input is always kept.
func splitHidden(def *commandDef, all map[string]string) (usable map[string]string, ignored []string) {
	usable = map[string]string{}
	for k, v := range all {
		if m, ok := metaFor(def.ID, k); ok && m.Hidden != "" && k != "input" {
			ignored = append(ignored, k)
			continue
		}
		usable[k] = v
	}
	sort.Strings(ignored)
	return usable, ignored
}

// handleConfigParse serves POST /api/config/parse: YAML config → params.
func (s *Server) handleConfigParse(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command string `json:"command"`
		YAML    string `json:"yaml"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	def := commandByID(body.Command)
	if def == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown command %q", body.Command), nil)
		return
	}
	cfg, err := util.ParseConfig([]byte(body.YAML))
	if err != nil {
		writeError(w, http.StatusBadRequest, err, nil)
		return
	}
	delete(cfg, "config")

	fs := newDiscardFlagSet(def.Name)
	def.newFlags(fs, s.opts.Now())
	known := map[string]string{}
	unknown := []string{}
	for k, v := range cfg {
		if fs.Lookup(k) == nil {
			unknown = append(unknown, k)
			continue
		}
		known[k] = v
	}
	sort.Strings(unknown)
	usable, ignored := splitHidden(def, known)
	writeJSON(w, http.StatusOK, map[string]any{"params": usable, "unknown": unknown, "ignored": nonNil(ignored)})
}

// handleConfigRender serves POST /api/config/render: params → YAML and CLI.
func (s *Server) handleConfigRender(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command string            `json:"command"`
		Params  map[string]string `json:"params"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	def := commandByID(body.Command)
	if def == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown command %q", body.Command), nil)
		return
	}
	fs := newDiscardFlagSet(def.Name)
	def.newFlags(fs, s.opts.Now())
	if err := s.applyParams(def, fs, body.Params); err != nil {
		writeError(w, http.StatusBadRequest, err, nil)
		return
	}
	yml, err := params.ConfigYAML(fs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"yaml": yml,
		"cli":  params.ShellJoin(params.CommandLine(def.Name, fs)),
	})
}

// matchCommand finds the registered command named at the start of args and
// returns it with the remaining arguments.
func matchCommand(args []string) (*commandDef, []string) {
	var best *commandDef
	rest := args
	for _, d := range registry {
		n := strings.Fields(d.Name)
		if len(n) > len(args) || (best != nil && len(strings.Fields(best.Name)) >= len(n)) {
			continue
		}
		ok := true
		for i := range n {
			if args[i] != n[i] {
				ok = false
				break
			}
		}
		if ok {
			best, rest = d, args[len(n):]
		}
	}
	return best, rest
}

// handleCLIParse serves POST /api/cli/parse: a pasted command → command and params.
func (s *Server) handleCLIParse(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Line string `json:"line"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	args, err := params.SplitCommandLine(body.Line)
	if err != nil {
		writeError(w, http.StatusBadRequest, err, nil)
		return
	}
	if len(args) > 0 && (args[0] == "forecast" || path.Base(args[0]) == "forecast") {
		args = args[1:]
	}
	def, rest := matchCommand(args)
	if def == nil {
		writeError(w, http.StatusBadRequest, errors.New("unknown command"), nil)
		return
	}
	all, err := ParamsFromArgs(def.ID, rest, s.opts.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err, nil)
		return
	}
	usable, ignored := splitHidden(def, all)
	writeJSON(w, http.StatusOK, map[string]any{"command": def.ID, "params": usable, "ignored": nonNil(ignored)})
}
