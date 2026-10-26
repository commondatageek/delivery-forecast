package web

import "net/http"

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	b := s.opts.Binary
	writeJSON(w, http.StatusOK, map[string]any{
		"version": s.opts.Version,
		"binary": map[string]any{
			"version_override": b.VersionOverride,
			"git_sha":          b.GitSHA,
			"git_time":         b.GitTime,
			"dirty":            b.Dirty,
			"go_version":       b.GoVersion,
			"module":           b.Module,
		},
	})
}

func (s *Server) handleDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"input":      s.opts.DefaultInput,
		"exclusions": s.opts.DefaultExclusions,
	})
}
