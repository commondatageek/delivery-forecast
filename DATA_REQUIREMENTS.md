# Data requirements

This document is the **input contract** for `forecast`: the exact set of
fields each command reads and what happens when one is missing. It's
satisfiable by three interchangeable sources, all shaped like the same
`issues.Issue` record (defined in [`issues/issue.go`](issues/issue.go), field
names mirroring Linear's own GraphQL field names for historical reasons, not
because Linear is required):

- **SQLite** — the `issues` table (schema in
  [`internal/sqlite/store.go`](internal/sqlite/store.go)), populated by
  `linear sync` or by writing to it yourself from another system (Jira,
  GitHub Issues, a spreadsheet, ...).
- **CSV** — one row per issue; see [CSV/JSON format](#csvjson-format) below.
- **JSON** — an array of objects, or JSON-Lines (one object per line); same
  section.

Pass any of the three via `-input` (`.db`/`.sqlite`/`.sqlite3` opens as
SQLite, everything else is read as CSV/JSON by extension). Every `forecast`
command works identically regardless of which source you use — run `forecast
check -input <file>` against an unfamiliar export to get this table's
requirements checked automatically instead of guessing. Timestamps are
bucketed to local calendar days once loaded into Go (see each public
package's doc comment, e.g. `go doc ./simulate`, for the exact rule — it
differs slightly per package).

If you're not using the `forecast` CLI at all — just calling the
`simulate`/`aging`/`cfd`/`counts`/`history` packages directly as a Go
library — skip to [Using this as a library](#using-this-as-a-library) below;
the field requirements below are about what `cmd/forecast` itself needs, not
about the packages' Go API, which takes plain structs.

## Per-command requirements

One row per selection a command makes (a command may make more than one).
"Required" means the row is silently dropped or excluded if the field is
missing; "filter semantics" describes what's matched or excluded; "optional /
display-only" fields are read but don't affect which rows are included.
Filtering happens in Go, in `cmd/forecast`, against whichever source
`-input` loaded (`issues.Issue`'s own `IsCompleted`/`IsCanceled`/
`IsTerminal`/`IsInProgress` methods, plus `issues.Filter` for team/project/
milestone scoping) — not in SQL, so the same rules apply whether the source
is SQLite, CSV, or JSON.

| Command | Required fields | Filter semantics | Optional / display-only fields |
|---|---|---|---|
| `sim items` / `sim days` / `sim probability` / `sim backtest` (sample pool) | `completed_at`, `assignee` (non-empty) | Counted if `completed_at` is non-zero and falls in `[sample-start, sample-end)`, and `assignee` is non-empty. `completed_at`'s presence is what makes an issue "completed" here — `state_type` is never consulted for this query. No team filtering — `sim` has no `-teams` flag; every team is always pooled together. | `identifier`, `title`, `team_name`, `project_name`, `started_at`, `updated_at` (feed `-manifest`'s issue dump only) |
| `sim backtest` (backtested issue set) | `created_at`³ | `-project` exact match (required); `-milestone` exact match (optional); excludes issues where `IsCanceled()` is true. `started_at`/`completed_at` are not required — a missing one doesn't drop the issue, it just changes which day `history.Normalize` (the day-walk `sim backtest` now shares with `history`/`cfd`) treats it as having left the backlog or exited. No team filtering. | `identifier`, `title`, `assignee`, `started_at`, `completed_at`, `state_type`² |
| `aging` (cycle-time distribution) | `started_at`, `completed_at` | Counted if `completed_at` is non-zero and falls in `[sample-start, sample-end)`. Does **not** require a non-empty `assignee`¹. `team_key` filter only if `-teams` given | `identifier`, `title`, `assignee`, `project_name`, `state_name` |
| `aging` (in-progress / WIP ranking) | `started_at` | `IsInProgress()`: `started_at` non-zero and not terminal. `aging.InProgressItems` also independently skips a zero `StartedAt`, so `state_type == "started"` alone can't substitute for a missing `started_at` here. `team_key` filter only if `-teams` given | `identifier`, `title`, `assignee`, `project_name`, `state_name` |
| `cfd` | `created_at`³ | `started_at`, `completed_at`, `canceled_at` are each optional per issue (they determine which band/day the issue occupies, not whether it's included). `team_key` filter only if `-teams` given | `state_type`² |
| `history` | `created_at`³ | Same day-walk as `cfd` (`history.Normalize`/`BuildRows`, which `cfd.BuildGrid` now wraps): `started_at`/`completed_at`/`canceled_at` optional, affecting only which day an issue's arrival/backlog-exit/completion lands on. `-project`/`-milestone` exact match, both optional. `team_key` filter only if `-teams` given | `state_type`² |
| `count` | none per-issue — aggregate counts and a max timestamp | `IsTerminal()` (not counted) vs. not (counted): prefers `completed_at`/`canceled_at`; falls back to `state_type` only when both are zero² — the one command where that fallback regularly matters, since backlog/unstarted issues often carry no timestamps at all. `updated_at` recency (`-updated-since`, project ordering) is measured across **all** issues, including terminal ones. `team_key` filter only if `-teams` given | `team_name`, `project_name`, `project_milestone_name` (grouping/display keys) |
| `linear sync` | writes all 19 columns | n/a — ingest, not a read | not needed at all if you populate the database/file yourself |

`forecast check -input <file>` runs a lighter-weight version of this table
against your data and prints a per-command readiness report — run it before
trusting a new export.

¹ Prior to the `-input` migration, `aging` reused `sim`'s SQL query for its
cycle-time distribution, whose `WHERE` clause unconditionally required a
non-empty `assignee` — even though `aging` never filters by a specific
assignee the way `sim -team`/`-typical-engineers` can. A completed issue with
no assignee was silently excluded from the distribution. This was fixed for
every source (not just files) when `aging` moved off that shared query; see
`completedBetween` in `cmd/forecast/aging.go`.

² `state_type` is read by every command but is a required field for none of
them: `IsCompleted`/`IsCanceled`/`IsTerminal`/`IsInProgress` all prefer the
relevant timestamp and only consult `state_type` when that timestamp is
zero. The one thing no timestamp can tell you is whether a canceled issue
was specifically a `duplicate` — that distinction exists only in
`state_type`, and only `IsCanceled` (which folds `duplicate` into canceled)
depends on it; nothing else in the codebase reads `duplicate` separately. If
your source has no `state_type` column at all, everything still works as
long as the relevant timestamps are present.

³ An issue with no `created_at` is dropped entirely: `history.Normalize`
(which `cfd.Normalize` now wraps) returns `ok=false` for it, and the caller
skips it (see `cmd/forecast/cfd.go`'s and `cmd/forecast/history.go`'s
skipped-issue counts).

## Full schema reference

| Column | Type | Nullable | Meaning |
|---|---|---|---|
| `identifier` | TEXT | NOT NULL (PK) | Unique issue key, e.g. `ENG-123`. Primary key. |
| `title` | TEXT | NOT NULL DEFAULT `''` | Issue title. |
| `assignee` | TEXT | NULL | Assignee name/identifier. NULL when unassigned. |
| `team_key` | TEXT | NOT NULL DEFAULT `''` | Team key (Linear's `team.key`), e.g. `ENG`. Only ever used to filter (`-teams`); never part of a query's selected result. |
| `team_name` | TEXT | NOT NULL DEFAULT `''` | Team display name (Linear's `team.name`). |
| `project_id` | TEXT | NULL | Project ID. NULL when the issue has no project. Written by `linear sync`; not read by any report. |
| `project_name` | TEXT | NULL | Project display name. NULL when the issue has no project. |
| `project_milestone_id` | TEXT | NULL | Milestone ID within the project. NULL when no milestone. Written by `linear sync`; not read by any report. |
| `project_milestone_name` | TEXT | NULL | Milestone display name. NULL when no milestone. |
| `state_type` | TEXT | NOT NULL DEFAULT `''` | Raw workflow state type (Linear's `state.type`), passed through as-is. The codebase only ever tests for the literal values `completed`, `started`, `canceled`, and `duplicate`; any other value passes through untouched (fetched, never specifically matched). |
| `state_name` | TEXT | NOT NULL DEFAULT `''` | Human-readable workflow state name (Linear's `state.name`). |
| `created_at` | DATETIME | NULL | When the issue was created. |
| `started_at` | DATETIME | NULL | When the issue entered a "started" state. NULL if never started. |
| `completed_at` | DATETIME | NULL | When the issue was completed. NULL if not completed. |
| `canceled_at` | DATETIME | NULL | When the issue was canceled. NULL if not canceled. Added for `cfd` support. |
| `archived_at` | DATETIME | NULL | When the issue was archived. Written by `linear sync`; not read by any report. |
| `auto_archived_at` | DATETIME | NULL | When the issue was auto-archived. Written by `linear sync`; not read by any report. |
| `added_to_project_at` | DATETIME | NULL | When the issue was added to its project. Written by `linear sync`; not read by any report. |
| `updated_at` | DATETIME | NULL | Last-modified timestamp. Drives `linear sync`'s incremental watermark and `count`'s `-updated-since` recency filter/ordering. |

## CSV/JSON format

CSV and JSON sources use the same field set as the schema above, minus the
SQLite-only bookkeeping (no primary key constraint, no `NOT NULL DEFAULT`) —
just the column names as headers (CSV) or keys (JSON), matching
`issues.Issue`'s JSON tags exactly (snake_case, e.g. `project_milestone_name`,
`created_at`). Column/key order doesn't matter, and unrecognized columns are
ignored rather than erroring — this is what lets a raw Linear or Jira export
work with only its relevant columns renamed. At least one recognized column
is required, or the file is rejected outright (`issues.ReadCSV`/`ReadJSON`
return an error). Only `identifier` plus whichever fields a given command
needs (per the [table above](#per-command-requirements)) actually matter in
practice — everything else can simply be omitted.

**CSV** — one row per issue, header required:

```csv
identifier,team_key,assignee,project_name,state_type,created_at,started_at,completed_at
ENG-1,ENG,alice,Search Revamp,completed,2025-01-02,2025-01-03,2025-01-08
ENG-2,ENG,bob,Search Revamp,started,2025-01-03,2025-01-04,
```

A blank cell (or a column omitted from the header entirely) means "not set"
— the zero time for timestamp columns, an empty string for text columns.

**JSON** — either a top-level array, or JSON-Lines (one object per line,
auto-detected by the first non-whitespace byte: `[` means array, `{` means
JSON-Lines):

```json
[
  {"identifier": "ENG-1", "team_key": "ENG", "assignee": "alice", "state_type": "completed", "created_at": "2025-01-02", "started_at": "2025-01-03", "completed_at": "2025-01-08"},
  {"identifier": "ENG-2", "team_key": "ENG", "assignee": "bob", "state_type": "started", "created_at": "2025-01-03", "started_at": "2025-01-04"}
]
```

**Timestamp values** (CSV cells and JSON string values both) accept, tried
in order: RFC3339/RFC3339Nano (`2025-01-02T15:04:05Z`, with or without a
zone or fractional seconds); a bare local date or datetime with no zone
(`2025-01-02`, `2025-01-02 15:04:05`, `2025-01-02T15:04:05`, interpreted as
local time); and `""` or the literal string `null`, both of which parse as
"not set" (the zero time), not an error. `-` as `-input` itself reads from
stdin, which requires `-input-format csv` or `-input-format json` since
there's no file extension to sniff.

See [testdata/sample-issues.csv](testdata/sample-issues.csv) for a complete
working example, or run `forecast check -input <file>` to validate one you
built yourself.

## Using this as a library

`simulate`, `aging`, `cfd`, `counts`, and `history` live at the repo root
(not under `internal/`), so they're importable on their own —
`github.com/commondatageek/delivery-forecast/simulate` etc. — by callers who
never touch a SQLite database, a CSV/JSON file, or the `forecast` binary at
all. Each package takes plain structs; build them from whatever source you
have. The `issues` package (also root-level) is there if you want the
`Issue` struct and its `ReadCSV`/`ReadJSON`/`ReadFile`/`Filter` helpers
without going through `cmd/forecast`.

```go
package main

import (
	"fmt"
	"time"

	"github.com/commondatageek/delivery-forecast/simulate"
)

func main() {
	// Build Completions from your own source (Jira, GitHub, a spreadsheet —
	// anything). Only Engineer and CompletedAt matter.
	completions := []simulate.Completion{
		{Engineer: "alice", CompletedAt: time.Date(2025, 6, 2, 0, 0, 0, 0, time.Local)},
		{Engineer: "alice", CompletedAt: time.Date(2025, 6, 5, 0, 0, 0, 0, time.Local)},
		{Engineer: "bob", CompletedAt: time.Date(2025, 6, 3, 0, 0, 0, 0, time.Local)},
		// ...
	}

	start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.Local) // local midnight — see simulate's doc comment
	end := time.Date(2025, 7, 1, 0, 0, 0, 0, time.Local)   // half-open: excludes July 1 itself

	pool := simulate.BuildPool(completions, simulate.Exclusions{}, start, end, false)

	dist := simulate.ItemsInDays(pool, simulate.Params{
		Mode:        simulate.ModeAnonymous,
		Engineers:   3,
		Days:        30,
		Simulations: 10_000,
		Workers:     4,
		Seed:        42,
	})

	// ItemsAtConfidence, not PercentileValue: PercentileValue(dist, 85) is the
	// value 85% of trials fell *at or below*, i.e. only a 15% chance of
	// reaching it. ItemsAtConfidence(dist, 85) inverts that on purpose,
	// returning the floor you're 85% likely to meet or beat.
	fmt.Printf("85%% confidence: at least %d items in 30 days\n", simulate.ItemsAtConfidence(dist, 85))
}
```

`aging`, `cfd`, `counts`, and `history` follow the same shape: build their
neutral input records (`aging.Issue`, `cfd.Issue`,
`counts.ProjectMilestoneCount`/`ProjectActivity`, `history.Issue`) from your
own data, then call the package's exported functions directly —
`aging.CycleTimes`/`InProgressItems`/`RankItems`,
`cfd.Normalize`/`BuildGrid`/`ComputeHealth`, `counts.Compute`,
`history.Compute`/`AssertInvariants`. None of them touch a file, network, or
database, and none of them call `time.Now`. See each package's doc comment
(`go doc ./aging`, `go doc ./cfd`, `go doc ./counts`, `go doc ./history`)
for its exact inputs and semantics.
