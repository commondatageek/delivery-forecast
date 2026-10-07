# Changelog

Notable changes to `forecast`, newest first. GitHub release notes are
auto-generated from commit subjects
([`.github/workflows/release.yml`](.github/workflows/release.yml)); this file
exists for the things a list of commit subjects doesn't convey — behavior
changes that alter numbers you may already be relying on.

Versions follow the `vMAJOR.MINOR.PATCH` tags on the repo. Pre-1.0: breaking
behavior changes ship in minor releases, called out here.

## Unreleased

### Added

- **`forecast history`** — per-day flow metrics: one row per calendar day of a
  project's or team's life (scope, completed/canceled/backlog/in-progress
  counts, daily deltas, rolling throughput, lead- and cycle-time percentiles,
  WIP age, Little's Law cross-check). CSV by default, plus JSON and text. It is
  the deterministic, no-Monte-Carlo twin of `sim backtest`, meant to be piped
  into a plotting script.
- **`forecast check`** — validates a source file or database and reports, per
  command, whether the data supports it (e.g. how many completed issues have no
  assignee and will be excluded from `sim`'s pool, or how many issues have no
  `updated_at` for `count`'s recency filter). Run it first against an
  unfamiliar export.
- **Bring your own data.** Every command now accepts `-input <path>`, which
  reads a SQLite database, a CSV file, or a JSON file (array or JSON Lines).
  No Linear account and no `linear sync` step required. `-input -` reads
  stdin, given `-stdin-format csv|json`. Column names are the SQLite column
  names; see [DATA_REQUIREMENTS.md](DATA_REQUIREMENTS.md) and the committed
  [testdata/sample-issues.csv](testdata/sample-issues.csv) fixture.
- **`issues` package** (public, at the repo root) — the source-neutral issue
  record plus CSV/JSON readers and a filter, usable as a standalone Go library.
- **`history` package** (public, at the repo root) — the pure day-walk behind
  `forecast history`, now also the single implementation `cfd.BuildGrid` and
  `simulate.RunBacktest` both build on.
- **`forecast aging -percentile`** (default `85`) chooses which percentile of
  the cycle-time distribution the report is anchored to — previously
  hardcoded. Every item also gets a `MULTIPLIER` column (`multiplier` key in
  JSON): its age expressed as a multiple of the cycle time at that
  percentile, so `1.34x` reads as "34% older than the anchor."

### Changed

- **`sim backtest` rows can shift by one day.** This is the one change here
  that alters existing output. `RunBacktest` now walks `history.Compute`'s
  rows, which day-truncate and clamp each issue's timestamps before counting,
  where the old inline loop compared raw timestamps. A completion landing
  mid-day therefore counts a calendar day earlier than it used to, so a
  boundary-day `completed`/`remaining` count — and the probability derived
  from it — may differ by one day from a previous release. The upside is that
  `sim backtest`, `cfd`, and `history` can no longer disagree about which day
  an event landed on. (Decision D4 in `HISTORY_PLAN.md`.)
- **`forecast aging` no longer drops unassigned completed issues** from its
  cycle-time distribution. The old SQL query required a non-empty `assignee`
  even though `aging` never groups by assignee, so an unassigned completed
  issue was silently excluded from the percentile baseline. Aging percentiles
  may move slightly if your data has unassigned completed work. `sim` still
  requires an assignee, where a per-engineer pool genuinely needs one.
- **`forecast aging -format json` gains a `multiplier` key** on every object.
  Relevant to anyone decoding the output strictly.
- **`forecast aging`'s text/HTML color bands now key on the multiplier**
  (`>= 1.00x` high, `>= 0.85x` medium) rather than on the percentile rank
  (`>= 85` / `>= 70`). At the default `-percentile 85` the two agree on
  roughly 99.5% of rows; the rest are hairline cases that used to be colored
  against the number printed beside them.
- **Pinned `-random-seed` results from earlier builds no longer reproduce.**
  The engine now draws day-by-day across slots (to apply calendar exclusions)
  rather than engineer-by-engineer, so the RNG stream is consumed in a
  different order. Same-build determinism is unchanged: the same seed, inputs,
  and `-goroutines` still give identical output every run.

### Deprecated

- **`-db` is a deprecated alias for `-input`** on `aging`, `cfd`, `count`, and
  `sim`. It still works (SQLite only) and logs a warning. In config files,
  prefer the `input:` key over `db:`.
- **`simulate.CountAsOf`** — retained only as the test baseline documenting the
  boundary-day change above. Use `history.Compute` and read
  `DayRow.Completed` / `DayRow.Remaining`.

### Removed

- **`forecast sim -team`.** Over a typical 3-month window each engineer's
  series is ~90 mostly-zero samples, so drawing from one person's history is
  lumpy and makes the forecast hypersensitive to which names were typed; it
  also invited "what if Alice worked on it instead of Bob?" comparisons the
  data can't support. Replaced in a following change by named `-engineers`.
- **`sim items -percentile` (the always-erroring tombstone) and
  `sim days -percentile` (alias of `-confidence`).** Use `-confidence`. Note:
  `aging -percentile` is unrelated and unchanged.
- `linear.Issue` (use `issues.Issue`; it was an alias for one release).
- The per-command read methods on `sqlite.Store` — `CompletedBetween`,
  `InProgress`, `NotCompletedCounts`, `ProjectLastUpdated`, `CFDIssues`,
  `ProjectMilestoneIssues` — superseded by `AllIssues` plus in-memory
  filtering, which is what makes every source behave identically.

## v0.131.0

### Changed

- **`forecast sim items -percentile` replaced by `-confidence`, with the
  meaning inverted.** The old `-percentile 85` read "85% of trials landed at or
  below N" (a 15%-confidence floor); `-confidence 85` reads "85% chance of
  completing *at least* N items" (`simulate.ItemsAtConfidence`). It wasn't
  renamed in place because the flip would have silently changed the numbers
  behind any existing script or config file. `-percentile` still exists on
  `sim items` but always errors with a migration message. To get the old
  numbers, translate: the old `-percentile 85` is now `-confidence 15`.
  `sim days` keeps `-percentile` as a working alias of `-confidence`, since for
  days the conservative direction was already "larger" and nothing inverted.
  `sim probability` is the exact inverse of `-confidence`: if
  `sim items -confidence 85` says "at least 40", `sim probability -items 40`
  reports ~85%.
