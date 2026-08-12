package issues

import "time"

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
	StateType            string    `json:"state_type"` // raw workflow state type, e.g. Linear's state.type
	StateName            string    `json:"state_name"` // human-readable workflow state name, e.g. Linear's state.name
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
// CompletedAt; falls back to StateType == "completed" when CompletedAt is
// zero.
func (i Issue) IsCompleted() bool {
	if !i.CompletedAt.IsZero() {
		return true
	}
	return i.StateType == "completed"
}

// IsCanceled reports whether the issue was canceled (including duplicates,
// which the codebase treats as a variant of canceled — see the package doc
// comment). Prefers CanceledAt; falls back to StateType when CanceledAt is
// zero.
func (i Issue) IsCanceled() bool {
	if !i.CanceledAt.IsZero() {
		return true
	}
	return i.StateType == "canceled" || i.StateType == "duplicate"
}

// IsTerminal reports IsCompleted() || IsCanceled().
func (i Issue) IsTerminal() bool {
	return i.IsCompleted() || i.IsCanceled()
}

// IsInProgress reports whether work has started and not yet finished: a
// non-zero StartedAt (or StateType == "started") and !IsTerminal().
func (i Issue) IsInProgress() bool {
	started := !i.StartedAt.IsZero() || i.StateType == "started"
	return started && !i.IsTerminal()
}
