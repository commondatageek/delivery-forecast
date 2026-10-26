package main

import (
	"context"
	"os"

	"github.com/commondatageek/delivery-forecast/internal/source"
	"github.com/commondatageek/delivery-forecast/issues"
)

// loadIssues reads every issue from path, dispatching on file extension:
// .db/.sqlite/.sqlite3 open the SQLite store; everything else goes to
// issues.ReadFile. Returns the issues unfiltered; apply a Filter after.
//
// A path of "-" reads stdin, which has no extension to dispatch on and so
// requires format ("csv" or "json") — the value of the command's
// -stdin-format flag. Every command routes its source through here, so
// -input - works uniformly wherever -input is accepted.
func loadIssues(ctx context.Context, path, format string) ([]issues.Issue, error) {
	return source.Load(ctx, path, format, os.Stdin)
}
