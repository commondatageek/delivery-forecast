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
	"source", "identifier", "title", "url", "assignee", "team_key", "team_name",
	"project_id", "project_name", "milestone_id", "milestone_name",
	"issue_type", "parent_identifier", "status", "status_name", "resolution",
	"created_at", "started_at", "completed_at", "canceled_at", "updated_at",
}

// fieldSetters assigns a parsed column value onto an Issue.
var fieldSetters = map[string]func(*Issue, string) error{
	"source":            func(i *Issue, v string) error { i.Source = v; return nil },
	"identifier":        func(i *Issue, v string) error { i.Identifier = v; return nil },
	"title":             func(i *Issue, v string) error { i.Title = v; return nil },
	"url":               func(i *Issue, v string) error { i.URL = v; return nil },
	"assignee":          func(i *Issue, v string) error { i.Assignee = v; return nil },
	"team_key":          func(i *Issue, v string) error { i.TeamKey = v; return nil },
	"team_name":         func(i *Issue, v string) error { i.TeamName = v; return nil },
	"project_id":        func(i *Issue, v string) error { i.ProjectID = v; return nil },
	"project_name":      func(i *Issue, v string) error { i.ProjectName = v; return nil },
	"milestone_id":      func(i *Issue, v string) error { i.MilestoneID = v; return nil },
	"milestone_name":    func(i *Issue, v string) error { i.MilestoneName = v; return nil },
	"issue_type":        func(i *Issue, v string) error { i.IssueType = v; return nil },
	"parent_identifier": func(i *Issue, v string) error { i.ParentIdentifier = v; return nil },
	"status":            func(i *Issue, v string) error { i.Status, _ = ParseStatus(v); return nil },
	"status_name":       func(i *Issue, v string) error { i.StatusName = v; return nil },
	"resolution":        func(i *Issue, v string) error { i.Resolution = v; return nil },
	"created_at":        func(i *Issue, v string) error { return setTimestamp(&i.CreatedAt, v) },
	"started_at":        func(i *Issue, v string) error { return setTimestamp(&i.StartedAt, v) },
	"completed_at":      func(i *Issue, v string) error { return setTimestamp(&i.CompletedAt, v) },
	"canceled_at":       func(i *Issue, v string) error { return setTimestamp(&i.CanceledAt, v) },
	"updated_at":        func(i *Issue, v string) error { return setTimestamp(&i.UpdatedAt, v) },
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

// HeaderError reports a CSV header that can't be a forecast data file.
type HeaderError struct {
	Header []string // the header row as read (BOM stripped)
	Msg    string
}

func (e *HeaderError) Error() string { return e.Msg }

// bom is the UTF-8 byte order mark some exporters (Excel, Jira) write before
// the first header cell.
const bom = "\uFEFF"

// columnIndex maps recognized header names (case-insensitive, trimmed) to
// their column position. Unrecognized headers are ignored. A header with no
// recognized cell, or without an identifier column, is a *HeaderError; a
// recognized column named twice is an error too, since there is no telling
// which one is meant.
func columnIndex(header []string) (map[string]int, error) {
	idx := make(map[string]int)
	seen := make(map[string]int)
	var repeated []string
	for i, h := range header {
		name := strings.ToLower(strings.TrimSpace(h))
		if _, ok := fieldSetters[name]; !ok {
			continue
		}
		seen[name]++
		if seen[name] == 2 {
			repeated = append(repeated, name)
		}
		if _, ok := idx[name]; !ok {
			idx[name] = i
		}
	}
	if len(idx) == 0 {
		return nil, &HeaderError{Header: header, Msg: fmt.Sprintf("no recognized columns in header; expected one or more of: %s", strings.Join(columnNames, ", "))}
	}
	if _, ok := idx["identifier"]; !ok {
		return nil, &HeaderError{Header: header, Msg: `no "identifier" column; expected the forecast column names (see DATA_REQUIREMENTS.md)`}
	}
	if len(repeated) > 0 {
		return nil, fmt.Errorf("column %q appears %d times", repeated[0], seen[repeated[0]])
	}
	return idx, nil
}

// ReadCSV parses issues from CSV. The first row must be a header naming
// columns, including identifier; column order is irrelevant, unrecognized
// columns are ignored, and a leading UTF-8 byte order mark is skipped.
func ReadCSV(r io.Reader) ([]Issue, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("empty input: no header row")
	}
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], bom)
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
	Source           string          `json:"source"`
	Identifier       string          `json:"identifier"`
	Title            string          `json:"title"`
	URL              string          `json:"url"`
	Assignee         string          `json:"assignee"`
	TeamKey          string          `json:"team_key"`
	TeamName         string          `json:"team_name"`
	ProjectID        string          `json:"project_id"`
	ProjectName      string          `json:"project_name"`
	MilestoneID      string          `json:"milestone_id"`
	MilestoneName    string          `json:"milestone_name"`
	IssueType        string          `json:"issue_type"`
	ParentIdentifier string          `json:"parent_identifier"`
	Status           string          `json:"status"`
	StatusName       string          `json:"status_name"`
	Resolution       string          `json:"resolution"`
	CreatedAt        json.RawMessage `json:"created_at"`
	StartedAt        json.RawMessage `json:"started_at"`
	CompletedAt      json.RawMessage `json:"completed_at"`
	CanceledAt       json.RawMessage `json:"canceled_at"`
	UpdatedAt        json.RawMessage `json:"updated_at"`
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
		Source:           raw.Source,
		Identifier:       raw.Identifier,
		Title:            raw.Title,
		URL:              raw.URL,
		Assignee:         raw.Assignee,
		TeamKey:          raw.TeamKey,
		TeamName:         raw.TeamName,
		ProjectID:        raw.ProjectID,
		ProjectName:      raw.ProjectName,
		MilestoneID:      raw.MilestoneID,
		MilestoneName:    raw.MilestoneName,
		IssueType:        raw.IssueType,
		ParentIdentifier: raw.ParentIdentifier,
		StatusName:       raw.StatusName,
		Resolution:       raw.Resolution,
	}
	it.Status, _ = ParseStatus(raw.Status)
	fields := []struct {
		raw json.RawMessage
		col string
		dst *time.Time
	}{
		{raw.CreatedAt, "created_at", &it.CreatedAt},
		{raw.StartedAt, "started_at", &it.StartedAt},
		{raw.CompletedAt, "completed_at", &it.CompletedAt},
		{raw.CanceledAt, "canceled_at", &it.CanceledAt},
		{raw.UpdatedAt, "updated_at", &it.UpdatedAt},
	}
	for _, f := range fields {
		t, err := rawTimestamp(f.raw)
		if err != nil {
			return Issue{}, fmt.Errorf("column %q: %w", f.col, err)
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
