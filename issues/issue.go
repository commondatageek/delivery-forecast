package issues

import (
	"strings"
	"time"
)

// Status is the normalized lifecycle phase every connector maps its tracker's
// workflow onto. Timestamps remain the primary signal (IsCompleted prefers
// CompletedAt); Status is the fallback and the display grouping.
type Status string

const (
	StatusUnknown   Status = ""          // not provided (file input may omit it)
	StatusBacklog   Status = "backlog"   // not started.   Linear: triage/backlog/unstarted. Jira: category "new"
	StatusStarted   Status = "started"   // in progress.   Linear: started.                  Jira: category "indeterminate"
	StatusCompleted Status = "completed" // finished, delivered. Linear: completed.          Jira: category "done", non-canceled resolution
	StatusCanceled  Status = "canceled"  // finished, not delivered. Linear: canceled/duplicate. Jira: category "done", canceled resolution/status
)

// ParseStatus accepts exactly the four values above (case-insensitive,
// surrounding whitespace ignored). Anything else returns StatusUnknown and
// false, never an error — a file's status column is advisory.
func ParseStatus(s string) (Status, bool) {
	switch st := Status(strings.ToLower(strings.TrimSpace(s))); st {
	case StatusBacklog, StatusStarted, StatusCompleted, StatusCanceled:
		return st, true
	}
	return StatusUnknown, false
}

// Issue is the neutral record every forecast command reads: the same shape
// as the SQLite issues table, and the JSON tags/CSV column names file
// sources use are exactly its snake_case column names (see
// DATA_REQUIREMENTS.md).
type Issue struct {
	Identifier           string    `json:"identifier"`
	Title                string    `json:"title"`
	Assignee             string    `json:"assignee"`
	TeamKey              string    `json:"team_key"`
	TeamName             string    `json:"team_name"`
	ProjectID            string    `json:"project_id"`
	ProjectName          string    `json:"project_name"`
	ProjectMilestoneID   string    `json:"project_milestone_id"`
	ProjectMilestoneName string    `json:"project_milestone_name"`
	Status               Status    `json:"status"`      // normalized lifecycle phase
	StatusName           string    `json:"status_name"` // the tracker's own status label, e.g. Linear's state.name
	CreatedAt            time.Time `json:"created_at"`
	StartedAt            time.Time `json:"started_at"`
	CompletedAt          time.Time `json:"completed_at"`
	CanceledAt           time.Time `json:"canceled_at"`
	ArchivedAt           time.Time `json:"archived_at"`
	AutoArchivedAt       time.Time `json:"auto_archived_at"`
	AddedToProjectAt     time.Time `json:"added_to_project_at"`
	UpdatedAt            time.Time `json:"updated_at"` // drives incremental fetch
}

// IsCompleted reports whether the issue finished successfully. Prefers
// CompletedAt; falls back to Status == StatusCompleted when CompletedAt is
// zero.
func (i Issue) IsCompleted() bool {
	if !i.CompletedAt.IsZero() {
		return true
	}
	return i.Status == StatusCompleted
}

// IsCanceled reports whether the issue finished without being delivered.
// Prefers CanceledAt; falls back to Status == StatusCanceled when CanceledAt
// is zero.
func (i Issue) IsCanceled() bool {
	if !i.CanceledAt.IsZero() {
		return true
	}
	return i.Status == StatusCanceled
}

// IsTerminal reports IsCompleted() || IsCanceled().
func (i Issue) IsTerminal() bool {
	return i.IsCompleted() || i.IsCanceled()
}

// IsInProgress reports whether work has started and not yet finished: a
// non-zero StartedAt (or Status == StatusStarted) and !IsTerminal().
func (i Issue) IsInProgress() bool {
	started := !i.StartedAt.IsZero() || i.Status == StatusStarted
	return started && !i.IsTerminal()
}
