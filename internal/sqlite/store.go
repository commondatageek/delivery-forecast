package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"

	_ "modernc.org/sqlite"
)

// Store is the concrete SQLite-backed issue store.
// All SQL in this repo lives here.
type Store struct {
	db *sql.DB
}

// schema is the full (and only) table definition. The store just replicates
// Linear's own data, so rebuilding the database from scratch is trivial and
// cheap; there's no history worth preserving across schema changes, so a
// single idempotent CREATE IF NOT EXISTS replaces versioned migrations.
const schema = `
CREATE TABLE IF NOT EXISTS issues (
    identifier              TEXT NOT NULL PRIMARY KEY,
    title                   TEXT NOT NULL DEFAULT '',
    assignee                TEXT,
    team_key                TEXT NOT NULL DEFAULT '',
    team_name               TEXT NOT NULL DEFAULT '',
    project_id              TEXT,
    project_name            TEXT,
    project_milestone_id    TEXT,
    project_milestone_name  TEXT,
    state_type              TEXT NOT NULL DEFAULT '',
    state_name              TEXT NOT NULL DEFAULT '',
    created_at              DATETIME,
    started_at              DATETIME,
    completed_at            DATETIME,
    canceled_at             DATETIME,
    archived_at             DATETIME,
    auto_archived_at        DATETIME,
    added_to_project_at     DATETIME,
    updated_at              DATETIME
);

CREATE INDEX IF NOT EXISTS idx_issues_team_key_updated_at ON issues (team_key, updated_at);
CREATE INDEX IF NOT EXISTS idx_issues_completed_at        ON issues (completed_at);
`

// Open opens (or creates) the SQLite database at path and ensures the schema
// exists. Since this always creates a missing file, it's meant for the
// ingest path (`linear sync`), which legitimately seeds a brand-new database.
// Read-only commands should use OpenExisting instead.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Enable WAL mode and foreign keys.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("pragma: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &Store{db: db}, nil
}

// OpenExisting opens the SQLite database at path, first checking that the
// file already exists. Use this for read-only commands (sim, count, aging,
// cfd): since SQLite otherwise creates a missing file lazily, a typo'd or
// wrong -db path would silently open an empty database and proceed rather
// than failing.
func OpenExisting(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("database %q does not exist", path)
		}
		return nil, fmt.Errorf("stat db %q: %w", path, err)
	}
	return Open(path)
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Upsert inserts or updates issues. The unique key is identifier.
func (s *Store) Upsert(ctx context.Context, items ...issues.Issue) error {
	const q = `
INSERT INTO issues
    (identifier, title, assignee, team_key, team_name, project_id, project_name,
     project_milestone_id, project_milestone_name, state_type, state_name,
     created_at, started_at, completed_at, canceled_at, archived_at, auto_archived_at,
     added_to_project_at, updated_at)
VALUES
    (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(identifier) DO UPDATE SET
    title                  = excluded.title,
    assignee               = excluded.assignee,
    team_key               = excluded.team_key,
    team_name              = excluded.team_name,
    project_id             = excluded.project_id,
    project_name           = excluded.project_name,
    project_milestone_id   = excluded.project_milestone_id,
    project_milestone_name = excluded.project_milestone_name,
    state_type             = excluded.state_type,
    state_name             = excluded.state_name,
    created_at             = excluded.created_at,
    started_at             = excluded.started_at,
    completed_at           = excluded.completed_at,
    canceled_at            = excluded.canceled_at,
    archived_at            = excluded.archived_at,
    auto_archived_at       = excluded.auto_archived_at,
    added_to_project_at    = excluded.added_to_project_at,
    updated_at             = excluded.updated_at`

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, q)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for _, it := range items {
		_, err := stmt.ExecContext(ctx,
			it.Identifier,
			it.Title,
			nullString(it.Assignee),
			it.TeamKey,
			it.TeamName,
			nullString(it.ProjectID),
			nullString(it.ProjectName),
			nullString(it.ProjectMilestoneID),
			nullString(it.ProjectMilestoneName),
			it.StateType,
			it.StateName,
			nullTime(it.CreatedAt),
			nullTime(it.StartedAt),
			nullTime(it.CompletedAt),
			nullTime(it.CanceledAt),
			nullTime(it.ArchivedAt),
			nullTime(it.AutoArchivedAt),
			nullTime(it.AddedToProjectAt),
			nullTime(it.UpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("upsert %s: %w", it.Identifier, err)
		}
	}

	return tx.Commit()
}

// AllIssues returns every row in the issues table, unfiltered, so callers can
// apply issues.Filter in memory and get semantics identical to file sources.
func (s *Store) AllIssues(ctx context.Context) ([]issues.Issue, error) {
	const q = `
SELECT identifier, title, assignee, team_key, team_name, project_id, project_name,
       project_milestone_id, project_milestone_name, state_type, state_name,
       created_at, started_at, completed_at, canceled_at, archived_at, auto_archived_at,
       added_to_project_at, updated_at
FROM issues`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("AllIssues: %w", err)
	}
	defer rows.Close()

	var out []issues.Issue
	for rows.Next() {
		var it issues.Issue
		var assignee, projectID, projectName, milestoneID, milestoneName sql.NullString
		var createdAt, startedAt, completedAt, canceledAt, archivedAt, autoArchivedAt, addedToProjectAt, updatedAt sql.NullTime
		if err := rows.Scan(
			&it.Identifier, &it.Title, &assignee, &it.TeamKey, &it.TeamName,
			&projectID, &projectName, &milestoneID, &milestoneName,
			&it.StateType, &it.StateName,
			&createdAt, &startedAt, &completedAt, &canceledAt, &archivedAt, &autoArchivedAt,
			&addedToProjectAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("AllIssues scan: %w", err)
		}
		it.Assignee = assignee.String
		it.ProjectID = projectID.String
		it.ProjectName = projectName.String
		it.ProjectMilestoneID = milestoneID.String
		it.ProjectMilestoneName = milestoneName.String
		if createdAt.Valid {
			it.CreatedAt = createdAt.Time
		}
		if startedAt.Valid {
			it.StartedAt = startedAt.Time
		}
		if completedAt.Valid {
			it.CompletedAt = completedAt.Time
		}
		if canceledAt.Valid {
			it.CanceledAt = canceledAt.Time
		}
		if archivedAt.Valid {
			it.ArchivedAt = archivedAt.Time
		}
		if autoArchivedAt.Valid {
			it.AutoArchivedAt = autoArchivedAt.Time
		}
		if addedToProjectAt.Valid {
			it.AddedToProjectAt = addedToProjectAt.Time
		}
		if updatedAt.Valid {
			it.UpdatedAt = updatedAt.Time
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// LatestUpdatedAtForTeam returns the maximum updated_at among issues for the
// given team key. Returns zero time if the team has no issues yet (signals a
// full fetch for that team).
func (s *Store) LatestUpdatedAtForTeam(ctx context.Context, teamKey string) (time.Time, error) {
	// Selecting the updated_at column directly (rather than MAX(updated_at))
	// keeps the result typed as DATETIME, which the sqlite driver requires
	// in order to scan it back into a time.Time instead of a string.
	row := s.db.QueryRowContext(ctx,
		`SELECT updated_at FROM issues WHERE team_key = ? ORDER BY updated_at DESC LIMIT 1`,
		teamKey)

	var ts sql.NullTime
	if err := row.Scan(&ts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("latest updated_at for team %s: %w", teamKey, err)
	}
	if !ts.Valid {
		return time.Time{}, nil
	}
	return ts.Time, nil
}

// DistinctTeamKeys returns every non-empty team_key currently present in the
// store, ordered alphabetically.
func (s *Store) DistinctTeamKeys(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT team_key FROM issues WHERE team_key <> '' ORDER BY team_key`)
	if err != nil {
		return nil, fmt.Errorf("distinct team keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("distinct team keys scan: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// nullTime converts a time.Time to sql.NullTime, treating zero as NULL.
func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

// nullString converts a string to sql.NullString, treating "" as NULL. Used
// for the optional columns (assignee, project, milestone) so that absent data
// is stored as NULL rather than an empty string.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
