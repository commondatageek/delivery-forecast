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

// SchemaVersion is the issues table layout this build reads and writes,
// recorded in PRAGMA user_version. There are no migrations: the store is a
// replica of trackers' data (or of an imported file), so a database built by
// a different version is refused and the user rebuilds it.
const SchemaVersion = 2

// schema is the full (and only) table definition. Issues are keyed by
// (source, identifier) so one database can hold several trackers' issues
// without identifier collisions.
const schema = `
CREATE TABLE IF NOT EXISTS issues (
    source            TEXT NOT NULL DEFAULT '',
    identifier        TEXT NOT NULL,
    title             TEXT NOT NULL DEFAULT '',
    url               TEXT,
    assignee          TEXT,
    team_key          TEXT NOT NULL DEFAULT '',
    team_name         TEXT NOT NULL DEFAULT '',
    project_id        TEXT,
    project_name      TEXT,
    milestone_id      TEXT,
    milestone_name    TEXT,
    issue_type        TEXT,
    parent_identifier TEXT,
    status            TEXT NOT NULL DEFAULT '',
    status_name       TEXT NOT NULL DEFAULT '',
    resolution        TEXT,
    created_at        DATETIME,
    started_at        DATETIME,
    completed_at      DATETIME,
    canceled_at       DATETIME,
    updated_at        DATETIME,
    PRIMARY KEY (source, identifier)
);

CREATE INDEX IF NOT EXISTS idx_issues_source_team_updated ON issues (source, team_key, updated_at);
CREATE INDEX IF NOT EXISTS idx_issues_completed_at        ON issues (completed_at);
`

// Open opens (or creates) the SQLite database at path and ensures the schema
// exists. Since this always creates a missing file, it's meant for the
// ingest path (`linear sync`), which legitimately seeds a brand-new database.
// Read-only commands should use OpenExisting instead.
//
// A brand-new (table-less) database is stamped with SchemaVersion; any other
// database whose user_version differs is refused, naming both versions.
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

	if err := checkVersion(db, path); err != nil {
		db.Close()
		return nil, err
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &Store{db: db}, nil
}

// checkVersion stamps a brand-new database with SchemaVersion and refuses an
// existing one built by a different version. Databases from before
// versioning read as user_version 0.
func checkVersion(db *sql.DB, path string) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version == SchemaVersion {
		return nil
	}
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
		return fmt.Errorf("inspect schema: %w", err)
	}
	if version == 0 && tables == 0 {
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, SchemaVersion)); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
		return nil
	}
	return fmt.Errorf("%s: database schema is v%d, this build needs v%d; delete the file and create it again (e.g. forecast linear sync)", path, version, SchemaVersion)
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

// upsertSQL inserts an issue or, when its (source, identifier) is already
// stored, replaces every other column.
const upsertSQL = `
INSERT INTO issues
    (source, identifier, title, url, assignee, team_key, team_name,
     project_id, project_name, milestone_id, milestone_name,
     issue_type, parent_identifier, status, status_name, resolution,
     created_at, started_at, completed_at, canceled_at, updated_at)
VALUES
    (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, identifier) DO UPDATE SET
    title             = excluded.title,
    url               = excluded.url,
    assignee          = excluded.assignee,
    team_key          = excluded.team_key,
    team_name         = excluded.team_name,
    project_id        = excluded.project_id,
    project_name      = excluded.project_name,
    milestone_id      = excluded.milestone_id,
    milestone_name    = excluded.milestone_name,
    issue_type        = excluded.issue_type,
    parent_identifier = excluded.parent_identifier,
    status            = excluded.status,
    status_name       = excluded.status_name,
    resolution        = excluded.resolution,
    created_at        = excluded.created_at,
    started_at        = excluded.started_at,
    completed_at      = excluded.completed_at,
    canceled_at       = excluded.canceled_at,
    updated_at        = excluded.updated_at`

// Upsert inserts or updates issues. The unique key is (source, identifier).
func (s *Store) Upsert(ctx context.Context, items ...issues.Issue) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if err := upsertTx(ctx, tx, items); err != nil {
		return err
	}
	return tx.Commit()
}

// upsertTx runs upsertSQL for every item inside tx.
func upsertTx(ctx context.Context, tx *sql.Tx, items []issues.Issue) error {
	stmt, err := tx.PrepareContext(ctx, upsertSQL)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for _, it := range items {
		_, err := stmt.ExecContext(ctx,
			it.Source,
			it.Identifier,
			it.Title,
			nullString(it.URL),
			nullString(it.Assignee),
			it.TeamKey,
			it.TeamName,
			nullString(it.ProjectID),
			nullString(it.ProjectName),
			nullString(it.MilestoneID),
			nullString(it.MilestoneName),
			nullString(it.IssueType),
			nullString(it.ParentIdentifier),
			string(it.Status),
			it.StatusName,
			nullString(it.Resolution),
			nullTime(it.CreatedAt),
			nullTime(it.StartedAt),
			nullTime(it.CompletedAt),
			nullTime(it.CanceledAt),
			nullTime(it.UpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("upsert %s: %w", it.Identifier, err)
		}
	}
	return nil
}

// Delete removes source's issues by identifier. Identifiers not in the store
// (or stored under another source) are ignored.
func (s *Store) Delete(ctx context.Context, source string, identifiers ...string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, id := range identifiers {
		if _, err := tx.ExecContext(ctx, `DELETE FROM issues WHERE source = ? AND identifier = ?`, source, id); err != nil {
			return fmt.Errorf("delete %s: %w", id, err)
		}
	}
	return tx.Commit()
}

// AllIssues returns every row in the issues table, unfiltered, so callers can
// apply issues.Filter in memory and get semantics identical to file sources.
func (s *Store) AllIssues(ctx context.Context) ([]issues.Issue, error) {
	const q = `
SELECT source, identifier, title, url, assignee, team_key, team_name,
       project_id, project_name, milestone_id, milestone_name,
       issue_type, parent_identifier, status, status_name, resolution,
       created_at, started_at, completed_at, canceled_at, updated_at
FROM issues`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("AllIssues: %w", err)
	}
	defer rows.Close()

	var out []issues.Issue
	for rows.Next() {
		var it issues.Issue
		var status string
		var url, assignee, projectID, projectName, milestoneID, milestoneName, issueType, parent, resolution sql.NullString
		var createdAt, startedAt, completedAt, canceledAt, updatedAt sql.NullTime
		if err := rows.Scan(
			&it.Source, &it.Identifier, &it.Title, &url, &assignee, &it.TeamKey, &it.TeamName,
			&projectID, &projectName, &milestoneID, &milestoneName,
			&issueType, &parent, &status, &it.StatusName, &resolution,
			&createdAt, &startedAt, &completedAt, &canceledAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("AllIssues scan: %w", err)
		}
		it.Status, _ = issues.ParseStatus(status)
		it.URL = url.String
		it.Assignee = assignee.String
		it.ProjectID = projectID.String
		it.ProjectName = projectName.String
		it.MilestoneID = milestoneID.String
		it.MilestoneName = milestoneName.String
		it.IssueType = issueType.String
		it.ParentIdentifier = parent.String
		it.Resolution = resolution.String
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
		if updatedAt.Valid {
			it.UpdatedAt = updatedAt.Time
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// LatestUpdatedAt returns the maximum updated_at among source's issues for
// the given team key. Returns zero time if that team has no issues from that
// source yet (signals a full fetch for that team).
func (s *Store) LatestUpdatedAt(ctx context.Context, source, teamKey string) (time.Time, error) {
	// Selecting the updated_at column directly (rather than MAX(updated_at))
	// keeps the result typed as DATETIME, which the sqlite driver requires
	// in order to scan it back into a time.Time instead of a string.
	row := s.db.QueryRowContext(ctx,
		`SELECT updated_at FROM issues WHERE source = ? AND team_key = ? ORDER BY updated_at DESC LIMIT 1`,
		source, teamKey)

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

// DistinctTeams returns every non-empty team_key source currently has in the
// store, ordered alphabetically.
func (s *Store) DistinctTeams(ctx context.Context, source string) ([]string, error) {
	return s.queryStrings(ctx, "distinct team keys",
		`SELECT DISTINCT team_key FROM issues WHERE source = ? AND team_key <> '' ORDER BY team_key`, source)
}

// Identifiers returns the identifiers of source's issues for one team,
// ordered alphabetically.
func (s *Store) Identifiers(ctx context.Context, source, teamKey string) ([]string, error) {
	return s.queryStrings(ctx, "identifiers",
		`SELECT identifier FROM issues WHERE source = ? AND team_key = ? ORDER BY identifier`, source, teamKey)
}

// queryStrings runs a one-column query and collects its values.
func (s *Store) queryStrings(ctx context.Context, what, q string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("%s scan: %w", what, err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// nullTime converts a time.Time to sql.NullTime, treating zero as NULL.
func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

// nullString converts a string to sql.NullString, treating "" as NULL. Used
// for the optional columns (url, assignee, project, milestone, issue type,
// parent, resolution) so that absent data is stored as NULL rather than an
// empty string.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
