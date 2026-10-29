package params

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// ImportRequest is the resolved form of `forecast import`'s flags.
type ImportRequest struct {
	From, Map, Out string
	DryRun         bool
	Example        string
}

// Import holds the flags of `forecast import`. It reads a raw tracker export
// (-from, never -input: a raw export is not a data source) and writes a
// normalized one (-out).
type Import struct {
	From, Map, Out, Example *string
	DryRun                  *bool
	Config                  *string
}

// NewImport registers the `import` flags on fs.
func NewImport(fs *flag.FlagSet) *Import {
	c := &Import{}
	c.From = fs.String("from", "", "path to the tracker's CSV export to convert")
	c.Map = fs.String("map", "", "path to the mapping YAML file (start from -example)")
	c.Out = fs.String("out", "", "where to write the converted data: .db (replaces this mapping's source), .csv or .json")
	c.DryRun = fs.Bool("dry-run", false, "report what would be imported without writing anything")
	c.Example = fs.String("example", "", `print an embedded mapping template ("list" names them) and exit`)
	c.Config = AddConfig(fs)
	return c
}

// ImportOutExt reports whether path's extension names a writer import knows.
func ImportOutExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".db", ".sqlite", ".sqlite3", ".csv", ".json", ".jsonl", ".ndjson":
		return true
	}
	return false
}

// Resolve validates the flag combination; InputPath is -from. With -example
// nothing else may be given (the command prints a template and exits).
func (c *Import) Resolve() (Resolved[ImportRequest], error) {
	var r Resolved[ImportRequest]
	r.Request = ImportRequest{From: *c.From, Map: *c.Map, Out: *c.Out, DryRun: *c.DryRun, Example: *c.Example}
	r.InputPath = *c.From
	req := r.Request

	if req.Example != "" {
		var others []string
		for _, f := range []struct {
			name string
			set  bool
		}{{"from", req.From != ""}, {"map", req.Map != ""}, {"out", req.Out != ""}, {"dry-run", req.DryRun}} {
			if f.set {
				others = append(others, f.name)
			}
		}
		if len(others) > 0 {
			return r, &app.FieldError{Fields: append([]string{"example"}, others...), Msg: "-example prints a template and exits; it can't be combined with -" + strings.Join(others, ", -")}
		}
		return r, nil
	}

	switch {
	case req.From == "":
		return r, &app.FieldError{Fields: []string{"from"}, Msg: "-from is required: the tracker's CSV export"}
	case req.From == "-":
		return r, &app.FieldError{Fields: []string{"from"}, Msg: "-from: stdin is not supported; pass the export file"}
	case req.Map == "":
		return r, &app.FieldError{Fields: []string{"map"}, Msg: "-map is required (start from forecast import -example list)"}
	case req.Out == "" && !req.DryRun:
		return r, &app.FieldError{Fields: []string{"out", "dry-run"}, Msg: "-out is required unless -dry-run is given"}
	case req.Out != "" && !ImportOutExt(req.Out):
		return r, &app.FieldError{Fields: []string{"out"}, Msg: fmt.Sprintf("-out: unrecognized extension %q (use .db, .csv, .json or .jsonl)", filepath.Ext(req.Out))}
	}
	return r, nil
}
