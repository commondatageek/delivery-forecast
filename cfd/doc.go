// Package cfd builds a Cumulative Flow Diagram from per-issue lifecycle
// timestamps: a 4-line / 3-band grid (Created, LeftBacklog, Departed,
// Completed) plus flow-health statistics (throughput, average WIP per band,
// cycle time, a Little's Law cross-check, and per-band stability trend).
//
// The package is pure and IO-free. Callers supply their own Issue records
// (the CLI maps sqlite.CFDRow via toCFDIssues): Normalize clamps each issue's
// timestamps to be monotonically non-decreasing and truncates them to day
// resolution (local midnight) — this is the package's own day bucketing, done
// internally, unlike package simulate's caller-supplied windows. BuildGrid
// then walks [start, end] one calendar day at a time, so start and end should
// themselves be local midnight for the same reason BuildPool's window bounds
// must be: a time-of-day offset can shift which day a boundary event lands
// on. AssertInvariants checks the resulting grid's four structural invariants
// (monotonic, nested, conserved, readable) before ComputeHealth derives
// statistics from it.
package cfd
