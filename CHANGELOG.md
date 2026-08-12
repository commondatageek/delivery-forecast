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
  assignee and will be excluded from `sim`'s pool). Run it first against an
  unfamiliar export.
- **Bring your own data.** Every command now accepts `-input <path>`, which
  reads a SQLite database, a CSV file, or a JSON file (array or JSON Lines).
  No Linear account and no `linear sync` step required. `-input -` reads
  stdin, given `-input-format csv|json`. Column names are the SQLite column
  names; see [DATA_REQUIREMENTS.md](DATA_REQUIREMENTS.md) and the committed
  [testdata/sample-issues.csv](testdata/sample-issues.csv) fixture.
- **`issues` package** (public, at the repo root) — the source-neutral issue
  record plus CSV/JSON readers and a filter, usable as a standalone Go library.
- **`history` package** (public, at the repo root) — the pure day-walk behind
  `forecast history`, now also the single implementation `cfd.BuildGrid` and
  `simulate.RunBacktest` both build on.

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

### Deprecated

- **`-db` is a deprecated alias for `-input`** on `aging`, `cfd`, `count`, and
  `sim`. It still works (SQLite only) and logs a warning. In config files,
  prefer the `input:` key over `db:`.
- **`simulate.CountAsOf`** — retained only as the test baseline documenting the
  boundary-day change above. Use `history.Compute` and read
  `DayRow.Completed` / `DayRow.Remaining`.

### Removed

- `linear.Issue` (use `issues.Issue`; it was an alias for one release).
- The per-command read methods on `sqlite.Store` — `CompletedBetween`,
  `InProgress`, `NotCompletedCounts`, `ProjectLastUpdated`, `CFDIssues`,
  `ProjectMilestoneIssues` — superseded by `AllIssues` plus in-memory
  filtering, which is what makes every source behave identically.
