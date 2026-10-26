package web

import (
	"net/http"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

func dateOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Format("2006-01-02")
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	path, err := s.resolvePath("input", r.URL.Query().Get("input"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err, nil)
		return
	}
	ds, err := s.datasets.get(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusBadRequest, &app.FieldError{Fields: []string{"input"}, Msg: err.Error()}, nil)
		return
	}
	sum := app.Summarize(ds.issues)

	type project struct {
		Name       string   `json:"name"`
		Milestones []string `json:"milestones"`
	}
	type readiness struct {
		Command  string `json:"command"`
		Excluded int    `json:"excluded"`
		Status   string `json:"status"`
	}
	projects := []project{}
	for _, p := range sum.Projects {
		ms := p.Milestones
		if ms == nil {
			ms = []string{}
		}
		projects = append(projects, project{p.Name, ms})
	}
	ready := []readiness{}
	for _, c := range sum.Readiness {
		ready = append(ready, readiness{c.Command, c.Excluded, c.Status})
	}
	teams, assignees := sum.Teams, sum.Assignees
	if teams == nil {
		teams = []string{}
	}
	if assignees == nil {
		assignees = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":             path,
		"issues":           sum.Issues,
		"teams":            teams,
		"projects":         projects,
		"assignees":        assignees,
		"earliest_created": dateOrNil(sum.EarliestCreated),
		"latest_updated":   dateOrNil(sum.LatestUpdated),
		"readiness":        ready,
	})
}
