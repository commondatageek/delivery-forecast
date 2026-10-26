// Package source loads issue sets and exclusions files for the forecast CLI
// (and, later, the web server). It is the IO boundary in front of
// internal/app, which is pure.
package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/commondatageek/delivery-forecast/internal/importmap"
	"github.com/commondatageek/delivery-forecast/internal/sqlite"
	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

// Load reads every issue from path, dispatching on file extension:
// .db/.sqlite/.sqlite3 open the SQLite store; everything else goes to
// issues.ReadFile. Returns the issues unfiltered; apply issues.Filter after.
//
// A path of "-" reads stdin, which has no extension to dispatch on and so
// requires format ("csv" or "json") — the value of the command's
// -stdin-format flag. Every command routes its source through here, so
// -input - works uniformly wherever -input is accepted. stdin is passed in
// rather than read from os.Stdin directly so callers and tests control it.
func Load(ctx context.Context, path, format string, stdin io.Reader) ([]issues.Issue, error) {
	if path == "-" {
		if format == "" {
			return nil, fmt.Errorf(`-stdin-format is required when reading from stdin (-input -): "csv" or "json"`)
		}
		return issues.ReadStream(stdin, format)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".db", ".sqlite", ".sqlite3":
		store, err := sqlite.OpenExisting(path)
		if err != nil {
			return nil, fmt.Errorf("open db: %w", err)
		}
		defer store.Close()

		all, err := store.AllIssues(ctx)
		if err != nil {
			return nil, fmt.Errorf("query issues: %w", err)
		}
		return all, nil
	default:
		items, err := issues.ReadFile(path)
		var he *issues.HeaderError
		if errors.As(err, &he) {
			return nil, headerHint(path, he)
		}
		return items, err
	}
}

// headerHint explains a CSV header that isn't a forecast data file. When it
// looks like a tracker export one of importmap's templates knows, the error
// says how to convert it with forecast import.
func headerHint(path string, he *issues.HeaderError) error {
	name, ok := importmap.Suggest(he.Header)
	if !ok {
		return fmt.Errorf("%s: %s", path, he.Msg)
	}
	return fmt.Errorf("%s: this looks like a %s (template %q), which needs converting first:\n"+
		"  forecast import -example %s > mapping.yaml\n"+
		"  forecast import -from %s -map mapping.yaml -out data.db",
		path, importmap.Describe(name), name, name, shellQuote(path))
}

// shellQuote single-quotes s for a POSIX shell when it holds anything but
// plainly safe characters, so a suggested command can be pasted as-is.
func shellQuote(s string) string {
	safe := s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:@%+=,", r))
	}) < 0
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// LoadExclusions reads and parses an exclusions JSON file. An empty path means
// no exclusions were requested and yields an empty Exclusions; a path that was
// given but cannot be read is an error, never silently treated as empty.
func LoadExclusions(path string) (simulate.Exclusions, error) {
	if path == "" {
		return simulate.Exclusions{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return simulate.Exclusions{}, fmt.Errorf("reading exclusions file %q: %w", path, err)
	}
	return simulate.ParseExclusions(data)
}

// ReadExclusions reads and parses an exclusions file, returning the read or
// parse error unwrapped. Unlike LoadExclusions it is meant for `check`,
// which reports the error as a finding rather than failing.
func ReadExclusions(path string) (simulate.Exclusions, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return simulate.Exclusions{}, err
	}
	return simulate.ParseExclusions(data)
}
