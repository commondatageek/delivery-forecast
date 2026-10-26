package report

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/importmap"
)

// importWrap is the column the fields list wraps at.
const importWrap = 76

// Import writes the `forecast import` report: what was read, excluded and
// imported, how statuses were decided, where timestamps came from, how full
// the other fields are, the same readiness block `check` prints, and what
// was written. from and out are the paths as typed.
func Import(w io.Writer, from, out string, dryRun bool, deleted int, r importmap.Report, readiness []app.CheckResult) {
	head := []int{r.Rows, r.Imported}
	for _, e := range r.Excluded {
		head = append(head, e.N)
	}
	hw := countWidth(head...)
	fmt.Fprintf(w, "%-8s %*d rows, %d columns from %s\n", "read", hw, r.Rows, r.Columns, from)
	for _, e := range r.Excluded {
		fmt.Fprintf(w, "%-8s %*d %s\n", "excluded", hw, e.N, e.Label)
	}
	fmt.Fprintf(w, "%-8s %*d issues (source %q)\n", "imported", hw, r.Imported, r.Source)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "status")
	var sn []int
	for _, s := range r.Status {
		sn = append(sn, s.N)
	}
	sw := countWidth(sn...)
	for _, s := range r.Status {
		writeCounted(w, s.Status, sw, s.N, s.Via)
	}

	if len(r.Timestamps) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "timestamps")
		var tn []int
		for _, t := range r.Timestamps {
			tn = append(tn, t.Set)
		}
		tw := countWidth(tn...)
		for _, t := range r.Timestamps {
			writeCounted(w, t.Target, tw, t.Set, t.From)
		}
	}

	var fields []string
	for _, f := range r.Fields {
		if f.Target != "identifier" { // always full: an empty one fails the import
			fields = append(fields, fmt.Sprintf("%s %d", f.Target, f.Filled))
		}
	}
	if len(fields) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "fields (non-empty of %d)\n", r.Imported)
		line := " "
		for i, f := range fields {
			sep := " "
			if i < len(fields)-1 {
				f += ","
			}
			if len(line)+len(sep)+len(f) > importWrap && line != " " {
				fmt.Fprintln(w, line)
				line = " "
			}
			line += sep + f
		}
		fmt.Fprintln(w, line)
	}

	fmt.Fprintln(w)
	Check(w, r.Imported, "the imported set", readiness)

	fmt.Fprintln(w)
	switch {
	case dryRun:
		fmt.Fprintln(w, "dry run: nothing written")
	case isDB(out):
		fmt.Fprintf(w, "wrote %d issues to %s (replaced %d issues from source %q)\n", r.Imported, out, deleted, r.Source)
	default:
		fmt.Fprintf(w, "wrote %d issues to %s\n", r.Imported, out)
	}
}

// writeCounted writes one "  <label>  <n>  <a> 1, <b> 2" row.
func writeCounted(w io.Writer, label string, width, n int, parts []importmap.ValueCount) {
	line := fmt.Sprintf("  %-12s%*d", label, width, n)
	if len(parts) > 0 {
		s := make([]string, len(parts))
		for i, p := range parts {
			s[i] = fmt.Sprintf("%s %d", p.Label, p.N)
		}
		line += "  " + strings.Join(s, ", ")
	}
	fmt.Fprintln(w, line)
}

// countWidth is the column width for right-aligned counts: one wider than
// the longest, so a count never touches the label before it.
func countWidth(ns ...int) int {
	w := 1
	for _, n := range ns {
		if d := len(fmt.Sprint(n)); d > w {
			w = d
		}
	}
	return w + 1
}

func isDB(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".db", ".sqlite", ".sqlite3":
		return true
	}
	return false
}
