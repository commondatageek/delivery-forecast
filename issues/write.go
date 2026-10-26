package issues

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"
)

// Columns returns the recognized column names in schema order — the header
// WriteCSV writes and the keys WriteJSON emits.
func Columns() []string {
	return slices.Clone(columnNames)
}

// formatTimestamp renders t for a written file: RFC3339 (seconds precision is
// all any source has), or "" for the zero time.
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// fields returns its values in Columns() order, timestamps formatted by
// formatTimestamp.
func fields(it Issue) []string {
	return []string{
		it.Source, it.Identifier, it.Title, it.URL, it.Assignee, it.TeamKey, it.TeamName,
		it.ProjectID, it.ProjectName, it.MilestoneID, it.MilestoneName,
		it.IssueType, it.ParentIdentifier, string(it.Status), it.StatusName, it.Resolution,
		formatTimestamp(it.CreatedAt), formatTimestamp(it.StartedAt), formatTimestamp(it.CompletedAt),
		formatTimestamp(it.CanceledAt), formatTimestamp(it.UpdatedAt),
	}
}

// WriteCSV writes items as CSV: a Columns() header, then one row per issue.
// ReadCSV reads the result back into equal issues (given timestamps without
// sub-second parts).
func WriteCSV(w io.Writer, items []Issue) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(columnNames); err != nil {
		return err
	}
	for _, it := range items {
		if err := cw.Write(fields(it)); err != nil {
			return fmt.Errorf("write %s: %w", it.Identifier, err)
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteJSON writes items as JSON Lines: one object per issue, keyed by
// Columns(), with zero timestamps as null. ReadJSON reads the result back
// into equal issues (given timestamps without sub-second parts).
func WriteJSON(w io.Writer, items []Issue) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, it := range items {
		// json.Marshal sorts map keys, so build the object as an ordered
		// sequence of raw members instead to keep Columns() order.
		vals := fields(it)
		buf := []byte{'{'}
		for i, name := range columnNames {
			if i > 0 {
				buf = append(buf, ',')
			}
			key, _ := json.Marshal(name)
			buf = append(buf, key...)
			buf = append(buf, ':')
			if vals[i] == "" && isTimestampColumn(name) {
				buf = append(buf, "null"...)
				continue
			}
			v, err := json.Marshal(vals[i])
			if err != nil {
				return fmt.Errorf("write %s: %w", it.Identifier, err)
			}
			buf = append(buf, v...)
		}
		buf = append(buf, '}')
		if err := enc.Encode(json.RawMessage(buf)); err != nil {
			return fmt.Errorf("write %s: %w", it.Identifier, err)
		}
	}
	return nil
}

// isTimestampColumn reports whether a Columns() name holds a timestamp.
func isTimestampColumn(name string) bool {
	switch name {
	case "created_at", "started_at", "completed_at", "canceled_at", "updated_at":
		return true
	}
	return false
}
