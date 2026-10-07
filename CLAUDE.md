# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

Uses [Just](https://just.systems) for automation (`just` CLI required):

```bash
just build       # Compile the forecast binary to bin/
just test        # Run all Go tests
```

Manual build (mirrors `just build`):
```bash
go build -o bin/forecast ./cmd/forecast
```

Single Go module (`github.com/commondatageek/delivery-forecast`, see `go.mod`) — no `go.work`. Tests exist in `cmd/forecast`, `simulate`, `aging`, `cfd`, `counts`, `history`, `issues`, `internal/sqlite`, `internal/linear`, `internal/syncer`, and `internal/selfupdate`; `just test` (or `go test ./...`) runs them. No linting is configured.

## Architecture

This is a delivery-forecasting toolkit whose source of record is `issues.Issue`, a neutral record shaped like Linear's own fields but not tied to Linear: `linear.Client` fetches from Linear's GraphQL API and `sqlite.Store` persists into a SQLite `issues` table, but any `forecast` command can equally read a CSV or JSON file of the same shape via `-input` — no Linear account or sync step required (see "Bring your own data" in [README.md](README.md)). Five packages — `simulate`, `aging`, `cfd`, `counts`, `history` — hold all of the actual analysis logic and are pure, IO-free, and source-agnostic (no Linear or SQLite types in their exported API); they live at the repo root, alongside `issues` (the shared record type + file readers/filter, also source-agnostic), rather than under `internal/`, so they're usable as a standalone Go library by callers who bring their own data. Everything else (`linear`, `sqlite`, `syncer`, `util`, `logx`, `selfupdate`) is `internal/`: implementation detail of the `forecast` CLI and Linear ingest path, not part of the public surface. Each analysis package defines its own small input structs (e.g. `aging.Issue`, `cfd.Issue`, `counts.ProjectMilestoneCount`/`ProjectActivity`, `history.Issue`, mirroring `simulate`'s pre-existing `Completion`/`BacktestItem`); `cmd/forecast` is the only place that converts `issues.Issue` (loaded from either source via `loadIssues`) into them (`toAgingIssues`, `toCFDIssues`, `toHistoryIssues`, `toCountsIssues`), so storage and analysis stay independent of each other. See [DATA_REQUIREMENTS.md](DATA_REQUIREMENTS.md) for exactly which `issues.Issue` fields each command/package needs.

```
linear.Client  --Fetch-->  issues.Issue  --Upsert-->  sqlite.Store (linear.db, "issues" table)  --+
                                                                                                    |
                                              issues.ReadFile (CSV/JSON, no Linear involved)  ------+
                                                                                                    |
                                                                              cmd/forecast.loadIssues (-input)
                                                                                                    |
                          +----------------------+-----------------------+-----------------------+
                          |                      |                       |                       |
                   forecast sim          forecast aging/cfd/count                        forecast history
             (Monte Carlo forecasts)   (cycle-time / WIP-age / CFD reports)      (per-day flow metrics; deterministic
                                                                                   twin of `sim backtest`)
```

**`issues`** — The source-agnostic record every command operates on. `Issue` mirrors the SQLite schema's column names as its JSON tags, so the same struct round-trips through SQLite, CSV, or JSON unchanged. `ReadCSV`/`ReadJSON` (array or JSON-Lines, auto-detected by first non-whitespace byte)/`ReadFile` (dispatches on `.csv`/`.json`/`.jsonl`/`.ndjson` extension; `-` requires calling `ReadStream` with an explicit format)/`ReadStream` parse timestamps by trying RFC3339Nano, then two no-timezone-→ local layouts, then `util.ParseDate` (local midnight); `""`/`"null"` parse as the zero time. `Filter{Teams, Project, Milestone}.Apply` applies the same team/project/milestone scoping every command uses. `IsCompleted`/`IsCanceled`/`IsTerminal`/`IsInProgress` prefer the relevant timestamp and fall back to `StateType` so file sources that omit `state_type` still work — the one thing `state_type` alone can disambiguate that no timestamp can is `duplicate` vs. plain `canceled` (both fold into `IsCanceled`).

**`internal/linear`** — Defines `Client`, which fetches `issues.Issue` records from the Linear.app GraphQL API (`Client.Fetch`, paginated, **all** issues — no state or assignee filter; only an optional team scope and the incremental `updatedAt` watermark). `KeyList` is a `flag.Value` for comma-separated, upper-cased team keys. `toIssue` keeps every issue, mapping the GraphQL `issueNode` onto `issues.Issue` and collapsing absent related objects (assignee, team, project, milestone, state) to empty strings; the store persists those as NULL. It uses Linear's own field names (`team.key`/`team.name` → `TeamKey`/`TeamName`, `state.type` → `StateType`, `state.name` → `StateName`, `projectMilestone.id/name` → `ProjectMilestoneID/Name`). `Client.ListTeams` lists every accessible team (`key`, `name`), used by `forecast linear teams` and `forecast linear sync -all-teams`. (`linear.Issue` no longer exists as its own type — it was hoisted to `issues.Issue` so file sources didn't have to depend on a Linear-named package; see Phase 1/5 of `HISTORY_PLAN.md`.)

**`internal/sqlite`** — The only place SQL lives. `Store` wraps a `database/sql` SQLite connection (via `modernc.org/sqlite`, pure Go, no cgo) with WAL mode and an idempotent `CREATE TABLE IF NOT EXISTS` schema (no migration framework — the store only replicates Linear's own data, so rebuilding it from scratch is trivial and cheap). `Open` creates the db file if missing (used by `linear sync`, which legitimately seeds a new database); `OpenExisting` first checks the file is present and errors clearly if not, used by every read-only command so a typo'd `-db`/`-input` path fails loudly instead of silently opening an empty database. Key methods: `Upsert` (keyed on `identifier`), `LatestUpdatedAtForTeam` (per-team watermark for incremental sync), `DistinctTeamKeys` (every team key currently in the store), `AllIssues` (every issue unfiltered — the read method `cmd/forecast` calls; it backs `-input`'s SQLite path exactly like `issues.ReadFile` backs the CSV/JSON path, with filtering/aggregation happening afterward in Go via `issues.Filter`/`counts.Aggregate` either way). The per-command SQL queries `AllIssues` superseded (`CompletedBetween`, `InProgress`, `NotCompletedCounts`, `ProjectLastUpdated`, `CFDIssues`, `ProjectMilestoneIssues`) were deleted once the `-input` migration (Phase 5) and `history`-backed CFD rewrite (Phase 4) left them uncalled — `sim backtest` was the last caller of `ProjectMilestoneIssues`, moved off it in Phase 4b.

### Commands (`cmd/forecast`)

All commands live in the single `forecast` binary. Run `forecast` with no arguments for the full command list.

- **`forecast linear sync`** — Production ingest path. `forecast linear sync -db <path> [-teams k1,k2] [-all-teams] [-full-reload]` syncs **one team at a time**, each against its own `team_key` watermark, committing before moving to the next. With no `-teams`/`-all-teams`, it incrementally syncs every team already in the db. `-teams k1,k2` limits/extends the candidate set (new keys get a full sync); `-all-teams` expands the candidate set to every accessible Linear team (via `Client.ListTeams`); `-full-reload` ignores the watermark and full-syncs every candidate team. A brand-new/empty db requires `-teams` or `-all-teams` to seed it. Progress is logged via `log/slog`. Accepts `-config <file.yaml>` to supply flag values from a YAML file keyed by flag name; CLI flags override config values, which override built-in defaults.
- **`forecast linear teams`** — Lists accessible Linear teams (key, name) and exits.
- **`forecast sim`** — The Monte Carlo engine. Four subcommands:
  - `items` — how many items can N engineers complete in D days?
  - `days` — how many days for N engineers to complete I items?
  - `probability` — probability of completing I items in D days?
  - `backtest` — replay probability forecasts day-by-day for a project/milestone.

  Builds a `SamplePool` (per-engineer slice of daily completion counts over `[sample-start, sample-end)`, default: today minus 3 months → now) from issues loaded via `-input` (SQLite/CSV/JSON — `-db` still works as a deprecated alias, SQLite only), and runs `-simulations` trials by resampling, parallelized across `-goroutines` workers (each with its own seeded `*rand.Rand` — never share one across goroutines). Two sampling modes, mutually exclusive, and one is required (no implicit default — `simulate.ResolveMode` errors if none is given): `-engineers` — either a count (`-engineers 3`: that many anonymous slots, each drawing from the pooled samples) or a list of names (`-engineers alice,bob,carol`: the same, but each slot is *named* so per-engineer exclusions can attach to it; names need not appear in the data; parsed by `engineerSpec` in `cmd/forecast/engineers.go`, where an integer is a count and anything else a name list) — and `-whole-team` (sums all engineers into one daily series, ignoring individual variance). Every forecast is anchored to a real date: `sim items`, `sim days`, and `sim probability` all take `-target-start-date` (default `tomorrow` — today's completions are already in the pool via `-sample-end now`), and `items`/`probability` take exactly one of `-days` or `-target-end-date` (inclusive end; `resolveTargetWindow` in `cmd/forecast/common.go`; neither has a default length). `sim days`'s `-items` is likewise required, with no default group size. Exclusions (`-exclusions`, see `exclusions.json` below) apply on both sides through one `simulate.Calendar`: sample-window dates are dropped from the pool (`BuildPool`, calendar anchored at `-sample-start`), and horizon dates contribute 0 completions without consuming an RNG draw (the engine, calendar anchored at the target start; the day still counts toward the calendar, so `-days 30` is 30 calendar days and `sim days` reports calendar days). `sim backtest` rebases the calendar to each replayed day so exclusions in `[replayDay, target-end]` apply as they would have then. `-whole-team` honors only `global` entries and warns if the file has per-engineer ones. `-manifest` (on `items`/`days`/`probability`) writes a run-provenance JSON document, `schema_version` 2: `data.exclusions` holds the file's path and entries plus the resolved dates dropped from the sample and zeroed in the horizon. All subcommands accept `-config <file.yaml>` to supply flag values from a YAML file keyed by flag name; CLI flags override config values, which override built-in defaults.

  `items`/`days` share a `-confidence` flag (default `50,75,85,95`), always read as a safety level chosen toward the conservative tail: `sim items -confidence 85` returns the largest N such that `ProbabilityAtLeast(dist, N) >= 85` (`simulate.ItemsAtConfidence`, a binary search — deliberately not `PercentileValue(dist, 85)`, which reads the opposite tail and would only be a 15%-confidence floor); `sim days -confidence 85` returns `PercentileValue(dist, 85)` directly, since for days the conservative direction is already "larger", no inversion needed. `sim probability`'s output is intentionally the mirror image: given a plan, it reports the computed probability, annotated in its own text to read as the same confidence-scale number (`ItemsAtConfidence`/`ProbabilityAtLeast` are exact inverses by construction).
- **`forecast count`** — Outstanding-work report. `forecast count -input <path> [-milestones] [-updated-since YYYY-MM-DD] [-teams k1,k2]` counts issues that are *not* in a terminal state (`state_type` not in `completed`/`canceled`/`duplicate` — i.e. all non-terminal issues, started or not), grouped by project. By default it prints a per-project summary table with columns for issue count and milestone count (real milestones only, i.e. excluding the `(No Milestone)` bucket); `-milestones` instead prints each project with its total followed by an indented per-milestone breakdown. `-updated-since` (default: today − 3 months) drops projects whose most-recently-updated issue predates the given date, and projects are ordered by most recently updated issue first; this "last touched" timestamp is measured across **all** the project's issues (including terminal ones), even though the counts themselves include only non-terminal issues — both computed in Go by `counts.Aggregate` from the loaded issue set. `-teams` (a `linear.TeamKeyList`, comma-separated and upper-cased) filters to the given team keys; default is all teams. When `-teams` is not given and the loaded set holds more than one team, a team name is shown alongside each project (a `TEAM` column in the summary, a `[Team]` suffix in the grouped view) and a `warning:` is logged to stderr noting that data is being blended across all teams (via `blendingTeamsWarning` + `distinctTeamKeys` in `cmd/forecast/common.go`/`source.go`, shared with `aging`/`cfd`/`history`). Issues without a milestone are bucketed under `(No Milestone)` and issues without a project under `(No Project)`. Read-only (never writes the db). Accepts `-config <file.yaml>` to supply flag values from a YAML file keyed by flag name; CLI flags override config values, which override built-in defaults.
- **`forecast aging`** — WIP-age / cycle-time report. Computes the historical cycle-time distribution (`completed_at - started_at`) from completed issues loaded via `-input`, then ranks currently in-progress issues by percentile against that distribution. `-percentile` (default `85`) chooses which percentile of that distribution the report is anchored to; every item's `Multiplier` (`AgeDays / threshold`, shown as the `MULTIPLIER` column in text/HTML and a `multiplier` key in JSON) expresses its age as a multiple of the cycle time at that percentile — `null`/em dash when the distribution has no usable threshold to divide by. The `Percentile` column (cumulative rank, `util.ComputePercentile`) and `Multiplier` (nearest-rank threshold, `util.PercentileValue`) are computed by different methods and are only approximate inverses of each other, so they can disagree by a hair on which side of the anchor a hairline item falls; `ageClass` (the text/HTML color banding) is deliberately keyed on the multiplier rather than the rank so a cell's color always agrees with the number printed in it — see `aging/aging.go`'s `ageClass` doc comment. Unlike `sim`'s sample pool, the completed-issue selection here (`completedBetween` in `cmd/forecast/aging.go`) does **not** require a non-empty `assignee` — aging never groups by assignee, so an unassigned completed issue still belongs in the distribution; this was a deliberate bug fix made during the Phase 5 `-input` migration (the old SQL-backed query silently dropped such issues for every source, not just files). With `-show-completed`, `text` and `html` output add a second section — visually divided from the first, with the same columns aligned to it — listing the completed issues that make up the percentile distribution sample itself, each ranked against that same distribution; both sections are sorted descending by days (age for in-progress, cycle time for completed). `json` is unaffected by `-show-completed` and only ever emits the in-progress items. Scoped by `-teams` (default: all teams); when `-teams` is omitted and the loaded set holds more than one team, a `warning:` is logged to stderr noting the cycle-time distribution blends all teams (via `blendingTeamsWarning`). Accepts `-config <file.yaml>` to supply flag values from a YAML file keyed by flag name; CLI flags override config values, which override built-in defaults.
- **`forecast cfd`** — Cumulative Flow Diagram. `forecast cfd -input <path> [-teams k1,k2] [-start YYYY-MM-DD] [-end YYYY-MM-DD] [-format html|json] [-out file.html]` builds a 4-line / 3-band CFD (Created, LeftBacklog, Departed, Completed) from `canceled_at`, `started_at`, `completed_at`, and `created_at`, asserts the four CFD invariants (monotonic, nested, conserved, readable), computes flow-health stats (throughput, avg WIP per band, cycle time, Little's Law cross-check, per-band stability), and renders an interactive Plotly stacked-area chart (HTML default) or a daily-series JSON. Window defaults to today minus 3 months → today. `BuildGrid` is a thin wrapper over `history.BuildRows` (see Phase 4 in `HISTORY_PLAN.md` — the two commands share one day-walk implementation). When `-teams` is omitted and the loaded set holds more than one team, a `warning:` is logged to stderr noting the CFD blends flow across all teams (via `blendingTeamsWarning`). Accepts `-config <file.yaml>` to supply flag values from a YAML file keyed by flag name; CLI flags override config values, which override built-in defaults.
- **`forecast history`** — Per-day flow metrics: one row per calendar day of a project's/team's life (total scope, completed/canceled/backlog/in-progress counts, deltas, throughput, lead/cycle-time percentiles, WIP age, Little's Law cross-check), the deterministic non-Monte-Carlo counterpart to `sim backtest`. `forecast history -input <path> [-project P] [-milestone M] [-teams k1,k2] [-start YYYY-MM-DD] [-end YYYY-MM-DD] [-window N] [-format csv|json|text] [-out file]`. No `-group-by`: with no `-project`, it emits one row per day across the whole loaded/filtered scope rather than faceting by project (see open question 10.1 in `HISTORY_PLAN.md` — deliberately deferred). `-start` defaults to the earliest `created_at` in scope; `-end` defaults to today. Built on `history.Compute`, which day-truncates and clamps every issue's timestamps before counting — this is a deliberate divergence from the old raw-timestamp comparisons `sim backtest` used to do inline (D4 in `HISTORY_PLAN.md`): a completion landing mid-day can now count a calendar day earlier than it used to. `sim backtest` was rewritten in Phase 4b to walk `history.Compute`'s rows instead of its own loop, so the two commands now agree exactly on which day a boundary event lands; `simulate.CountAsOf` was deliberately kept with its original raw-timestamp semantics rather than becoming a thin wrapper, since a test in `history_test.go` cross-checks against it specifically to document this divergence. Accepts `-config <file.yaml>` the same way every other subcommand does.
- **`forecast check`** — Validates a source (`-input <path>`, plus `-stdin-format` for stdin) and reports, per command (`history`, `cfd`, `aging`, `count`, `sim`), whether the loaded issue set supports it — e.g. how many completed issues have no `assignee` and will be silently excluded from `sim`'s pool. Pure logic lives in `checkResults` (`cmd/forecast/check.go`), checked against the same fields documented per-command in `DATA_REQUIREMENTS.md`'s table (created_at for history/cfd, started_at-on-completed for aging, updated_at for count, assignee-on-completed for sim). `-exclusions <path>` additionally validates an exclusions file (`checkExclusions`): parse validity (an unreadable/unparseable file prints an `invalid` line and exits 0), entry counts per scope, a past/future date summary, and names matching no assignee in the input. Meant to be the first command run against an unfamiliar export.
- **`forecast version`** — Prints the binary's version (the `-ldflags -X main.version=...` override if set at build time, else `(dev)`) plus VCS-stamped build info (git SHA/time, dirty flag, Go version, module) via `buildInfo()`.
- **`forecast update`** — Self-update. Checks `github.com/commondatageek/delivery-forecast`'s latest release, compares its tag against the running binary's version, and — after an interactive confirmation (skippable with `-yes`) — downloads the release asset matching this OS/arch, verifies its SHA256 against the release's `checksums.txt`, extracts the binary, and atomically replaces the running executable. `-check` reports current vs. latest without installing anything; `-force` proceeds even when already up to date or when the running build has no version info (a dev build). Logic lives in `internal/selfupdate`.

**`scripts/check-engineer-data.sh`** — Sanity-checks `linear.db` for a set of engineers before trusting a `forecast sim`/`forecast aging` run (completed-issue counts, distinct days with completions, zero-count days, lifetime first/last completion). Mirrors `forecast sim`'s date semantics: start inclusive, end exclusive.

### Data formats

**`linear.db`** (SQLite, the default data store, written by `linear sync`) — single `issues` table, primary key `identifier`. Schema defined inline in `internal/sqlite/store.go` (`schema` constant). Columns are faithful transliterations of Linear's own field names (e.g. `team_key`/`team_name` from `team.key`/`team.name`, `state_type`/`state_name` from `state.type`/`state.name`, `project_milestone_id`/`project_milestone_name` from `projectMilestone`). The genuinely-optional columns (`assignee`, `project_id`, `project_name`, `project_milestone_id`, `project_milestone_name`) are nullable and stored as NULL when absent (via `nullString` in `internal/sqlite`); always-present columns (`title`, `team_key`, `team_name`, `state_type`, `state_name`) are `NOT NULL DEFAULT ''`. Reads coalesce NULL back to `""`, so consumers still see plain strings. `canceled_at` is a nullable DATETIME column added for CFD support; populated by re-syncing after any schema reset.

**CSV/JSON files** (read via `-input`, an equally valid alternative to a SQLite `.db`) — same shape as the `issues` table, one row/object per issue; only `identifier` plus whichever timestamp columns a given command needs are required, everything else is optional. Column/field names are the `issues.Issue` struct's JSON tags (snake_case, matching the SQLite column names exactly). See [DATA_REQUIREMENTS.md](DATA_REQUIREMENTS.md) for the accepted column names and timestamp formats, and [testdata/sample-issues.csv](testdata/sample-issues.csv) for a working example.

**`exclusions.json`** (optional input to `forecast sim` via `-exclusions <path>`, e.g. for holidays and PTO):
```json
{
  "global": [
    "2025-12-25",
    "2025-12-22/2026-01-02",
    {"from": "2026-07-03", "to": "2026-07-06", "reason": "July 4th weekend"},
    {"date": "2026-11-26", "reason": "Thanksgiving"}
  ],
  "engineers": {
    "alice": ["2026-03-02/2026-03-13", {"date": "2026-04-10", "reason": "PTO"}],
    "bob":   ["2026-02-16"]
  }
}
```
`-exclusions` has no default and the file must exist when given. One calendar applies on both sides: a date in the sample window is dropped from the throughput sample, a date in the forecast horizon contributes no completions.

### Config files (`-config`)

Every subcommand accepts `-config <file.yaml>`, applied via `util.ApplyConfig` (`internal/util/config.go`) immediately after `fs.Parse`. Precedence is **CLI flag > config file > built-in default**. Rules:

- **Keys equal flag names**, exactly as passed on the command line (e.g. `-sample-end` → `sample-end`, `-random-seed` → `random-seed`).
- **List flags** (`-teams`, `-typical-engineers`, `-confidence`, `-items`) take a YAML sequence, joined into the same comma-separated string the flag itself accepts: `teams: [ENG, DATA]` behaves identically to `-teams ENG,DATA`. A plain string (`teams: "ENG,DATA"`) works too.
- **Presence-sensitive flags behave as if passed on the CLI.** Config values are applied via `fs.Set`, so `sample-end` or `random-seed` set only in a config file still counts as "explicitly set" for `resolveEndDate`/`resolveSeed` — e.g. a `random-seed: 42` in config pins the seed exactly like `-random-seed 42` would, rather than falling back to the time-based default.
- The `config` key itself is reserved/ignored inside the file (prevents self-reference).
- One config file's keys are shared by exactly one command's `FlagSet` — there's no per-command sectioning (a `sim items` config and a `count` config are separate files); see Stage 4's non-goal on a shared multi-command file.

Example config for `forecast sim items` (`sim-items.yaml`) — `input:` is the current key; `db:` still works as a deprecated alias wherever the command accepts `-db`:
```yaml
input: linear.db
engineers: 4
days: 30
sample-start: "2025-01-01"
sample-end: "2025-07-01"
random-seed: 42
confidence: [50, 75, 85, 95]
```
```bash
forecast sim items -config sim-items.yaml            # uses every value above
forecast sim items -config sim-items.yaml -days 60   # CLI -days wins over the file's 30
```

Example for `forecast linear sync` (`sync.yaml`):
```yaml
db: linear.db
teams: [ENG, DATA]
full-reload: true
```

Example for `forecast count` (`count.yaml`):
```yaml
input: linear.db
milestones: true
updated-since: "2025-04-01"
teams: [ENG, DESIGN]
```

Example for `forecast aging` (`aging.yaml`):
```yaml
input: linear.db
format: html
sample-start: "2025-01-01"
min-cycle-time: 1h
percentile: 90
```

Example for `forecast cfd` (`cfd.yaml`):
```yaml
input: linear.db
start: "2025-04-01"
end: "2025-07-01"
format: json
```

Example for `forecast history` (`history.yaml`):
```yaml
input: testdata/sample-issues.csv
window: 28
format: json
```

### Conventions worth knowing

- Date flags: every user-facing date flag (`-sample-start`/`-sample-end`, `-start`/`-end`, `-updated-since`, `-target-start-date`/`-target-end-date`, `-replay-start-date`) is parsed by one of two shared helpers in `internal/util/date.go`, keyed off whether the flag is a window's start or end/threshold bound:
  - `util.ParseFlexibleDate` — used for end/threshold-type bounds (`-sample-end`, `-end`, `-updated-since`, `-target-end-date`). Accepts `YYYY-MM-DD`; the keywords `now`/`yesterday`/`today`/`tomorrow`; and relative offsets (`-3 months`, `+2 weeks`, `90 days`, `3 months ago`; units day/week/month/year, singular or plural; a leading `+` is future, a `-`/`ago`/no-sign is past; `+…ago` is rejected). Every result snaps to local midnight **except** `now`, which resolves to the exact instant passed in — `now` names a point in time, not a calendar day. `now` is itself `-sample-end`'s literal flag default, so an unset `-sample-end` and an explicit `-sample-end now` behave identically: today's already-completed work counts up to this exact moment.
  - `util.ParseFlexibleStartDate` — used for start-type bounds (`-sample-start`, `-start`, `-target-start-date`, `-replay-start-date`). Identical to `ParseFlexibleDate` except it rejects `"now"` with an error. This isn't just a style rule: window starts are bucketed into whole days via `util.DayIndex`, which rounds a day-vs-start gap to the nearest 24h — a `now` start (carrying a time-of-day offset) can round a same-day record to the *previous* day and silently drop it from the window. `now` is safe as an end bound (only ever compared against, never used as `DayIndex`'s anchor) but not as a start bound.
  
  Both helpers take `now` as a parameter (not `time.Now()`) so resolution is deterministic and testable.
- Random seeding: `-random-seed` is time-based (non-deterministic) unless explicitly passed, via the `isFlagSet` flag-presence pattern in `cmd/forecast/common.go`.
- Source loading: `aging`/`cfd`/`count`/`sim`/`sim backtest` accept both `-input` (SQLite/CSV/JSON, preferred) and `-db` (deprecated alias, SQLite only, logs a warning); `resolveInput` in `cmd/forecast/source.go` prefers `-input` and falls back to `-db`, erroring if neither is set. `history` and `check` are new commands with no legacy `-db` to support, so they only ever take `-input`. Every command loads the full issue set once via `loadIssues` (dispatching on file extension: `.db`/`.sqlite`/`.sqlite3` → `sqlite.OpenExisting` + `Store.AllIssues`, everything else → `issues.ReadFile`) and then filters/scopes it in Go via `issues.Filter`, rather than pushing any filtering into SQL — this is what makes every source behave identically regardless of backend. `-input -` reads stdin, which has no extension to dispatch on and so requires the companion `-stdin-format` flag (`csv`/`json`, registered by `addStdinFormatFlag` wherever `addInputFlag` is); `loadIssues` handles that case itself rather than each command doing it inline, so the stdin promise in `-input`'s help text holds for every command that offers the flag.

## On-call modeling

`ONCALL_MODELING.md` documents a planned (not yet implemented) feature to model on-call rotations. Two design options are discussed: a `-oncall-fraction` flag vs. separate sample pools for on-call vs. normal days.
