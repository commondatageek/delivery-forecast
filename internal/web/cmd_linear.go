package web

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/linear"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/report"
	"github.com/commondatageek/delivery-forecast/internal/sqlite"
	"github.com/commondatageek/delivery-forecast/internal/syncer"
)

func newLinearSyncFlags(fs *flag.FlagSet, _ time.Time) any  { return params.NewLinearSync(fs) }
func newLinearTeamsFlags(fs *flag.FlagSet, _ time.Time) any { return params.NewLinearTeams(fs) }

type linearTeamDTO struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type linearTeamsDTO struct {
	Teams []linearTeamDTO `json:"teams"`
}

func newLinearTeamsDTO(teams []linear.Team) linearTeamsDTO {
	d := linearTeamsDTO{Teams: []linearTeamDTO{}}
	for _, t := range teams {
		d.Teams = append(d.Teams, linearTeamDTO{t.Key, t.Name})
	}
	return d
}

func prepareLinearTeams(rc *runContext, _ any) (*runner, error) {
	apiKey, err := linear.GetAPIKey()
	if err != nil {
		return nil, err
	}
	return &runner{run: func(events) (*runOutput, error) {
		teams, err := linear.New(apiKey).ListTeams(rc.ctx)
		out := &runOutput{}
		if err != nil {
			return out, err
		}
		var text strings.Builder
		report.LinearTeams(&text, teams)
		out.Text = text.String()
		out.Result = newLinearTeamsDTO(teams)
		out.Exports = map[string]export{"json": jsonExport(out.Result)}
		return out, nil
	}}, nil
}

func prepareLinearSync(rc *runContext, b any) (*runner, error) {
	c := b.(*params.LinearSync)
	r, err := c.Resolve()
	rc.warnings = append(rc.warnings, r.Warnings...)
	if err != nil {
		return nil, err
	}
	absDB, err := rc.s.resolvePath("db", r.InputPath)
	if err != nil {
		return nil, err
	}
	apiKey, err := linear.GetAPIKey()
	if err != nil {
		return nil, err
	}
	opts := r.Request
	typedPath := r.InputPath
	return &runner{run: func(ev events) (*runOutput, error) {
		out := &runOutput{}
		store, err := sqlite.Open(absDB)
		if err != nil {
			return out, fmt.Errorf("open db: %w", err)
		}
		var lines []string
		opts.Logf = func(format string, args ...any) {
			line := fmt.Sprintf(format, args...)
			lines = append(lines, line)
			ev.Log(line)
		}
		// Not the request context: a closed tab must not abort a half-finished
		// team. The syncer commits per team.
		runErr := syncer.Run(context.Background(), linear.New(apiKey), store, opts)
		store.Close()
		rc.s.datasets.invalidate(absDB)
		out.Text = strings.Join(lines, "\n")
		if out.Text != "" {
			out.Text += "\n"
		}
		out.Result = map[string]string{"db": typedPath}
		if runErr != nil {
			return out, runErr
		}
		return out, nil
	}}, nil
}

// handleLinearTeams serves GET /api/linear/teams.
func (s *Server) handleLinearTeams(w http.ResponseWriter, r *http.Request) {
	apiKey, err := linear.GetAPIKey()
	if err != nil {
		writeError(w, http.StatusBadRequest, err, nil)
		return
	}
	teams, err := linear.New(apiKey).ListTeams(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, newLinearTeamsDTO(teams))
}
