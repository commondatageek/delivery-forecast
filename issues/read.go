package issues

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
)

// columnNames lists the recognized CSV/JSON column names, in the order
// documented in DATA_REQUIREMENTS.md's schema table.
var columnNames = []string{
	"identifier", "title", "assignee", "team_key", "team_name",
	"project_id", "project_name", "project_milestone_id", "project_milestone_name",
	"state_type", "state_name",
	"created_at", "started_at", "completed_at", "canceled_at",
	"archived_at", "auto_archived_at", "added_to_project_at", "updated_at",
}

// fieldSetters assigns a parsed column value onto an Issue.
var fieldSetters = map[string]func(*Issue, string) error{
	"identifier":             func(i *Issue, v string) error { i.Identifier = v; return nil },
	"title":                  func(i *Issue, v string) error { i.Title = v; return nil },
	"assignee":               func(i *Issue, v string) error { i.Assignee = v; return nil },
	"team_key":               func(i *Issue, v string) error { i.TeamKey = v; return nil },
	"team_name":              func(i *Issue, v string) error { i.TeamName = v; return nil },
	"project_id":             func(i *Issue, v string) error { i.ProjectID = v; return nil },
	"project_name":           func(i *Issue, v string) error { i.ProjectName = v; return nil },
	"project_milestone_id":   func(i *Issue, v string) error { i.ProjectMilestoneID = v; return nil },
	"project_milestone_name": func(i *Issue, v string) error { i.ProjectMilestoneName = v; return nil },
	"state_type":             func(i *Issue, v string) error { i.StateType = v; return nil },
	"state_name":             func(i *Issue, v string) error { i.StateName = v; return nil },
	"created_at":             func(i *Issue, v string) error { return setTimestamp(&i.CreatedAt, v) },
	"started_at":             func(i *Issue, v string) error { return setTimestamp(&i.StartedAt, v) },
	"completed_at":           func(i *Issue, v string) error { return setTimestamp(&i.CompletedAt, v) },
	"canceled_at":            func(i *Issue, v string) error { return setTimestamp(&i.CanceledAt, v) },
	"archived_at":            func(i *Issue, v string) error { return setTimestamp(&i.ArchivedAt, v) },
	"auto_archived_at":       func(i *Issue, v string) error { return setTimestamp(&i.AutoArchivedAt, v) },
	"added_to_project_at":    func(i *Issue, v string) error { return setTimestamp(&i.AddedToProjectAt, v) },
	"updated_at":             func(i *Issue, v string) error { return setTimestamp(&i.UpdatedAt, v) },
}

func setTimestamp(dst *time.Time, v string) error {
	t, err := parseTimestamp(v)
	if err != nil {
		return err
	}
	*dst = t
	return nil
}

// parseTimestamp parses a timestamp value, trying formats in order: RFC3339
// (with or without nanoseconds and offset), "2006-01-02T15:04:05" (local),
// "2006-01-02 15:04:05" (local), and "2006-01-02" (local midnight). An empty
// string or the literal "null" parses to the zero time.
func parseTimestamp(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "null") {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", v, time.Local); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", v, time.Local); err == nil {
		return t, nil
	}
	if t, err := util.ParseDate(v); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", v)
}

// columnIndex maps recognized header names (case-insensitive, trimmed) to
// their column position. Unrecognized headers are ignored. Returns an error
// if no header cell is recognized at all.
func columnIndex(header []string) (map[string]int, error) {
	idx := make(map[string]int)
	for i, h := range header {
		name := strings.ToLower(strings.TrimSpace(h))
		if _, ok := fieldSetters[name]; ok {
			idx[name] = i
		}
	}
	if len(idx) == 0 {
		return nil, fmt.Errorf("no recognized columns in header; expected one or more of: %s", strings.Join(columnNames, ", "))
	}
	return idx, nil
}

// ReadCSV parses issues from CSV. The first row must be a header naming
// columns; column order is irrelevant and unrecognized columns are ignored.
func ReadCSV(r io.Reader) ([]Issue, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("empty input: no header row")
	}
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	idx, err := columnIndex(header)
	if err != nil {
		return nil, err
	}

	var out []Issue
	rowNum := 1
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", rowNum, err)
		}
		var it Issue
		for name, col := range idx {
			if col >= len(record) {
				continue
			}
			if err := fieldSetters[name](&it, record[col]); err != nil {
				return nil, fmt.Errorf("row %d, column %q: %w", rowNum, name, err)
			}
		}
		out = append(out, it)
	}
	return out, nil
}

// jsonIssue mirrors Issue but with the timestamp columns typed as raw JSON so
// they can be parsed with the same flexible formats ReadCSV accepts, rather
// than encoding/json's default (RFC3339-only) time.Time unmarshaling.
type jsonIssue struct {
	Identifier           string          `json:"identifier"`
	Title                string          `json:"title"`
	Assignee             string          `json:"assignee"`
	TeamKey              string          `json:"team_key"`
	TeamName             string          `json:"team_name"`
	ProjectID            string          `json:"project_id"`
	ProjectName          string          `json:"project_name"`
	ProjectMilestoneID   string          `json:"project_milestone_id"`
	ProjectMilestoneName string          `json:"project_milestone_name"`
	StateType            string          `json:"state_type"`
	StateName            string          `json:"state_name"`
	CreatedAt            json.RawMessage `json:"created_at"`
	StartedAt            json.RawMessage `json:"started_at"`
	CompletedAt          json.RawMessage `json:"completed_at"`
	CanceledAt           json.RawMessage `json:"canceled_at"`
	ArchivedAt           json.RawMessage `json:"archived_at"`
	AutoArchivedAt       json.RawMessage `json:"auto_archived_at"`
	AddedToProjectAt     json.RawMessage `json:"added_to_project_at"`
	UpdatedAt            json.RawMessage `json:"updated_at"`
}

func rawTimestamp(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return time.Time{}, fmt.Errorf("expected a string timestamp, got %s", string(raw))
	}
	return parseTimestamp(s)
}

// decodeJSONIssue converts data (one JSON object) into an Issue, parsing its
// timestamp fields with the same flexible formats ReadCSV accepts.
func decodeJSONIssue(data []byte) (Issue, error) {
	var raw jsonIssue
	if err := json.Unmarshal(data, &raw); err != nil {
		return Issue{}, err
	}
	it := Issue{
		Identifier:           raw.Identifier,
		Title:                raw.Title,
		Assignee:             raw.Assignee,
		TeamKey:              raw.TeamKey,
		TeamName:             raw.TeamName,
		ProjectID:            raw.ProjectID,
		ProjectName:          raw.ProjectName,
		ProjectMilestoneID:   raw.ProjectMilestoneID,
		ProjectMilestoneName: raw.ProjectMilestoneName,
		StateType:            raw.StateType,
		StateName:            raw.StateName,
	}
	fields := []struct {
		raw string
		dst *time.Time
	}{
		{"created_at", &it.CreatedAt}, {"started_at", &it.StartedAt},
		{"completed_at", &it.CompletedAt}, {"canceled_at", &it.CanceledAt},
		{"archived_at", &it.ArchivedAt}, {"auto_archived_at", &it.AutoArchivedAt},
		{"added_to_project_at", &it.AddedToProjectAt}, {"updated_at", &it.UpdatedAt},
	}
	rawByName := map[string]json.RawMessage{
		"created_at": raw.CreatedAt, "started_at": raw.StartedAt,
		"completed_at": raw.CompletedAt, "canceled_at": raw.CanceledAt,
		"archived_at": raw.ArchivedAt, "auto_archived_at": raw.AutoArchivedAt,
		"added_to_project_at": raw.AddedToProjectAt, "updated_at": raw.UpdatedAt,
	}
	for _, f := range fields {
		t, err := rawTimestamp(rawByName[f.raw])
		if err != nil {
			return Issue{}, fmt.Errorf("column %q: %w", f.raw, err)
		}
		*f.dst = t
	}
	return it, nil
}

// ReadJSON parses issues from either a top-level JSON array of objects or
// JSON Lines (one object per line). The form is detected from the first
// non-whitespace byte: '[' means array, anything else means JSON Lines.
func ReadJSON(r io.Reader) ([]Issue, error) {
	br := bufio.NewReader(r)
	first, err := peekFirstNonSpace(br)
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if first == '[' {
		var raws []json.RawMessage
		if err := json.NewDecoder(br).Decode(&raws); err != nil {
			return nil, fmt.Errorf("parsing json array: %w", err)
		}
		out := make([]Issue, len(raws))
		for i, raw := range raws {
			it, err := decodeJSONIssue(raw)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i+1, err)
			}
			out[i] = it
		}
		return out, nil
	}

	var out []Issue
	scanner := bufio.NewScanner(br)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		it, err := decodeJSONIssue([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		out = append(out, it)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading json lines: %w", err)
	}
	return out, nil
}

// peekFirstNonSpace returns the first non-whitespace byte in r without
// consuming anything before it.
func peekFirstNonSpace(r *bufio.Reader) (byte, error) {
	for i := 0; ; i++ {
		b, err := r.Peek(i + 1)
		if err != nil {
			return 0, err
		}
		c := b[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		return c, nil
	}
}

// ReadFile reads issues from path, choosing the parser by file extension:
// .csv -> ReadCSV; .json, .jsonl, .ndjson -> ReadJSON. A path of "-" reads
// stdin, which requires an explicit format (see ReadStream).
func ReadFile(path string) ([]Issue, error) {
	if path == "-" {
		return nil, fmt.Errorf(`reading "-" (stdin) requires an explicit format; use ReadStream instead`)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".csv":
		return ReadCSV(f)
	case ".json", ".jsonl", ".ndjson":
		return ReadJSON(f)
	default:
		return nil, fmt.Errorf("unrecognized file extension %q for %s (expected .csv, .json, .jsonl, or .ndjson)", ext, path)
	}
}

// ReadStream reads issues from r using the named format ("csv" or "json").
func ReadStream(r io.Reader, format string) ([]Issue, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "csv":
		return ReadCSV(r)
	case "json":
		return ReadJSON(r)
	default:
		return nil, fmt.Errorf("unrecognized format %q (use csv or json)", format)
	}
}
