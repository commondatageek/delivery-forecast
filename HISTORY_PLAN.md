# Implementation plan: `forecast history` + source abstraction

Status: **done.** All six phases shipped on the `history-command` branch; the
document is kept as the record of why things are shaped the way they are.
`CLAUDE.md` and `DATA_REQUIREMENTS.md` describe the code as it now stands —
read those first. The decisions in §2 are still binding on future changes, and
§3's metric definitions are still the normative spec for the `history` package.

Two things deliberately diverged from what's written below:

- **§7 step 3 (`simulate.CountAsOf`).** The plan said to delete it if unused,
  or otherwise make it a thin wrapper over `history` and deprecate it. It has
  no non-test callers, but deleting it would take the cross-check test in
  `history/history_test.go` with it — that test exists to pin D4's
  boundary-day divergence and needs the old raw-timestamp semantics to compare
  against. It keeps its implementation and carries a `Deprecated:` marker.
- **§10.1 (`-group-by project`).** Not built; `history` emits one row per day
  across the whole filtered scope, which is the "default if unanswered"
  recorded below. Still addable without breaking the existing columns.

§10.2 (folding `sim backtest` into `history` behind a `-probability` flag)
remains open by design — it was always meant to be decided after using the CSV
for real.

---

## 0. How to use this document

**Rules for whoever implements this:**

1. **Work one phase at a time, in order.** Each phase must compile and pass
   `just test` on its own before the next phase begins. Do not start Phase N+1
   with Phase N red.
2. **Commit at the end of each phase**, with the phase name in the commit
   message. Do not squash phases together.
3. **Write the tests described in each phase.** They are not optional and not
   "nice to have later". Every phase lists its required tests explicitly.
4. **Do not relitigate the decisions in §2.** They were made deliberately and
   the reasoning is recorded. If you believe one is wrong, stop and raise it
   with the user rather than silently doing something else.
5. **Do not do work assigned to a later phase**, even if it seems convenient.
   The phase boundaries exist to keep each change reviewable.
6. If a phase's acceptance criteria cannot be met as written, stop and report
   what's blocking rather than improvising a different design.

**Repo conventions to follow** (these already exist; match them):

- Root-level packages (`simulate`, `aging`, `cfd`, `counts`) are **pure and
  IO-free**: no file, network, or database access, and they never call
  `time.Now()`. Time and inputs are always passed in as parameters. The new
  `history` and `issues` packages follow the same rule.
- Root-level packages may import `internal/util` (`cfd` already does). That is
  fine for external importers — the `internal/` restriction applies to the
  importer's path, and these packages are inside the module.
- `cmd/forecast` is the only layer that converts storage/source records into a
  package's neutral input structs, and the only layer that does IO.
- Every subcommand accepts `-config <file.yaml>` via `util.ApplyConfig`, called
  immediately after `fs.Parse`. Precedence: CLI flag > config file > default.
- Date flags parse through `util.ParseFlexibleDate` (end/threshold bounds) or
  `util.ParseFlexibleStartDate` (start bounds). Never `time.Parse` directly in
  a command.

---

## 1. Goal

Two connected changes:

**(A) A new `forecast history` command** that emits one row per calendar day
of a project's (or team's) life, with per-day flow metrics, in CSV / JSON /
text. It is the cheap, deterministic, no-Monte-Carlo twin of `sim backtest`,
intended to be piped into a plotting script.

**(B) A source abstraction** so that any command can read issues from a CSV or
JSON file instead of the SQLite database. The point is adoption friction: a
new user with a Jira/GitHub/spreadsheet export should be able to run a report
in minutes, with no API key and no `linear sync`.

These are connected because `history` should be `-input`-native from day one;
retrofitting it afterward would be wasted work.

---

## 2. Decisions already made

Do not re-open these without checking with the user first.

| # | Decision | Reasoning |
|---|---|---|
| D1 | **No `report`/`data` command group.** `history` is a new **top-level** command alongside `aging`, `cfd`, `count`. | Nesting adds keystrokes and buys no shared behavior. The real inconsistency is in flags, not in command placement. |
| D2 | The command is named **`history`**. | Reads naturally: `forecast history -project "Foo" -format csv`. Rejected alternatives: `flow`, `trend`, `series`. |
| D3 | **`history` becomes the single canonical day-walk.** `cfd.BuildGrid` and `simulate.RunBacktest` are refactored to call it (Phase 4) — but only *after* `history` exists and is proven against them. | An up-front "unify the walks" refactor would be speculative. Extraction-by-construction is verifiable. |
| D4 | The unified day-walk uses **day-truncated, monotonically-clamped** timestamps (`cfd.Normalize` semantics), not raw timestamps. | `simulate.CountAsOf` currently uses raw timestamps and so disagrees with `cfd.BuildGrid` on boundary days. A day-grain series must use day-grain membership. This will shift some existing `sim backtest` rows by one day. Pre-1.0; acceptable; must be called out in the changelog. |
| D5 | **Filtering happens in Go, in memory, uniformly for every source.** SQL push-down is not used for the new code path. | At these data volumes (thousands to tens of thousands of issues) in-memory filtering is free, and it means every source and every command gets identical filter semantics. The current per-command filter inconsistency is a *consequence* of each store method having its own bespoke `WHERE` clause. |
| D6 | **`linear.Issue` is hoisted to a public root package `issues`.** | It is already the de facto neutral model, but its name and `internal/` location say "Linear-specific", which is exactly the adoption barrier we're removing. |
| D7 | **`state_type` is optional for file input.** Terminal/started status is derived from timestamps where possible. | A CSV user should not have to translate their tool's workflow vocabulary into Linear's. The four timestamps carry almost all the signal. |
| D8 | **`remaining = backlog + in_progress`**, and `remaining = total − departed` (departed = completed + canceled). Not `total − completed`. | `CountAsOf` today computes `total − completed`, which silently counts canceled items as remaining. It only avoids the bug because `ProjectMilestoneIssues` filters canceled out in SQL. Once canceled issues are in scope, that breaks. |
| D9 | **Lead time and cycle time are separate, separately-named columns.** `lead_time_*` = created→completed. `cycle_time_*` = started→completed. | The codebase currently disagrees with itself: `cfd.ComputeHealth` measures created→completed and calls it cycle time; `aging` measures started→completed and calls it cycle time. Emitting both under distinct names resolves it without breaking either. |
| D10 | **`history` does not early-exit.** It emits every day in `[start, end]`. | `RunBacktest` breaks out once everything is done (`simulate/backtest.go:97`). A plotting series wants the full window. When Phase 4 lands, `sim backtest` preserves its early-exit by trimming `history`'s rows after the fact. |

---

## 3. Metric definitions (normative)

These definitions are binding. Implement exactly these; the tests in Phase 2
assert them.

All day boundaries are **local midnight** (`util.LocalDay`). "As of day D"
always means "at end of day D", i.e. an event whose timestamp truncates to D
*has* happened as of D.

Let `N` = `Options.WindowDays` (default 28).

### 3.1 Per-issue normalized events

Reuse `cfd.Normalize` (see `cfd/cfd.go:102`). It produces:

- `Arrival` — day the issue was created. **Issues with no `created_at` are
  dropped entirely** (`Normalize` returns `ok=false`); count them and report
  the count to the caller.
- `LeftBacklog` — day work started. Falls back to the terminal day if the issue
  ended without ever being started.
- `Exit` — day the issue completed or was canceled; zero if neither.
- `ExitType` — `"completed"`, `"canceled"`, or `""`.

All are clamped so `Arrival <= LeftBacklog <= Exit`.

### 3.2 Tier 1 — cumulative state as of day D

| Column | Definition |
|---|---|
| `total` | count of issues with `Arrival <= D`. **This grows over time** — it is the burnup scope line, not a constant. The final row equals the size of the issue set. |
| `completed` | `Exit <= D` and `ExitType == "completed"` |
| `canceled` | `Exit <= D` and `ExitType == "canceled"` |
| `backlog` | `Arrival <= D` and (`LeftBacklog` is zero or `LeftBacklog > D`) |
| `in_progress` | `LeftBacklog <= D` and (`Exit` is zero or `Exit > D`) |
| `remaining` | `backlog + in_progress` |

**Invariants** (assert these; see Phase 2):

```
total     == completed + canceled + in_progress + backlog
remaining == total - completed - canceled
```

plus monotonic non-decrease of `total`, `completed`, and `canceled` across
consecutive rows.

### 3.3 Tier 2 — daily deltas

Each is the count of events landing exactly on day D (`X_delta[D] = X[D] - X[D-1]`
for the cumulative ones; for the first row, `X_delta = X`).

`created_delta`, `started_delta`, `completed_delta`, `canceled_delta`.

`started_delta` counts issues whose `LeftBacklog == D`.

### 3.4 Tier 2 — rolling metrics

All windows are **trailing and inclusive of D**: the window covers days
`(D − N, D]`, i.e. `N` days ending on D. Days before `Options.Start` are
included in the window if the underlying issue data covers them — the window
looks at the *issue set*, not at the emitted row range. (This matters: the
first emitted row should have a meaningful rolling value if history exists
before it.)

| Column | Definition |
|---|---|
| `throughput_7d` | completions in the trailing 7 days ÷ 7. Items/day. |
| `throughput_28d` | completions in the trailing `N` days ÷ `N`. Items/day. Note: `N` is configurable, but the column name stays `throughput_28d` only if `N == 28`; otherwise name it `throughput_window`. **Simpler: always name it `throughput_window` and emit `window_days` in the JSON/text header.** Use `throughput_window`. |
| `scope_growth_window` | issues with `Arrival` in the trailing window ÷ `N`. Items/day. |
| `net_flow_window` | (arrivals − departures) in the trailing window ÷ `N`. Departure = `Exit` in window, any `ExitType`. |
| `lead_time_p50`, `lead_time_p85` | percentiles of `Exit − Arrival` (in days) over issues that **completed** (`ExitType == "completed"`) in the trailing window. |
| `cycle_time_p50`, `cycle_time_p85` | percentiles of `Exit − LeftBacklog` (in days) over issues that **completed** in the trailing window. |
| `wip_age_avg`, `wip_age_p85`, `wip_age_max` | over issues **in progress on day D** (the `in_progress` set above), the age `D − LeftBacklog` in days. |
| `littles_law_ct` | `in_progress ÷ throughput_window`. |
| `days_remaining_at_rate` | `remaining ÷ throughput_window`. |

**Undefined values.** When a metric has no defined value — an empty sample
(no completions in the window, no WIP on that day) or a division by zero
throughput — set the field to `math.NaN()`. Renderers emit NaN as an **empty
cell** in CSV and text, and as **`null`** in JSON. Never emit `0` for
"undefined"; a real zero and a missing value must be distinguishable, because
they mean opposite things on a chart.

**Percentiles.** Use `util.PercentileValue[T]` (`internal/util/percentile.go`)
on a sorted `[]float64`, matching how `aging` does it. Do not write a new
percentile implementation.

### 3.5 Explicitly out of scope

Not implementable with current data; do not attempt:

- Blocked / stalled counts.
- Per-workflow-state WIP breakdown. The store keeps only the *current* state,
  not transition history.
- `flow_efficiency`. Deferred; revisit after the user has used the CSV.

---

## 4. Phase 1 — the `issues` package

**Goal:** a public, source-neutral issue record; CSV and JSON readers; one
in-memory filter. No behavior change to any existing command.

### 4.1 Create the package

New directory `issues/` at repo root (sibling of `simulate`, `aging`, `cfd`,
`counts`). Package name `issues`. Import path
`github.com/commondatageek/delivery-forecast/issues`.

Move the struct from `internal/linear/issue.go` verbatim into
`issues/issue.go`, renaming the type `linear.Issue` → `issues.Issue`. Keep
every field and every field name unchanged:

```go
type Issue struct {
    Identifier           string
    Title                string
    Assignee             string
    TeamKey              string
    TeamName             string
    ProjectID            string
    ProjectName          string
    ProjectMilestoneID   string
    ProjectMilestoneName string
    StateType            string
    StateName            string
    CreatedAt            time.Time
    StartedAt            time.Time
    CompletedAt          time.Time
    CanceledAt           time.Time
    ArchivedAt           time.Time
    AutoArchivedAt       time.Time
    AddedToProjectAt     time.Time
    UpdatedAt            time.Time
}
```

**In `internal/linear`, leave behind a true Go type alias** so that nothing
else has to change yet:

```go
// Issue is an alias for issues.Issue, retained so existing callers compile
// unchanged. New code should use issues.Issue directly. Removed in Phase 5.
type Issue = issues.Issue
```

A type alias (`=`), not a defined type. With a defined type the existing code
will not compile.

Do **not** move `TeamKey` / `TeamKeyList` (`internal/linear/keylist.go`). They
are a CLI flag concern and stay where they are.

### 4.2 Status helpers (implements D7)

In `issues/issue.go`, add methods that prefer timestamps and fall back to
`StateType`, so file-based input can omit `state_type`:

```go
// IsCompleted reports whether the issue finished successfully. Prefers
// CompletedAt; falls back to StateType == "completed" when no timestamp is set.
func (i Issue) IsCompleted() bool

// IsCanceled reports whether the issue was canceled. Prefers CanceledAt;
// falls back to StateType in {"canceled", "duplicate"}.
func (i Issue) IsCanceled() bool

// IsTerminal reports IsCompleted() || IsCanceled().
func (i Issue) IsTerminal() bool

// IsInProgress reports whether work has started and not yet finished:
// a non-zero StartedAt (or StateType == "started") and !IsTerminal().
func (i Issue) IsInProgress() bool
```

Document on the package doc comment that `duplicate` cannot be distinguished
from `canceled` without `state_type` — that is a known, accepted limitation of
timestamp-only input.

### 4.3 File readers

New file `issues/read.go`. Public API:

```go
// ReadCSV parses issues from CSV. The first row must be a header naming
// columns; column order is irrelevant and unrecognized columns are ignored.
func ReadCSV(r io.Reader) ([]Issue, error)

// ReadJSON parses issues from either a top-level JSON array of objects or
// JSON Lines (one object per line). The form is detected from the first
// non-whitespace byte: '[' means array, anything else means JSON Lines.
func ReadJSON(r io.Reader) ([]Issue, error)

// ReadFile reads issues from path, choosing the parser by file extension:
// .csv -> ReadCSV; .json, .jsonl, .ndjson -> ReadJSON. A path of "-" reads
// stdin, which requires an explicit format (see ReadStream).
func ReadFile(path string) ([]Issue, error)

// ReadStream reads issues from r using the named format ("csv" or "json").
func ReadStream(r io.Reader, format string) ([]Issue, error)
```

**Column / key names are the SQLite column names**, snake_case, exactly as
documented in `DATA_REQUIREMENTS.md`'s schema table:

```
identifier, title, assignee, team_key, team_name, project_id, project_name,
project_milestone_id, project_milestone_name, state_type, state_name,
created_at, started_at, completed_at, canceled_at, archived_at,
auto_archived_at, added_to_project_at, updated_at
```

JSON object keys use the same snake_case names. Add matching `json:"..."` tags
to `Issue` so `encoding/json` round-trips without a separate DTO.

**Timestamp parsing.** Accept, in this order, first match wins:

1. RFC3339 / RFC3339 with nanoseconds (`2025-06-02T14:03:00Z`, with offset)
2. `2006-01-02T15:04:05` (no zone → local)
3. `2006-01-02 15:04:05` (no zone → local)
4. `2006-01-02` (→ local midnight)

An empty string, or the literal `null`, parses to the zero `time.Time`. An
unparseable non-empty value is an **error** naming the row number, the column,
and the offending value — silent zeroing here would produce quietly wrong
charts.

**Error handling.**

- Missing header row, or a header with **no recognized column names at all**:
  error `"no recognized columns in header; expected one or more of: ..."`.
- Recognized-but-incomplete header: fine. Absent columns yield zero values.
  Per-command field requirements are enforced by the commands, per
  `DATA_REQUIREMENTS.md`, not by the reader.
- Ragged CSV rows: error naming the row number.

### 4.4 The filter

New file `issues/filter.go`:

```go
// Filter selects a subset of issues. A zero Filter matches everything.
type Filter struct {
    // Teams matches Issue.TeamKey, case-insensitively. Empty means all teams.
    Teams []string
    // Project matches Issue.ProjectName exactly. Empty means all projects.
    Project string
    // Milestone matches Issue.ProjectMilestoneName exactly. Empty means all
    // milestones. Only meaningful together with Project.
    Milestone string
}

// Apply returns the issues matching f, preserving input order.
func (f Filter) Apply(in []Issue) []Issue
```

Team matching is case-insensitive (uppercase both sides) — `TeamKeyList`
already uppercases on `Set`, but a hand-written CSV will not, and being strict
here is pure user friction for no benefit. Project and milestone matching is
exact and case-sensitive, matching the existing SQL behavior.

### 4.5 Tests (required)

New `issues/read_test.go` and `issues/filter_test.go`:

- CSV round-trip: write a small CSV covering all four timestamp formats, parse,
  assert every field.
- CSV with columns in scrambled order + an unknown column → parses correctly,
  unknown column ignored.
- CSV with only `identifier,created_at,completed_at` → parses, other fields
  zero.
- CSV with a bad timestamp → error mentioning the row number and column name.
- CSV with a header containing no recognized columns → error.
- JSON array and JSON Lines both parse to the same result.
- `ReadFile` dispatches on extension; unknown extension errors clearly.
- Status helpers: a table test covering timestamp-only issues, state_type-only
  issues, and both-present issues, asserting all four predicates.
- `Filter.Apply`: empty filter is identity; team filter is case-insensitive;
  project+milestone narrows correctly; order is preserved.

### 4.6 Acceptance criteria

- `just test` passes.
- `go build ./...` succeeds with **no changes to any existing command file**.
- `internal/linear` still exports `Issue` (as an alias) and all existing code
  compiles unchanged.

### 4.7 Do not do in this phase

Do not touch `cmd/forecast`. Do not add an `-input` flag anywhere. Do not
change `internal/sqlite`.

---

## 5. Phase 2 — the `history` package

**Goal:** the pure day-walk and its renderers. Still no CLI changes.

### 5.1 Package

New directory `history/` at repo root. Package `history`. Pure and IO-free;
never calls `time.Now()`. It may import `internal/util` and `cfd` (for
`Normalize` / `NormalizedIssue` — do **not** duplicate that clamping logic).

Write `history/doc.go` with a package doc comment in the same style as
`cfd/doc.go`, stating the day-bucketing rule and the metric definitions from §3.

### 5.2 API

```go
// Issue is the neutral per-issue input: the four lifecycle timestamps.
// It mirrors cfd.Issue minus StateType, which history does not need.
type Issue struct {
    CreatedAt   time.Time
    StartedAt   time.Time
    CompletedAt time.Time
    CanceledAt  time.Time
}

// Options controls the emitted window and the rolling-metric window.
type Options struct {
    // Start is the first day emitted, inclusive.
    Start time.Time
    // End is the last day emitted, inclusive.
    End time.Time
    // WindowDays is the trailing window for rolling metrics. Zero means 28.
    WindowDays int
}

// DayRow is one calendar day's metrics. See the package doc for exact
// definitions. Float fields are NaN when undefined.
type DayRow struct {
    Date time.Time

    Total      int
    Completed  int
    Canceled   int
    Backlog    int
    InProgress int
    Remaining  int

    CreatedDelta   int
    StartedDelta   int
    CompletedDelta int
    CanceledDelta  int

    Throughput7d        float64
    ThroughputWindow    float64
    ScopeGrowthWindow   float64
    NetFlowWindow       float64
    LeadTimeP50         float64
    LeadTimeP85         float64
    CycleTimeP50        float64
    CycleTimeP85        float64
    WIPAgeAvg           float64
    WIPAgeP85           float64
    WIPAgeMax           float64
    LittlesLawCT        float64
    DaysRemainingAtRate float64
}

// Result bundles the series with the metadata a renderer needs in its header.
type Result struct {
    Rows          []DayRow
    WindowDays    int
    TotalIssues   int // issues in the input set
    SkippedIssues int // dropped for having no created_at
}

// Compute builds the day series. Issues with no CreatedAt are skipped and
// counted in Result.SkippedIssues.
func Compute(issues []Issue, opts Options) (Result, error)

// EarliestCreatedAt returns the minimum non-zero CreatedAt, or the zero time.
// Used by the CLI to default Options.Start.
func EarliestCreatedAt(issues []Issue) time.Time

// AssertInvariants checks the identities in the package doc and returns a
// descriptive error on the first violation. Mirrors cfd.AssertInvariants.
func AssertInvariants(rows []DayRow) error
```

`Compute` returns an error if `End` is before `Start`, or if `Start`/`End` are
zero.

### 5.3 Renderers

`history/render.go`:

```go
func RenderCSV(w io.Writer, res Result) error
func RenderJSON(w io.Writer, res Result) error
func RenderText(w io.Writer, res Result) error
```

- **CSV**: header row of snake_case column names in the struct's field order,
  `date` first, formatted `2006-01-02`. Integers plain; floats to 3 decimal
  places; **NaN → empty cell**. No metadata header (it must stay
  machine-parseable). Use `encoding/csv`, like `printBacktestCSV`.
- **JSON**: `{"window_days":N,"total_issues":N,"skipped_issues":N,"series":[...]}`.
  Row keys snake_case. **NaN → `null`** (use `*float64` in the JSON DTO, or a
  custom marshaller; `encoding/json` cannot marshal a bare NaN and will error
  if you forget this).
- **Text**: `text/tabwriter`, same style as `printBacktestText`. A short header
  block (window, issue counts, skipped) then the table. NaN renders as `-`.
  Floats to 1 decimal.

### 5.4 Tests (required)

`history/history_test.go`. Build small, hand-computed issue sets with explicit
dates and assert exact values — no golden files at this stage.

- **Tier 1 correctness**: a 5-issue set over ~10 days; assert every tier-1
  column on every day.
- **Invariants**: `AssertInvariants` passes on the above; and construct a
  deliberately corrupt `[]DayRow` and assert it fails with a useful message.
- **Cross-check against `cfd.BuildGrid`**: same input, same window; assert
  `history`'s `Total`/`Completed`/`Backlog`/`InProgress` equal `cfd`'s
  `Created`/`Completed`/`Backlog`/`InProgress` row for row. This is the
  contract Phase 4 depends on.
- **Cross-check against `simulate.CountAsOf`**: same input; assert
  `Completed` matches. Expect boundary-day differences from D4's truncation —
  where they differ, the test should assert the *history* value and carry a
  comment explaining why (this is the intended change, not a bug).
- **Deltas**: assert `X_delta` sums to the final cumulative `X`.
- **Rolling metrics**: a set with a known completion cadence; assert
  `ThroughputWindow` and `Throughput7d` to exact values.
- **Undefined values**: a day with no WIP → `WIPAge*` are NaN; a window with no
  completions → percentiles and `ThroughputWindow` are NaN, and
  `LittlesLawCT` / `DaysRemainingAtRate` are NaN rather than `+Inf`.
- **Lead vs cycle time differ**: an issue created well before it started must
  produce `lead_time > cycle_time`. This guards D9.
- **No early exit**: an issue set fully completed halfway through the window
  still emits rows through `End`.
- **Renderers**: NaN → empty CSV cell, `null` in JSON, `-` in text. Assert the
  JSON actually marshals (the NaN trap).

### 5.5 Acceptance criteria

- `just test` passes.
- `go doc ./history` reads coherently and states the day-bucketing rule.
- Nothing outside `history/` changed.

---

## 6. Phase 3 — the `forecast history` command

**Goal:** wire it up, `-input`-native.

### 6.1 Files

- New `cmd/forecast/history.go` with `func cmdHistory(args []string) error`.
- Register in `cmd/forecast/main.go`'s `topCommands`, placed alphabetically
  (after `count`):
  `{Name: "history", Summary: "Per-day history of flow metrics for a project or team.", Run: cmdHistory}`.

### 6.2 Source loading helper

Also new in this phase, in `cmd/forecast/common.go` (or a new
`cmd/forecast/source.go` — preferred, to keep `common.go` from growing):

```go
// loadIssues reads every issue from path, dispatching on file extension:
// .db/.sqlite/.sqlite3 open the SQLite store; everything else goes to
// issues.ReadFile. Returns the issues unfiltered; apply issues.Filter after.
func loadIssues(ctx context.Context, path string) ([]issues.Issue, error)
```

For the SQLite branch this needs a new store method — add to
`internal/sqlite/store.go`:

```go
// AllIssues returns every row in the issues table, unfiltered, so callers can
// apply issues.Filter in memory and get semantics identical to file sources.
func (s *Store) AllIssues(ctx context.Context) ([]linear.Issue, error)
```

Model it on `CFDIssues` (`internal/sqlite/store.go:544`) but select **all**
columns and apply **no** `WHERE` clause. `linear.Issue` is an alias for
`issues.Issue` after Phase 1, so no conversion is needed.

Add a shared input flag helper alongside `addDBFlag`:

```go
// addInputFlag registers -input, the source-agnostic replacement for -db.
func addInputFlag(fs *flag.FlagSet) *string

// resolveInput returns the input path, preferring -input and falling back to
// -db (deprecated). Errors if neither is set; warns if -db was used.
func resolveInput(fs *flag.FlagSet, input, db *string) (string, error)
```

`history` uses `-input` only (it is new; it has no `-db` legacy). The
`-db` fallback in `resolveInput` is there for Phase 5's migrations.

### 6.3 Flags

| Flag | Type | Default | Notes |
|---|---|---|---|
| `-input` | string | *(required)* | path to `.db`, `.csv`, or `.json`; `-` for stdin |
| `-stdin-format` | string | `""` | `csv` or `json`; required only when `-input -` |
| `-project` | string | `""` | exact project name; empty = all projects |
| `-milestone` | string | `""` | exact milestone name; only with `-project` |
| `-teams` | `linear.TeamKeyList` | all | via `addTeamsFlag` |
| `-start` | string | earliest `created_at` in scope | `util.ParseFlexibleStartDate` |
| `-end` | string | `today` | `util.ParseFlexibleDate` |
| `-window` | int | `28` | trailing window in days for rolling metrics |
| `-format` | string | `csv` | `csv`, `json`, or `text` |
| `-out` | string | `""` | output file; empty = stdout |
| `-config` | string | `""` | via `addConfigFlag` |

Note `-format` defaults to **`csv`**, not `text` — the stated purpose is
feeding a plotting script. `cfd` uses `-start`/`-end`, so reuse those exact
names rather than inventing `-replay-start-date`-style ones.

### 6.4 Behavior

1. Parse flags, `util.ApplyConfig`, validate `-format` and `-window > 0`.
2. `now := time.Now()`; resolve `-start`/`-end` through the util helpers.
3. `loadIssues` → `issues.Filter{Teams, Project, Milestone}.Apply(...)`.
4. If the filtered set is empty, return a clear error naming the filters that
   were applied — mirror `cmdSimBacktest`'s "check spelling" wording
   (`cmd/forecast/backtest.go:105`).
5. If `-start` was not set, default to `history.EarliestCreatedAt` truncated
   with `util.LocalDay`. Note this differs from `sim backtest`, which defaults
   to earliest `started_at`; created_at is correct here because `total` is a
   created-to-date line.
6. Convert to `[]history.Issue` in a `toHistoryIssues` function — this is the
   cmd layer's job, per repo convention.
7. `history.Compute`, then `history.AssertInvariants` on the result; a
   violation is a hard error, matching how `cmd/forecast/cfd.go` treats
   `cfd.AssertInvariants`.
8. Render to `-out` or stdout.
9. Call `warnIfBlendingTeams` **only when the input is SQLite** — it takes a
   `*sqlite.Store` today. Either extend it to take the team-key set (preferred:
   `blendingTeamsWarning` is already pure and takes `[]string`) or skip the
   warning for file sources. Preferred approach: derive distinct team keys from
   the loaded `[]issues.Issue` in memory and call `blendingTeamsWarning`
   directly, which makes the warning work for every source.

### 6.5 Tests (required)

Add to `cmd/forecast/` alongside the existing `main_test.go` /
`blendteams_test.go`:

- `toHistoryIssues` maps fields correctly.
- End-to-end: write a small CSV to `t.TempDir()`, run `cmdHistory` with
  `-format json -out <tmp>`, parse the JSON back, assert the series.
- Same fixture via a temp SQLite db → byte-identical JSON output. **This is the
  key test of the whole source abstraction**: it proves file and db paths agree.
- `-start` defaulting to earliest `created_at`.
- Empty filtered set → error mentioning the project name.
- Bad `-format` → error.

### 6.6 Acceptance criteria

- `just test` passes.
- `forecast history -input <db> -project X -format csv` produces a parseable
  CSV whose `completed`/`remaining` columns agree with
  `forecast sim backtest -project X` on the same days (allowing the D4
  boundary-day shift).
- `forecast` with no args lists `history` in its usage output.

---

## 7. Phase 4 — unify the day-walks

**Goal:** delete the duplicated iteration now that `history` is proven.

Do this **only after Phase 3 is merged and the Phase 2 cross-check tests are
green.** Those tests are the safety net for this phase.

1. **`cfd.BuildGrid`** — reimplement over `history.Compute`, mapping
   `history.DayRow` → `cfd.DayRow` (`Created` ← `Total`, `LeftBacklog` ←
   `Total − Backlog`, `Departed` ← `Completed + Canceled`, and the band
   heights directly). Keep `cfd.BuildGrid`'s signature and `cfd.DayRow`
   unchanged so `cmd/forecast/cfd.go` and the HTML template need no edits.
   `cfd`'s existing tests must pass **unmodified** — do not edit them to fit.
   If they fail, the mapping is wrong.

   Watch the import direction: `history` imports `cfd` for `Normalize`
   (§5.1). If `cfd` now imports `history`, that is an import cycle. Resolve by
   **moving `Normalize` / `NormalizedIssue` into `history`** and having `cfd`
   re-export them as aliases (`type NormalizedIssue = history.NormalizedIssue`,
   `var Normalize = history.Normalize`). Decide this at the start of the phase,
   not halfway through.

2. **`simulate.RunBacktest`** — replace its inline `CountAsOf` loop with a
   `history.Compute` call, then walk the resulting rows running the Monte Carlo
   per day. Preserve: the early-exit (trim rows once `Remaining == 0 &&
   AllCreatedBy`), the `Prob = 100.0` shortcut when `Remaining == 0`, and the
   `BacktestRow` shape.

   `simulate` importing `history` is acceptable — both are pure root packages.

3. **Delete** `simulate.CountAsOf` if nothing else uses it; otherwise
   reimplement it as a thin wrapper over `history` and mark it deprecated.

4. **`sim backtest` output will shift** for boundary days (D4). Update
   `simulate`'s backtest tests to the new expected values, with a comment
   pointing at D4. Do not paper over the change.

**Acceptance:** `just test` passes; `cfd`'s and `aging`'s tests are unmodified;
`simulate`'s backtest test changes are limited to boundary-day values and
carry explanatory comments.

---

## 8. Phase 5 — migrate the remaining commands to `-input`

**Goal:** every command reads any source. Ordered easiest → hardest; each
command is independently committable.

For each: add `-input` (keep `-db` working as a deprecated alias via
`resolveInput`, warning once on stderr), load through `loadIssues`, filter with
`issues.Filter`, and replace the SQL-side filtering with in-memory equivalents
using the §4.2 status helpers.

1. **`cfd`** — easiest. `CFDIssues` becomes `AllIssues` + `Filter`. The only
   SQL predicate was `team_key`.
2. **`aging`** — two queries. The cycle-time distribution replaces
   `state_type = 'completed'` with `IsCompleted()`, and the WIP list replaces
   `state_type = 'started' AND started_at IS NOT NULL` with `IsInProgress()`.
   **Note a deliberate behavior fix:** `CompletedBetween` unconditionally
   requires a non-empty `assignee` (`DATA_REQUIREMENTS.md` footnote 1), which
   silently drops unassigned completed issues from `aging`'s distribution even
   though `aging` never filters by assignee. In-memory, drop that requirement
   for `aging` and keep it for `sim` (where the pool is per-engineer and an
   unassigned issue genuinely has nowhere to go). Document the change.
3. **`sim`** (all four subcommands) — `loadPool` currently calls
   `CompletedBetween` directly (`cmd/forecast/common.go:99`). Change it to take
   `[]issues.Issue` and do the date-window + assignee filtering in memory. The
   pool math in `simulate` is untouched.
4. **`count`** — hardest, do last. It is the only command whose SQL does
   aggregation (`NotCompletedCounts`, `ProjectLastUpdated` — `COUNT(*)` /
   `MAX()`). Migrating means writing a pure aggregator that produces
   `[]counts.ProjectMilestoneCount` and `[]counts.ProjectActivity` from
   `[]issues.Issue`. Put it in `counts` as
   `counts.Aggregate(issues []counts.Issue) ([]ProjectMilestoneCount, []ProjectActivity)`,
   with its own tests. Note `duplicate` detection still needs `state_type`
   (D7's documented limitation).

**Then:** remove the `linear.Issue` alias and update all references to
`issues.Issue`. This is mechanical; do it as the final commit of the phase.

**Tests:** for each migrated command, add a test asserting file-source and
db-source produce identical output on the same fixture — the same pattern as
§6.5.

---

## 9. Phase 6 — docs and onboarding

1. **`CLAUDE.md`** — add `forecast history` to the command list with the same
   depth as the existing entries; add the `history` and `issues` packages to
   the architecture paragraph and the ASCII diagram; document `-input` and the
   `-db` deprecation; note D4's backtest boundary change.
2. **`README.md`** — add `history` to the flag tables. Add a short
   **"Bring your own data"** section near the top showing the minimum CSV and a
   working command. Lead with this — it is the adoption pitch.
3. **`DATA_REQUIREMENTS.md`** — reframe from "the SQLite table is the
   interchange format" to "**this is the input contract**, satisfiable by
   SQLite, CSV, or JSON". Add a `history` row to the per-command table. Add a
   CSV/JSON format section (column names, accepted timestamp formats). Document
   that `state_type` is optional when timestamps are present, and that
   `duplicate` is the one thing it is needed for.
4. **`testdata/sample-issues.csv`** — a committed ~30-issue fixture spanning
   ~90 days that produces an interesting `history` output. Reference it from
   the README so a new user can run a real command against the repo in one
   step.
5. **`forecast check -input <file>`** — new command: validates a source and
   reports, per command, whether the data supports it (driven by the
   `DATA_REQUIREMENTS.md` table). Output like:

   ```
   Read 412 issues from issues.csv
     history   ok
     cfd       ok
     aging     ok
     count     ok
     sim       12 completed issues have no assignee and will be excluded
   ```

   This is the single highest-leverage onboarding tool in the plan — it turns
   "did I export the right columns?" from a guessing game into one command.
6. **`NEXTSTEPS.md`** — tick off "A chart showing how p85 for Cycle Time has
   changed over time" (Phase 2 delivers the data for it) and mark "Source
   abstraction" as done for the file-input half.

---

## 10. Open questions for the user

Ask these before starting the phase that depends on them; do not guess.

1. **(Blocks Phase 3.)** When `-project` is *not* given and the scope is a
   whole team, should `history` emit one row per day for the entire team, or
   accept `-group-by project` and emit a `project` column so the plotting
   script can facet? The latter is more useful for process improvement but
   widens the row schema. **Default if unanswered: whole-scope only, no
   `-group-by`** — it can be added later without breaking the existing columns.

2. **(Blocks Phase 4's scope, not its start.)** Should `history` eventually
   absorb `sim backtest` via a `-probability -target-end-date X` flag that
   appends the Monte Carlo column, deprecating `sim backtest`? Attractive (one
   command, one row per day, everything on it) but it drags `sim`'s ~10
   sample-pool flags into a data command. The plan above keeps them separate
   and keeps `history.DayRow` a superset of `BacktestRow`, so the merge stays
   possible. **Decide after using the CSV for real.**

3. **(Phase 6.)** Is `forecast check` worth building now, or deferred? It is
   listed last so it can be dropped without affecting anything else.
