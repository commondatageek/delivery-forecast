package source

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/importmap"
	"github.com/commondatageek/delivery-forecast/internal/sqlite"
	"github.com/commondatageek/delivery-forecast/issues"
)

// ImportResult is what `forecast import` did.
type ImportResult struct {
	Issues  []issues.Issue
	Report  importmap.Report
	Deleted int  // rows replaced in a .db; 0 otherwise
	Wrote   bool // false for a dry run
}

// Import reads and parses the mapping, reads the export, applies the
// mapping and, unless dryRun, writes out. Paths are used as given. Nothing
// is written if any earlier step fails; the result is still filled as far
// as the steps got. Errors are *app.FieldError naming the flag to blame.
func Import(ctx context.Context, from, mapPath, out string, dryRun bool) (ImportResult, error) {
	var res ImportResult
	fail := func(field, prefix string, err error) (ImportResult, error) {
		return res, &app.FieldError{Fields: []string{field}, Msg: prefix + err.Error()}
	}

	data, err := os.ReadFile(mapPath)
	if err != nil {
		return fail("map", "-map ", err)
	}
	m, err := importmap.Parse(data)
	if err != nil {
		return fail("map", "-map "+mapPath+": ", err)
	}

	f, err := os.Open(from)
	if err != nil {
		return fail("from", "-from ", err)
	}
	header, rows, err := importmap.ReadCSV(f)
	f.Close()
	if err != nil {
		return fail("from", "-from "+from+": ", err)
	}

	res.Issues, res.Report, err = importmap.Apply(m, header, rows)
	if err != nil {
		return fail("from", "-from "+from+": ", err)
	}
	if dryRun {
		return res, nil
	}

	switch strings.ToLower(filepath.Ext(out)) {
	case ".db", ".sqlite", ".sqlite3":
		store, err := sqlite.Open(out)
		if err != nil {
			return fail("out", "-out "+out+": ", err)
		}
		res.Deleted, err = store.ReplaceSource(ctx, m.Source, res.Issues)
		store.Close()
		if err != nil {
			return fail("out", "-out "+out+": ", err)
		}
	case ".csv":
		err = writeAtomically(out, func(w io.Writer) error { return issues.WriteCSV(w, res.Issues) })
	case ".json", ".jsonl", ".ndjson":
		err = writeAtomically(out, func(w io.Writer) error { return issues.WriteJSON(w, res.Issues) })
	default:
		err = fmt.Errorf("unrecognized extension %q (use .db, .csv, .json or .jsonl)", filepath.Ext(out))
	}
	if err != nil {
		return fail("out", "-out "+out+": ", err)
	}
	res.Wrote = true
	return res, nil
}

// writeAtomically writes path through a temp file in the same directory,
// renamed over path only once the whole file is written, so a failed import
// never leaves a half-written data file behind.
func writeAtomically(path string, write func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	// CreateTemp makes the file owner-only; a data file is readable like any
	// other the user writes.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
