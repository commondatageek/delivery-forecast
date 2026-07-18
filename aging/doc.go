// Package aging computes cycle-time and WIP-age statistics: CycleTimes builds
// a cycle-time distribution from completed issues, InProgressItems ages
// currently in-progress ones, and RankItems scores both against that
// distribution by percentile.
//
// The package is pure and IO-free: no file, network, or database access, and
// no time.Now — InProgressItems takes "today" as an explicit parameter.
// Callers supply their own Issue records (the CLI maps linear.Issue via
// toAgingIssues).
//
// Options is a convenience bundle for callers that mirror the CLI's flags; the
// functions above don't read it themselves; SampleStart/SampleEnd are only
// carried through to RenderText/RenderHTML for display. Callers are expected
// to have already filtered their Issue set to the desired window before
// calling CycleTimes/InProgressItems/CompletedItems — cycle time and age are
// plain time.Time subtraction, not bucketed by calendar day, so there's no
// day-bucketing convention to observe here (contrast package simulate).
package aging
