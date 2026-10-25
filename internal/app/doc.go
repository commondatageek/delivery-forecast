// Package app is the application layer between the forecast adapters (the CLI
// today, a web server later) and the analysis packages (simulate, aging, cfd,
// counts, history). Adapters parse user input into a typed request, call one
// function here, and render the typed result; everything in between —
// choosing which issues feed an analysis, converting issues.Issue into each
// package's input structs, defaults, validation, result tables and warnings —
// lives here so every adapter behaves identically.
//
// Purity. This package does no IO: it never reads files, touches os.Stdout,
// os.Stderr or os.Args, logs, or calls time.Now. It must not import
// internal/source, internal/sqlite, internal/linear, internal/logx, flag or
// os. Loading issues and exclusions is internal/source's job; the caller
// passes the loaded []issues.Issue in, so a long-lived server can load once
// and call app many times against the same slice.
//
// Requests carry resolved, typed values. Dates are time.Time (parsing
// "-3 months" or "tomorrow" is the adapter's job), the seed is concrete, and
// "today"/"now" are request fields. A zero value means "not given": Days == 0,
// a zero TargetEnd, a zero ReplayStart.
//
// Validation and errors. Each request has a Validate method covering
// everything that doesn't need the issue set; each run function calls it
// first, and adapters may call it earlier to fail fast. Errors attributable to
// specific inputs are *FieldError, naming the CLI flag(s) (== -config YAML
// keys) at fault.
//
// Warnings and results. Every result carries Warnings: the exact text the CLI
// logs, without the log-level prefix. A run function always returns a non-nil
// result, even alongside an error; Warnings are valid either way and every
// other field is meaningful only when the error is nil. Adapters print
// warnings before checking the error.
package app
