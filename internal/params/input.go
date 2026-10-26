package params

import (
	"flag"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// Input is the -input / -db / -stdin-format block. DB is nil for commands
// that never had the deprecated -db alias (history, check).
type Input struct {
	Path        *string
	DB          *string
	StdinFormat *string
}

// AddInput registers -input and -stdin-format, plus -db when withDB is set,
// with the CLI's existing usage strings.
func AddInput(fs *flag.FlagSet, withDB bool) Input {
	var in Input
	if withDB {
		in.DB = fs.String("db", "", "path to SQLite database")
	}
	in.Path = fs.String("input", "", "path to a SQLite database (.db), CSV, or JSON file; \"-\" reads stdin (requires -stdin-format)")
	in.StdinFormat = fs.String("stdin-format", "", `format of -input when reading stdin ("-"): "csv" or "json"`)
	return in
}

// Resolve returns -input, or the deprecated -db with the warning
// "-db is deprecated; use -input instead". With neither set it returns
// &app.FieldError{Fields: []string{"input"}, Msg: "-input is required"}.
func (in Input) Resolve() (path string, warnings []string, err error) {
	if *in.Path != "" {
		return *in.Path, nil, nil
	}
	if in.DB != nil && *in.DB != "" {
		return *in.DB, []string{"-db is deprecated; use -input instead"}, nil
	}
	return "", nil, &app.FieldError{Fields: []string{"input"}, Msg: "-input is required"}
}
