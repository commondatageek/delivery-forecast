# delivery-forecast

A delivery-forecasting toolkit: Monte Carlo forecasts, cycle-time/flow
reports, and per-day flow metrics, all from a single `forecast` binary.
Issues can come from [Linear](https://linear.app) (`linear sync` into a
local SQLite database) or from a plain CSV/JSON file you already have — see
[Bring your own data](#bring-your-own-data) below.

```
linear.Client  --Fetch-->  issues.Issue  --Upsert-->  sqlite.Store (linear.db)  --+
                                                                                    |
                                       issues.ReadFile (CSV/JSON, no Linear)  ------+
                                                                                    |
                                                             loadIssues (-input)
                                                                                    |
                    +----------------------+-----------------------+---------------+
                    |                      |                       |
             forecast sim          forecast aging/cfd/count   forecast history
       (Monte Carlo forecasts)  (cycle-time / WIP-age / CFD)  (per-day flow metrics)
```

## Install

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/commondatageek/delivery-forecast/main/install.sh | sh
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/commondatageek/delivery-forecast/main/install.ps1 | iex
```

Both scripts download the release asset matching your OS/arch, verify its
SHA256 checksum, and install `forecast` to `~/.forecast/bin`
(`%USERPROFILE%\.forecast\bin` on Windows), adding it to your `PATH` unless
opted out.

| Env var | Default | Description |
|---|---|---|
| `FORECAST_INSTALL_DIR` | `~/.forecast/bin` | where to install the binary |
| `FORECAST_VERSION` | latest release | pin a specific release tag, e.g. `v1.2.3` |
| `FORECAST_NO_MODIFY_PATH` | unset | set to skip editing your shell profile / User `PATH` |

To pin a version (or set any other env var above) with the piped `install.sh`
one-liner, put the assignment **after** the pipe, on the `sh` side — not
before `curl`. `curl` and `sh` are separate processes joined by a pipe, so a
prefix like `FORECAST_VERSION=v1.2.3 curl ... | sh` only sets the variable
for `curl` and `sh` never sees it, silently installing latest instead:

```bash
# correct — the env var reaches sh, which is what reads it
curl -fsSL https://raw.githubusercontent.com/commondatageek/delivery-forecast/main/install.sh | FORECAST_VERSION=v1.2.3 sh

# also correct
export FORECAST_VERSION=v1.2.3
curl -fsSL https://raw.githubusercontent.com/commondatageek/delivery-forecast/main/install.sh | sh
```

(PowerShell's `irm | iex` doesn't have this issue — `$env:FORECAST_VERSION = "v1.2.3"` set beforehand works fine since both run in the same session.)

Prefer to install manually? Grab the archive for your platform from the
[releases page](https://github.com/commondatageek/delivery-forecast/releases)
and extract the `forecast` binary onto your `PATH` yourself.

Once installed, `forecast update` (below) handles future upgrades in place.
Before upgrading, skim [CHANGELOG.md](CHANGELOG.md) — it records the changes
that move numbers you may already be relying on.

## Build & test

Uses [Just](https://just.systems):

```bash
just build       # compiles bin/forecast
just test        # go test ./...
```

Or plain Go:

```bash
go build -o bin/forecast ./cmd/forecast
go test ./...
```

Run `forecast` with no arguments to see the full command list.

## Bring your own data

Linear isn't required. Every command reads issues via `-input`, which accepts
a SQLite database (what `linear sync` produces), a CSV file, or a JSON file —
no sync step, no API key. The minimum a CSV needs is an identifier and
whichever lifecycle timestamps the command you're running uses:

```csv
identifier,created_at,started_at,completed_at
ENG-1,2025-01-02,2025-01-03,2025-01-08
ENG-2,2025-01-03,2025-01-04,2025-01-10
ENG-3,2025-01-05,2025-01-06,2025-01-09
```

```bash
forecast check -input issues.csv     # sanity-check the file before trusting it
forecast history -input issues.csv -format text
```

A richer fixture — 30 issues spanning ~90 days, with completed, canceled,
in-progress, and backlog work — is committed at
[testdata/sample-issues.csv](testdata/sample-issues.csv), so you can run a
real command against this repo in one step:

```bash
forecast history -input testdata/sample-issues.csv -format text
```

See [DATA_REQUIREMENTS.md](DATA_REQUIREMENTS.md) for the accepted CSV/JSON
column names and timestamp formats, and exactly which fields each command
needs. `-db` still works as a deprecated alias for `-input` wherever a
command used to require it (SQLite only; logs a warning).

## Linear ingest

`forecast linear sync` and `forecast linear teams` require a
`LINEAR_API_KEY` environment variable (a Linear personal API key).

```bash
export LINEAR_API_KEY=lin_api_...
forecast linear teams
forecast linear sync -db linear.db -all-teams
```

A brand-new/empty database needs `-teams` or `-all-teams` to seed it; after
that, `forecast linear sync -db linear.db` alone will incrementally sync
every team already in the db, each against its own watermark.

| Flag | Default | Description |
|---|---|---|
| `-db` | *(required)* | path to SQLite database |
| `-teams` | | comma-separated team keys, e.g. ENG,DESIGN; limits the candidate team set |
| `-all-teams` | `false` | expand the candidate team set to every accessible Linear team; mutually exclusive with `-teams` |
| `-full-reload` | `false` | ignore each team's stored watermark and do a full reload |
| `-config` | | path to a YAML config file supplying flag values (CLI flags override) |

`forecast linear teams` just lists accessible teams (key, name) and exits; it takes only `-config`.

## `forecast sim` — Monte Carlo forecasting

Four subcommands, all sampling from the same historical daily-completion
data (`-sample-start`/`-sample-end`) in one of three mutually-exclusive
modes:

One of the three is required — there is no implicit default mode:

- `-engineers N` — pool all engineers' history together and draw for N anonymous equivalent engineers.
- `-team alice,bob` — each named engineer draws from their own history.
- `-whole-team` — sum all engineers' daily counts into one series (ignores individual variance).

**Confidence vs. probability vs. percentile** — three different jobs, don't
conflate them:
- **Confidence** is an input: a safety level you choose, always read toward
  the conservative side of the distribution. `sim items -confidence 85` means
  "give me a floor I'll hit or beat 85% of the time" (fewer items, since more
  is optimistic). `sim days -confidence 85` means "give me a ceiling I'll
  finish within 85% of the time" (more days, since fewer is optimistic) — same
  85, opposite arithmetic, because items and days are conservative in opposite
  directions.
- **Probability** (`sim probability`) is the output computed for a plan you
  already have — not a knob. It's the exact inverse of confidence: if
  `sim items -confidence 85` says "at least 40 items", `sim probability -items
  40` reports ~85%.
- **Percentile** is a plain descriptive rank with no safe side, used where
  there's nothing to commit to — e.g. `forecast aging`, where an in-progress
  issue's percentile against historical cycle times is a *warning* (higher is
  older/worse), not a floor.

### `sim items` — how many items in D days?

```bash
forecast sim items -db linear.db -team alice,bob -days 30
```

| Flag | Default | Description |
|---|---|---|
| `-input` | *(required)* | path to a SQLite database (`.db`), CSV, or JSON file; `-` reads stdin — see [Bring your own data](#bring-your-own-data) |
| `-stdin-format` | | format of `-input` when reading stdin: `csv` or `json` |
| `-db` | | deprecated alias for `-input` (SQLite databases only); still works but logs a warning |
| `-exclusions` | `exclusions.json` | path to exclusions JSON file |
| `-engineers` | *(required unless `-team`/`-whole-team`)* | number of (equivalent) engineers |
| `-days` | `30` | number of days |
| `-whole-team` | `false` | use whole-team daily throughput from historical data (ignores `-engineers`) |
| `-simulations` | `10000` | number of Monte Carlo simulations to run |
| `-goroutines` | NumCPU | number of parallel worker goroutines |
| `-sample-start` | 3 months ago | sample data start date (YYYY-MM-DD) |
| `-sample-end` | now | sample data end date (YYYY-MM-DD) |
| `-random-seed` | time-based | seed for the random number generator |
| `-confidence` | `50,75,85,95` | comma-separated confidence levels to output; `-confidence 85` means "85% chance of completing at least N items" (`-percentile` is removed — see below) |
| `-typical-engineers` | all | comma-separated list of the team's typical engineers to build the sample pool from |
| `-team` | | comma-separated list of specific engineer names to model individually |
| `-manifest` | | write a run-provenance JSON manifest to this path (`-` for stdout) |
| `-config` | | path to a YAML config file supplying flag values (CLI flags override) |

`-percentile` no longer exists on `sim items` — it always errors with a
migration message. It wasn't renamed in place because its meaning was
inverted: the old `-percentile 85` read "85% of trials landed at or below N",
the opposite of "85% chance of at least N". The old `-percentile 85` is now
`-confidence 15`.

### `sim days` — how many days to finish I items?

```bash
forecast sim days -db linear.db -whole-team -items 50
```

Same flags as `sim items`, plus:

| Flag | Default | Description |
|---|---|---|
| `-items` | *(required)* | number of items to complete; comma-separated for a grouped trajectory report (e.g. `13,12,9`) |
| `-target-start-date` | `today` | forecast start date used to compute calendar dates (YYYY-MM-DD, or: today, tomorrow) |
| `-confidence` | `50,75,85,95` | comma-separated confidence levels to output; `-confidence 85` means "85% chance of finishing within N days" (`-percentile` still works here as a deprecated alias — same meaning, since more days is already the conservative direction) |

(no `-days` flag — that's `sim items`'s target quantity.)

### `sim probability` — probability of completing I items in D days?

```bash
forecast sim probability -db linear.db -engineers 4 -days 30 -items 40
```

Same base flags as `sim items` (minus `-confidence`), plus:

| Flag | Default | Description |
|---|---|---|
| `-days` | | number of days; mutually exclusive with `-target-end-date`, one must be given |
| `-target-start-date` | `tomorrow` | start of the target window (YYYY-MM-DD, or: today, tomorrow) |
| `-target-end-date` | | end of the target window (YYYY-MM-DD, or: today, tomorrow); mutually exclusive with `-days`, one must be given |
| `-items` | `-1` | number of items to complete (omit, i.e. leave at -1, to show the full distribution) |

### `sim backtest` — replay probability forecasts day-by-day

Replays `sim probability`-style forecasts against a project/milestone's
actual history, one row per day from the replay start date to a completion
deadline.

```bash
forecast sim backtest -db linear.db -whole-team -project "Q3 Migration" -target-end-date 2025-09-30
```

Same base sampling flags as `sim items`, plus:

| Flag | Default | Description |
|---|---|---|
| `-project` | *(required)* | project name to backtest |
| `-milestone` | | milestone name within the project (optional) |
| `-replay-start-date` | earliest `started_at` in the issue set | first day to replay from, inclusive (YYYY-MM-DD) |
| `-target-end-date` | *(required)* | completion deadline to forecast against (YYYY-MM-DD) |
| `-format` | `text` | output format: `text` or `csv` |

Note: `-simulations` here means "simulations per backtested day" (same default, `10000`).

## `forecast aging` — WIP-age / cycle-time report

Computes the historical cycle-time distribution from completed issues, then
ranks currently in-progress issues by percentile against that distribution.
Every item also gets a `MULTIPLIER`: its age divided by the cycle time at
`-percentile` (the "threshold"), so `1.34x` means 34% older than that
anchor and `0.80x` means 20% younger. The `PERCENTILE` and `MULTIPLIER`
columns answer related but different questions computed by different
methods (cumulative rank vs. nearest rank), so on rare rows near a
boundary they can disagree by a hair about which side of the anchor an
item falls on — the color bands are keyed on the multiplier specifically
so a cell's color always agrees with the number printed in it.

```bash
forecast aging -db linear.db -format html > aging.html
```

| Flag | Default | Description |
|---|---|---|
| `-input` | *(required)* | path to a SQLite database (`.db`), CSV, or JSON file; `-` reads stdin — see [Bring your own data](#bring-your-own-data) |
| `-stdin-format` | | format of `-input` when reading stdin: `csv` or `json` |
| `-db` | | deprecated alias for `-input` (SQLite databases only); still works but logs a warning |
| `-sample-start` | today minus 3 months | start of completed-issue window (YYYY-MM-DD) |
| `-sample-end` | today | end of completed-issue window (YYYY-MM-DD) |
| `-format` | `text` | output format: `text`, `json`, `html` |
| `-min-cycle-time` | | exclude completed issues with cycle time below this duration (e.g. `5m`, `1h`, `1d`) |
| `-percentile` | `85` | percentile of the cycle-time distribution to anchor the report to; the `MULTIPLIER` column shows each item's age as a multiple of that threshold |
| `-teams` | all teams | comma-separated team keys to filter by (e.g. DATA,PLT) |
| `-config` | | path to a YAML config file supplying flag values (CLI flags override) |

## `forecast cfd` — Cumulative Flow Diagram

Builds a 4-line / 3-band CFD (Created, LeftBacklog, Departed, Completed)
and flow-health stats (throughput, avg WIP, cycle time, Little's Law
cross-check). Renders an interactive Plotly HTML chart by default, or a
daily-series JSON.

```bash
forecast cfd -db linear.db -start 2025-01-01 -end 2025-07-01 -out cfd.html
```

| Flag | Default | Description |
|---|---|---|
| `-input` | *(required)* | path to a SQLite database (`.db`), CSV, or JSON file; `-` reads stdin — see [Bring your own data](#bring-your-own-data) |
| `-stdin-format` | | format of `-input` when reading stdin: `csv` or `json` |
| `-db` | | deprecated alias for `-input` (SQLite databases only); still works but logs a warning |
| `-teams` | all teams | comma-separated team keys to filter by (e.g. ENG,DATA) |
| `-start` | today minus 3 months | start date, inclusive (YYYY-MM-DD) |
| `-end` | today | end date, inclusive (YYYY-MM-DD) |
| `-format` | `html` | output format: `html`, `json` |
| `-out` | stdout | write output to this file instead of stdout |
| `-config` | | path to a YAML config file supplying flag values (CLI flags override) |

## `forecast check` — validate a source before trusting it

Reads a source (SQLite db, CSV, or JSON) and reports, per command, whether
the loaded issues support it — e.g. how many completed issues are missing an
assignee and will be silently excluded from `sim`'s sample pool. Run this
first against a new export instead of guessing which columns matter; see
[DATA_REQUIREMENTS.md](DATA_REQUIREMENTS.md) for the full per-command
requirements this checks against.

```bash
forecast check -input testdata/sample-issues.csv
```

```
Read 30 issues from testdata/sample-issues.csv
  history   ok
  cfd       ok
  aging     ok
  count     ok
  sim       ok
```

| Flag | Default | Description |
|---|---|---|
| `-input` | *(required)* | path to a SQLite database (`.db`), CSV, or JSON file; `-` reads stdin (requires `-stdin-format`) |
| `-stdin-format` | | format of `-input` when reading stdin: `csv` or `json` |
| `-config` | | path to a YAML config file supplying flag values (CLI flags override) |

## `forecast count` — outstanding-work report

Counts non-terminal issues (not `completed`/`canceled`/`duplicate`), grouped
by project (and optionally milestone). Read-only.

```bash
forecast count -db linear.db -milestones
```

| Flag | Default | Description |
|---|---|---|
| `-input` | *(required)* | path to a SQLite database (`.db`), CSV, or JSON file; `-` reads stdin — see [Bring your own data](#bring-your-own-data) |
| `-stdin-format` | | format of `-input` when reading stdin: `csv` or `json` |
| `-db` | | deprecated alias for `-input` (SQLite databases only); still works but logs a warning |
| `-milestones` | `false` | add a per-milestone breakdown under each project |
| `-updated-since` | today minus 3 months | only include projects with an issue updated on/after this date (YYYY-MM-DD) |
| `-teams` | all teams | comma-separated team keys to filter by (e.g. ENG,DESIGN) |
| `-config` | | path to a YAML config file supplying flag values (CLI flags override) |

## `forecast history` — per-day flow metrics

Emits one row per calendar day of a project's or team's life — total scope,
completed/canceled/backlog/in-progress counts, throughput, lead/cycle-time
percentiles, WIP age, and a Little's Law cross-check — the deterministic,
non-Monte-Carlo counterpart to `sim backtest`.

```bash
forecast history -input testdata/sample-issues.csv -format text
```

| Flag | Default | Description |
|---|---|---|
| `-input` | *(required)* | path to a SQLite database (`.db`), CSV, or JSON file; `-` reads stdin (requires `-stdin-format`) |
| `-stdin-format` | | format of `-input` when reading stdin: `csv` or `json` |
| `-project` | all projects | exact project name to scope to |
| `-milestone` | all milestones | exact milestone name within `-project` |
| `-teams` | all teams | comma-separated team keys to filter by (e.g. ENG,DATA) |
| `-start` | earliest `created_at` in scope | first day emitted, inclusive (YYYY-MM-DD; or: yesterday, today, tomorrow, `-3 months`) |
| `-end` | today | last day emitted, inclusive |
| `-window` | `28` | trailing window in days for rolling metrics (throughput, scope growth, net flow) |
| `-format` | `csv` | output format: `csv`, `json`, `text` |
| `-out` | stdout | write output to this file instead of stdout |
| `-config` | | path to a YAML config file supplying flag values (CLI flags override) |

Note: `history` day-truncates and clamps timestamps before counting (a
completion can land a calendar day earlier than raw-timestamp comparisons
would put it). `sim backtest` now walks the same day-walk internally, so its
day-by-day counts match `history`'s exactly — this is a deliberate behavior
change from `sim backtest`'s pre-`history` implementation, which compared raw
timestamps instead.

## `forecast version` — print version and build info

```bash
forecast version
```

Prints the build-time version (set via `-ldflags "-X main.version=..."` for
released binaries, `(dev)` for local builds) plus VCS-stamped build info:
git SHA, git time, dirty flag, Go version, module.

## `forecast update` — self-update

Checks the latest GitHub release, compares it against the running binary's
version, and — after confirmation — downloads, checksum-verifies, and
installs the release asset matching this OS/arch, replacing the running
executable in place.

```bash
forecast update -check   # report current vs. latest, install nothing
forecast update -yes     # download, verify, and install without prompting
```

| Flag | Default | Description |
|---|---|---|
| `-check` | `false` | report current vs. latest version and exit without installing anything |
| `-yes` | `false` | skip the interactive confirmation prompt |
| `-force` | `false` | proceed even if already on the latest version, or the current version is unknown (a dev build) |
| `-timeout` | `60s` | overall HTTP timeout |

## Config files (`-config`)

Every subcommand accepts `-config <file.yaml>` to supply flag values from a
YAML file, applied immediately after flag parsing. Precedence is **CLI flag
> config file > built-in default**.

- Keys equal flag names exactly as passed on the command line (e.g.
  `-sample-end` → `sample-end`).
- List flags (`-teams`, `-team`, `-typical-engineers`, `-confidence`, `-items`) take a
  YAML sequence, joined into the same comma-separated string the flag
  itself accepts: `teams: [ENG, DATA]` behaves identically to
  `-teams ENG,DATA`. A plain string (`teams: "ENG,DATA"`) also works.
- Presence-sensitive flags behave as if passed on the CLI: a `sample-end` or
  `random-seed` set only in a config file still counts as "explicitly set"
  — e.g. `random-seed: 42` in a config pins the seed exactly like
  `-random-seed 42` would, rather than falling back to the time-based
  default.
- The `config` key itself is reserved/ignored inside the file.
- One config file's keys are shared by exactly one command's flags — there's
  no per-command sectioning (a `sim items` config and a `count` config are
  separate files).

Example for `forecast sim items` (`sim-items.yaml`) — `input:` is the
current key; `db:` still works as a deprecated alias wherever a command
accepts `-db`:

```yaml
input: linear.db
engineers: 4
days: 30
sample-start: "2025-01-01"
sample-end: "2025-07-01"
random-seed: 42
confidence: [50, 75, 85, 95]
team: [alice, bob]
```

```bash
forecast sim items -config sim-items.yaml            # uses every value above
forecast sim items -config sim-items.yaml -days 60   # CLI -days wins over the file's 30
```

## `exclusions.json`

Optional input to `forecast sim` (e.g. for holidays), pointed at via
`-exclusions` (default: `exclusions.json` in the working directory):

```json
{
  "global": ["2024-12-25"],
  "engineers": {"alice": ["2024-06-17"]}
}
```

`global` dates are excluded for every engineer; `engineers` dates are
excluded only for the named engineer.

## Using as a library

The `simulate`, `aging`, `cfd`, `counts`, and `history` packages are pure,
IO-free, and independent of Linear/SQLite, so they're importable on their
own — `github.com/commondatageek/delivery-forecast/simulate`, etc. — by
anyone who wants the same Monte Carlo forecasting, cycle-time/CFD/count
analysis, or per-day flow metrics over data from another source. The
`issues` package (also root-level) provides the shared `Issue` record plus
`ReadCSV`/`ReadJSON`/`ReadFile` if you want file parsing without going
through `cmd/forecast` at all. See [DATA_REQUIREMENTS.md](DATA_REQUIREMENTS.md)
for what each package needs from your data and a short library-usage example.

## Conventions worth knowing

- `-sample-end`: if explicitly set, it's a calendar date (midnight, that day
  excluded). If omitted, it defaults to *now*, so today's already-completed
  work counts.
- `-random-seed`: time-based (non-deterministic) unless explicitly passed —
  either on the CLI or via `-config`.

## License

Copyright 2026 Aaron Johnson.

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for the
full text.
