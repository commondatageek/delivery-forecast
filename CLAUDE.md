# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

Uses [Task](https://taskfile.dev) for automation (`task` CLI required):

```bash
task build       # Compile all Go binaries to bin/
task generate    # Generate synthetic issues.json via Python (py/generate-issues)
task fetch       # Sync completed/in-progress issues from Linear into items.db (requires LINEAR_API_KEY)
```

Manual builds (mirrors `task build`):
```bash
go build -C cmd/aging-report -o ../../bin/aging-report .
go build -C cmd/linear-fetch -o ../../bin/linear-fetch .
go build -C cmd/sim -o ../../bin/sim .
go build -C cmd/sync -o ../../bin/sync .
```

Single Go module (`forecasting`, see `go.mod`) — no `go.work`. There are no automated tests or linting configured.

## Architecture

This is a throughput-forecasting toolkit built around one vendor-neutral data model (`internal/item.Item`) that flows into a single SQLite store, which the simulation and reporting tools query.

```
Source (Linear API)  --Fetch-->  item.Item  --Upsert-->  sqlite.Store (items.db)
                                                                |
                                          +---------------------+----------------------+
                                          |                                            |
                                     cmd/sim                                  cmd/aging-report
                              (Monte Carlo forecasts)                    (cycle-time / WIP-age report)
```

**`internal/item`** — Defines `Item` (the common record: source, identifier, assignee, team, project, status, timestamps) and the `Source` interface (`Name() string`, `Fetch(ctx, since) ([]Item, error)`). Every upstream integration implements `Source`; everything downstream consumes `Item`. `since == zero time` means a full fetch.

**`internal/linear`** — Implements `item.Source` for the Linear.app GraphQL API (`Source.Fetch`, paginated, filters to `completed`/`started` issues with a non-null assignee). `KeyList` is a `flag.Value` for comma-separated, upper-cased team keys. In-progress issues without a `startedAt` are dropped (can't be used for aging).

**`internal/sqlite`** — The only place SQL lives. `Store` wraps a `database/sql` SQLite connection (via `modernc.org/sqlite`, pure Go, no cgo) with WAL mode and goose migrations embedded from `migrations/*.sql`. Key methods: `Upsert` (keyed on `source, identifier`), `LatestUpdatedAt` (per-source watermark for incremental sync), `CompletedBetween` (date-ranged, optionally assignee-filtered), `InProgress`.

**`internal/sync`** — `Sync(ctx, src, store, full)` orchestrates fetch → upsert. Watermarks are derived per `src.Name()` from the items table itself (via `LatestUpdatedAt`), so a zero watermark triggers a full fetch and multiple sources never clobber each other's incremental state.

### Commands (`cmd/`)

- **`sync`** — Production path. Syncs an `item.Source` (currently only `linear`) into `items.db`. `-sync-all` forces a full reload ignoring the watermark; `-all-teams` vs `-teams` selects scope.
- **`sim`** — The Monte Carlo engine. Three subcommands:
  - `items` — how many items can N engineers complete in D days?
  - `days` — how many days for N engineers to complete I items?
  - `probability` — probability of completing I items in D days?

  Builds a `SamplePool` (per-engineer slice of daily completion counts over `[sample-start, sample-end)`) and runs `-simulations` trials by resampling, parallelized across `-goroutines` workers (each with its own seeded `*rand.Rand` — never share one across goroutines). Three sampling modes, mutually exclusive: anonymous `-engineers N` (pools all engineers' samples together), named `-team a,b,c` (each engineer draws from their own history), and `-whole-team` (sums all engineers into one daily series, ignoring individual variance). Defaults to reading from `-db items.db`; passing `-issues` explicitly switches to the legacy NDJSON loader instead (see `resolvePool`/`isFlagSet` in `cmd/sim/main.go` — flag *presence*, not value, decides the source).
- **`aging-report`** — WIP-age / cycle-time report. Computes the historical cycle-time distribution (`completed_at - started_at`) from completed issues, then ranks currently in-progress issues by percentile against that distribution. Same `-db` vs `-issues` dual-path convention as `sim`. Outputs `text`, `json`, or `html`.
- **`linear-fetch`** — Thin one-shot CLI: fetches from Linear and prints NDJSON to stdout (no DB). Mostly superseded by `sync`, kept for ad-hoc/legacy NDJSON workflows.

**`py/generate-issues/main.py`** — Generates synthetic NDJSON issue data for testing, in the same wire format `sim`/`aging-report` expect. No dependencies; managed with `uv` (see `pyproject.toml`/`uv.lock`).

**`scripts/check-engineer-data.sh`** — Sanity-checks `items.db` for a set of engineers before trusting a `sim`/`aging-report` run (completed-item counts, distinct days with completions, zero-count days, lifetime first/last completion). Mirrors `sim`'s date semantics: start inclusive, end exclusive.

### Data formats

**`items.db`** (SQLite, the primary data store) — single `items` table, primary key `(source, identifier)`. Schema in `internal/sqlite/migrations/00001_create_items.sql`.

**NDJSON issues file** (legacy/alternate input to `sim` and `aging-report`, output of `linear-fetch` and `generate-issues`) — one JSON object per line:
```json
{"engineer": "alice", "team": "ENG", "identifier": "ENG-123", "title": "Fix bug", "project": "Q3", "started_at": "2025-08-01T10:00:00Z", "completed_at": "2025-08-01T14:00:00Z", "status": "completed"}
```

**`exclusions.json`** (optional input to `sim`, e.g. for holidays):
```json
{
  "global": ["2024-12-25"],
  "engineers": {"alice": ["2024-06-17"]}
}
```

### Conventions worth knowing

- `-db` vs `-issues`: across `sim` and `aging-report`, the SQLite path is the default; explicitly passing `-issues` switches to the NDJSON loader. This is decided by flag *presence* (`isFlagSet`), not by whether `-db` was also passed.
- `-sample-end` semantics: if explicitly set, it's a calendar date (midnight, that day excluded). If omitted, it defaults to *now* so today's already-completed work counts (see `resolveEndDate`/`daysBetween` in `cmd/sim/main.go`).
- Random seeding: `-random-seed` is time-based (non-deterministic) unless explicitly passed, via the same `isFlagSet` pattern.

## On-call modeling

`ONCALL_MODELING.md` documents a planned (not yet implemented) feature to model on-call rotations. Two design options are discussed: a `-oncall-fraction` flag vs. separate sample pools for on-call vs. normal days.
