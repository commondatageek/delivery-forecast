// Package params registers each forecast command's flags on a flag.FlagSet and
// resolves the parsed flags into a validated internal/app request. The CLI
// fills the FlagSet with fs.Parse; the web server fills it with fs.Set, so
// defaults, presence semantics, date expressions and error messages are
// identical for both. It performs no IO and never reads the clock: callers
// pass now to Resolve, and anything it would log is returned as a warning.
package params
