package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/commondatageek/delivery-forecast/internal/logx"
	"github.com/commondatageek/delivery-forecast/internal/sqlite"
	"github.com/commondatageek/delivery-forecast/issues"
)

// loadIssues reads every issue from path, dispatching on file extension:
// .db/.sqlite/.sqlite3 open the SQLite store; everything else goes to
// issues.ReadFile. Returns the issues unfiltered; apply issues.Filter after.
//
// A path of "-" reads stdin, which has no extension to dispatch on and so
// requires format ("csv" or "json") — the value of the command's
// -stdin-format flag. Every command routes its source through here, so
// -input - works uniformly wherever -input is accepted.
func loadIssues(ctx context.Context, path, format string) ([]issues.Issue, error) {
	if path == "-" {
		if format == "" {
			return nil, fmt.Errorf(`-stdin-format is required when reading from stdin (-input -): "csv" or "json"`)
		}
		return issues.ReadStream(os.Stdin, format)
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
		return issues.ReadFile(path)
	}
}

// addInputFlag registers -input, the source-agnostic replacement for -db.
func addInputFlag(fs *flag.FlagSet) *string {
	return fs.String("input", "", "path to a SQLite database (.db), CSV, or JSON file; \"-\" reads stdin (requires -stdin-format)")
}

// addStdinFormatFlag registers -stdin-format, which names the format of
// -input when it is "-" (stdin). It pairs with addInputFlag: every command
// that offers one offers the other, so -input's help text is true everywhere.
func addStdinFormatFlag(fs *flag.FlagSet) *string {
	return fs.String("stdin-format", "", `format of -input when reading stdin ("-"): "csv" or "json"`)
}

// resolveInput returns the input path, preferring -input and falling back to
// -db (deprecated). Errors if neither is set; warns if -db was used.
func resolveInput(fs *flag.FlagSet, input, db *string) (string, error) {
	if *input != "" {
		return *input, nil
	}
	if *db != "" {
		logx.Warnf("-db is deprecated; use -input instead")
		return *db, nil
	}
	return "", fmt.Errorf("-input is required")
}

// distinctTeamKeys returns the sorted, deduplicated, non-empty team keys
// present in items — the in-memory equivalent of Store.DistinctTeamKeys, so
// the blending-teams warning works identically for every source.
func distinctTeamKeys(items []issues.Issue) []string {
	seen := make(map[string]bool)
	var keys []string
	for _, it := range items {
		if it.TeamKey == "" || seen[it.TeamKey] {
			continue
		}
		seen[it.TeamKey] = true
		keys = append(keys, it.TeamKey)
	}
	sort.Strings(keys)
	return keys
}
