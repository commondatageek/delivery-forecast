// Package issues defines the source-neutral issue record every forecast
// command ultimately operates on, plus readers that build it from CSV or
// JSON files instead of the SQLite store.
//
// Issue mirrors the SQLite issues table column-for-column (see
// DATA_REQUIREMENTS.md) so that a file-based source and a database-based
// source produce identical records. State is normally read from
// StateType/StateName, but file input may omit those entirely: the
// IsCompleted/IsCanceled/IsTerminal/IsInProgress helpers prefer the
// lifecycle timestamps and fall back to StateType only when a timestamp is
// absent. The one thing timestamps alone cannot express is the distinction
// between "canceled" and "duplicate" — both are terminal, non-completed
// states, but only StateType carries which one applies. Timestamp-only input
// is treated as "canceled" in that case; this is a known, accepted
// limitation.
//
// The package is pure and IO-free except for the CSV/JSON file readers in
// read.go, which perform no filesystem access themselves beyond the
// io.Reader/path they're given.
package issues
