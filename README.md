# delivery-forecast

Answer "when will we be done?" and "how healthy is our flow?" from your
issue tracker's history. `forecast` is a single command-line tool that turns a
list of issues (with created / started / completed timestamps) into:

- **Forecasts** — Monte Carlo simulations of how many items you'll finish, how
  long a batch will take, or how likely a plan is to land.
- **Flow reports** — cumulative flow diagrams, cycle-time and WIP-age reports,
  outstanding-work counts, and per-day flow metrics.

Issues can come straight from [Linear](https://linear.app), or from a plain
CSV/JSON file exported from any tracker. No Linear account is required.

## What do you want to know?

| Question | Command |
|---|---|
| Is my data good enough to use? | [`forecast check`](#check--is-my-data-usable) |
| How has work flowed over time, day by day? | [`forecast history`](#history--per-day-flow-metrics) |
| Where is work piling up? Is WIP stable? | [`forecast cfd`](#cfd--cumulative-flow-diagram) |
| Which in-progress items are stuck or unusually old? | [`forecast aging`](#aging--which-in-progress-items-are-old) |
| How much open work is there, per project? | [`forecast count`](#count--outstanding-work) |
| How many items can we finish in D days? | [`forecast sim items`](#sim-items--how-many-items-in-d-days) |
| How many days will I items take? | [`forecast sim days`](#sim-days--how-many-days-for-i-items) |
| How likely is "I items in D days"? | [`forecast sim probability`](#sim-probability--how-likely-is-this-plan) |
| Would past forecasts have been right? | [`forecast sim backtest`](#sim-backtest--would-past-forecasts-have-held-up) |

Run `forecast` with no arguments for the command list, and
`forecast <command> -help` for every flag of a command.

## Install

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/commondatageek/delivery-forecast/main/install.sh | sh
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/commondatageek/delivery-forecast/main/install.ps1 | iex
```

Both scripts download the release for your OS/arch, verify its SHA256
checksum, install `forecast` to `~/.forecast/bin` (`%USERPROFILE%\.forecast\bin`
on Windows), and add it to your `PATH`. Prefer to do it by hand? Grab an
archive from the
[releases page](https://github.com/commondatageek/delivery-forecast/releases).

| Env var | Default | Description |
|---|---|---|
| `FORECAST_INSTALL_DIR` | `~/.forecast/bin` | where to install the binary |
| `FORECAST_VERSION` | latest release | pin a release tag, e.g. `v1.2.3` |
| `FORECAST_NO_MODIFY_PATH` | unset | set to skip editing your shell profile / User `PATH` |

With the piped `install.sh` one-liner, set variables on the `sh` side of the
pipe (`... | FORECAST_VERSION=v1.2.3 sh`) or `export` them first. A prefix
before `curl` never reaches `sh`, so you'd silently get the latest release.

Later, `forecast update` upgrades in place (see [Maintenance](#maintenance)).
Skim [CHANGELOG.md](https://github.com/commondatageek/delivery-forecast/blob/main/CHANGELOG.md) before upgrading — it records changes that
move numbers you may already rely on.

## Quick start

Try it on the sample data in this repo, a CSV of 30 issues from early 2025:

```bash
curl -fsSLO https://raw.githubusercontent.com/commondatageek/delivery-forecast/main/testdata/sample-issues.csv

forecast check -input sample-issues.csv
```

```
Read 30 issues from sample-issues.csv
  history   ok
  cfd       ok
  aging     ok
  count     ok
  sim       ok
```

```bash
# Per-day flow metrics, as a table
forecast history -input sample-issues.csv -format text -end 2025-03-15

# A cumulative flow diagram you can open in a browser
forecast cfd -input sample-issues.csv -start 2025-01-01 -end 2025-03-15 -out cfd.html

# Open work, per project and milestone
forecast count -input sample-issues.csv -updated-since 2025-01-01 -milestones

# A forecast: how many items could the whole team finish in 14 days,
# starting 2025-03-16? (-target-start-date defaults to tomorrow.)
forecast sim items -input sample-issues.csv -whole-team -days 14 \
  -sample-start 2025-01-01 -sample-end 2025-03-15 -target-start-date 2025-03-16
```

```
whole-team throughput, 2025-03-16 to 2025-03-29 (14 days) -> how many items?

Confidence  Items
50%         at least 5
75%         at least 3
85%         at least 3
95%         at least 2
```

Read that as: "we'd finish **at least 5** items half the time, and at least 3
items 85% of the time."

Every command also logs a table of its effective flag values (and where each
came from) to stderr. Results go to stdout, so redirects and pipes only see
the report.

> **Why the explicit dates?** Most commands default to "the last 3 months".
> The sample data is from early 2025, so those defaults would find nothing.
> With your own current data you can leave them off.

When you're ready to use your own data, see [Getting your data in](#getting-your-data-in).

## Getting your data in

### From a CSV, JSON, or SQLite file

Every command that reads issues takes `-input`, which accepts a CSV file, a
JSON file (an array, or one object per line), or a SQLite database. There's
no sync step and no API key. At minimum a CSV needs an `identifier` column
plus whichever timestamps the command you're running uses:

```csv
identifier,created_at,started_at,completed_at
ENG-1,2025-01-02,2025-01-03,2025-01-08
ENG-2,2025-01-03,2025-01-04,2025-01-10
ENG-3,2025-01-05,2025-01-06,2025-01-09
```

```bash
forecast check -input issues.csv                          # what will work with this file?
cat issues.csv | forecast history -input - -stdin-format csv   # stdin works too
```

Which columns does each command need?

| Command | Needs |
|---|---|
| `history`, `cfd` | `created_at` (issues without it are skipped); `started_at`, `completed_at`, `canceled_at` sharpen the picture |
| `aging` | `started_at` and `completed_at` on completed issues; `started_at` on in-progress ones |
| `count` | `updated_at`, `state_type` (or `completed_at`/`canceled_at`), and `project_name` for grouping. Projects with no recent `updated_at` are hidden. |
| `sim *` | `completed_at` and a non-empty `assignee` on completed issues |

`forecast check` tests your file against this list and tells you how many
issues each command will silently ignore. Run it first. The full column
list and timestamp formats are in [DATA_REQUIREMENTS.md](https://github.com/commondatageek/delivery-forecast/blob/main/DATA_REQUIREMENTS.md).

### From Linear

`forecast linear` syncs issues from Linear into a local SQLite database, which
you then pass to other commands as `-input`. You need a Linear personal API
key in `LINEAR_API_KEY`.

```bash
export LINEAR_API_KEY=lin_api_...
forecast linear teams                              # list the teams you can access
forecast linear sync -db linear.db -all-teams      # first sync: pick teams
forecast linear sync -db linear.db                 # later: incremental, per team

forecast history -input linear.db -teams ENG
```

| Flag | Default | Description |
|---|---|---|
| `-db` | *(required)* | SQLite database to write (created if missing) |
| `-teams` | | comma-separated team keys, e.g. `ENG,DESIGN`; limits (or extends) the teams to sync |
| `-all-teams` | `false` | sync every team you can access; mutually exclusive with `-teams` |
| `-full-reload` | `false` | ignore each team's stored watermark and re-fetch everything |
| `-config` | | YAML file of flag values (see [Config files](#config-files)) |

A brand-new database needs `-teams` or `-all-teams` to seed it. After that,
`sync` with no team flags incrementally updates every team already in the
database. `forecast linear teams` takes only `-config`.

## Flags every analysis command shares

These apply to `aging`, `cfd`, `check`, `count`, `history`, and all `sim`
subcommands unless noted.

| Flag | Description |
|---|---|
| `-input` | **Required.** A SQLite database (`.db`/`.sqlite`/`.sqlite3`), CSV, or JSON file. `-` reads stdin. |
| `-stdin-format` | `csv` or `json`. Required when `-input -`. |
| `-teams` | Comma-separated team keys to include (e.g. `ENG,DATA`); default is all teams. Available on `aging`, `cfd`, `count`, `history`; `sim` has no team filter. |
| `-config` | Path to a YAML file of flag values; see [Config files](#config-files). |
| `-db` | Deprecated alias for `-input` (SQLite only, logs a warning) on `aging`, `cfd`, `count`, and `sim`. |

If you don't pass `-teams` and your data spans several teams, `aging`, `cfd`,
`count`, and `history` log a warning that they're blending all of them. Every
other flag is listed under its command below.

**Dates.** Every date flag takes `YYYY-MM-DD`, the keywords `yesterday`,
`today`, `tomorrow`, or a relative offset such as `-3 months`, `+2 weeks`, or
`90 days ago` (units: day, week, month, year). End-type flags (`-end`,
`-sample-end`, `-target-end-date`, `-updated-since`) also accept `now`; start-type flags
(`-start`, `-sample-start`, `-target-start-date`, `-replay-start-date`) do not.
A `-sample-end` is exclusive: `2025-03-15` stops at the start of that day.

## Flow reports

### `check` — is my data usable?

Reads a source and reports, per command, whether the issues support it. Run it
first against any new export; see the sample output in [Quick start](#quick-start).

```bash
forecast check -input issues.csv
```

Takes the shared flags (`-input`, `-stdin-format`, `-config`), plus
`-exclusions <path>` to validate an [exclusions file](#exclusionsjson--holidays-and-time-off)
against the same input: whether it parses, how many entries and dates it has
(split into past and future), and any engineer names that match no assignee.

```bash
forecast check -input issues.csv -exclusions exclusions.json
```

```
Exclusions: exclusions.json
  entries   2 global, 1 engineers (alice: 2)
  span      2025-12-25 .. 2026-04-10 (1 dates past, 4 future, as of 2026-01-15)
  engineers ok
```

A file that doesn't parse (or doesn't exist) is reported on an `invalid` line
rather than failing the command.

### `history` — per-day flow metrics

One row per calendar day of a project's or team's life: total scope,
completed / canceled / backlog / in-progress counts, throughput, lead- and
cycle-time percentiles, WIP age, and a Little's Law cross-check. Output is CSV
by default (handy for plotting), or JSON or a text table. It's the
deterministic, non-Monte-Carlo counterpart to `sim backtest`; the two share one
day-walk, so their daily counts always agree.

```bash
forecast history -input linear.db -teams ENG -project "Q3 Migration" -format text
```

| Flag | Default | Description |
|---|---|---|
| `-project` | all projects | exact project name to scope to |
| `-milestone` | all milestones | exact milestone name within `-project` |
| `-start` | earliest `created_at` in scope | first day emitted, inclusive |
| `-end` | `today` | last day emitted, inclusive |
| `-window` | `28` | trailing window in days for rolling metrics (throughput, scope growth, net flow) |
| `-format` | `csv` | `csv`, `json`, or `text` |
| `-out` | stdout | write output to this file |

Without `-project` it emits one row per day across everything loaded, not one
series per project.

### `cfd` — cumulative flow diagram

Builds a four-line, three-band CFD (Created, LeftBacklog, Departed, Completed)
plus flow-health stats: throughput, average WIP, cycle time, a Little's Law
cross-check, and per-band stability. The default output is an interactive
HTML chart; `json` gives the daily series.

```bash
forecast cfd -input linear.db -teams ENG -start 2025-01-01 -end 2025-07-01 -out cfd.html
```

| Flag | Default | Description |
|---|---|---|
| `-start` | `-3 months` | start date, inclusive |
| `-end` | `today` | end date, inclusive |
| `-format` | `html` | `html` or `json` |
| `-out` | stdout | write output to this file |

### `aging` — which in-progress items are old?

Builds the historical cycle-time distribution (`completed_at - started_at`)
from recently completed issues, then ranks every in-progress issue against it.
Each row gets a `PERCENTILE` (where its age falls in that distribution) and a
`MULTIPLIER`: its age divided by the cycle time at `-percentile`. At the
default 85, `1.34x` means "34% older than the 85th-percentile cycle time" and
`0.80x` means "20% younger than it." Row colors in text/HTML output follow the multiplier, so
a cell's color always matches the number printed in it.

```bash
forecast aging -input linear.db -format html > aging.html
```

| Flag | Default | Description |
|---|---|---|
| `-sample-start` | `-3 months` | start of the completed-issue window |
| `-sample-end` | `today` | end of the window (exclusive, so today's completions are not included) |
| `-percentile` | `85` | which percentile of the distribution to anchor `MULTIPLIER` to (1–100) |
| `-min-cycle-time` | | drop completed issues faster than this, e.g. `5m`, `1h`, `1d` |
| `-show-completed` | `false` | text/HTML: also list the completed issues that make up the distribution |
| `-format` | `text` | `text`, `json`, or `html` (`json` only ever lists in-progress items) |

### `count` — outstanding work

Counts issues not in a terminal state (not `completed`/`canceled`/`duplicate`)
and groups them by project, most recently updated first. When you don't pass
`-teams` and your data spans several teams, a `TEAM` column is added.

```bash
forecast count -input linear.db -milestones
```

| Flag | Default | Description |
|---|---|---|
| `-milestones` | `false` | add a per-milestone breakdown under each project |
| `-updated-since` | `-3 months` | hide projects whose most recently updated issue is older than this |

## Forecasting with `sim`

`forecast sim` resamples your team's real daily completion history to run
thousands of simulated futures. There are four subcommands, all built on the
same sampling setup.

A forecast is only as good as its sample window. It assumes the future will
look like that window (same team, same mix of work, roughly same-sized items)
and counts only completed issues with an assignee. If the team or the work has
changed, narrow `-sample-start`/`-sample-end` to a period that resembles what
you're forecasting. Use `sim backtest` to see how well forecasts held up
before you rely on them.

### Choosing how to model the team

Pick **exactly one** of these; there is no default.

| Flag | Slots | Each slot draws from | Per-engineer exclusions |
|---|---|---|---|
| `-engineers 3` | 3 anonymous | everyone's pooled history | `global` only |
| `-engineers alice,bob,carol` | 3 named | everyone's pooled history | yes |
| `-whole-team` | 1 | the summed whole-team series | `global` only |

`-engineers` takes a count or a list of names. Names change nothing about the
statistics (every engineer is still an interchangeable draw from the pool), but
they let [per-engineer exclusions](#exclusionsjson--holidays-and-time-off) attach
to a specific person. A name doesn't have to appear in your data, so a new hire
with no history is fine. `-typical-engineers` independently decides whose
history forms the pool.

There is deliberately no mode that draws each person from their own history.
Over a typical 3-month window one engineer's series is about 90 mostly-zero
samples, which makes the forecast lumpy and hypersensitive to who you typed, and
invites "what if Alice took this instead of Bob?" comparisons the data can't
support. To model a team that really differs from the whole, narrow the pool
with `-typical-engineers`, or use `-whole-team`.

(`sim` pools every team; `-teams`, the issue-tracker team filter, is a separate
flag on other commands.)

### Sampling flags (all four subcommands)

| Flag | Default | Description |
|---|---|---|
| `-sample-start` | `-3 months` | start of the history to sample from |
| `-sample-end` | `now` | end of the history (exclusive). `now` includes today's completions so far. |
| `-simulations` | `10000` | Monte Carlo trials (for `backtest`: trials per backtested day) |
| `-random-seed` | time-based | fix this to make a run reproducible |
| `-typical-engineers` | all | restrict the sample pool to these engineers' history |
| `-exclusions` | *(none)* | [exclusions file](#exclusionsjson--holidays-and-time-off) |
| `-goroutines` | CPU count | parallel workers |
| `-manifest` | | write a run-provenance JSON file (`-` for stdout); on `items`, `days`, `probability` |

### Confidence, probability, percentile

Three different jobs; don't conflate them.

| Term | Kind | Meaning |
|---|---|---|
| **Confidence** | an input you choose | A safety level, always read toward the conservative side. `sim items -confidence 85` → "a floor I'll hit or beat 85% of the time" (fewer items). `sim days -confidence 85` → "a ceiling I'll finish within 85% of the time" (more days). Same 85, opposite arithmetic. |
| **Probability** | an output | What `sim probability` computes for a plan you already have. It's the exact inverse of confidence: if `sim items -confidence 85` says "at least 40", then `sim probability -items 40` reports about 85%. |
| **Percentile** | a plain rank | Used where there's nothing to commit to, e.g. `aging`, where a higher percentile means "older" (a warning). |

### `sim items` — how many items in D days?

```bash
forecast sim items -input linear.db -engineers 2 -days 30
```

| Flag | Default | Description |
|---|---|---|
| `-days` | | length of the forecast window; give this **or** `-target-end-date` |
| `-target-start-date` | `tomorrow` | first day of the forecast window |
| `-target-end-date` | | last day of the window, inclusive; give this **or** `-days` |
| `-confidence` | `50,75,85,95` | confidence levels to report |

### `sim days` — how many days for I items?

```bash
forecast sim days -input linear.db -whole-team -items 50
```

| Flag | Default | Description |
|---|---|---|
| `-items` | *(required)* | items to complete; comma-separated for a grouped trajectory (e.g. `13,12,9`) |
| `-target-start-date` | `tomorrow` | start date used to turn day counts into calendar dates |
| `-confidence` | `50,75,85,95` | confidence levels to report |

### `sim probability` — how likely is this plan?

```bash
forecast sim probability -input linear.db -engineers 4 -days 30 -items 40
```

| Flag | Default | Description |
|---|---|---|
| `-days` | | length of the window; give this **or** `-target-end-date` |
| `-target-start-date` | `tomorrow` | start of the target window |
| `-target-end-date` | | last day of the window, inclusive; give this **or** `-days` |
| `-items` | all | items to complete; leave off to see the full distribution |

### `sim backtest` — would past forecasts have held up?

Replays `sim probability`-style forecasts against a project's actual history,
one row per day from the replay start to your deadline, so you can see how the
forecast evolved and whether it was calibrated. Rows after today are
projections that assume no further completions, marked by a divider in `text`
output and a `projected` column in `csv`.

```bash
forecast sim backtest -input linear.db -whole-team -project "Q3 Migration" -target-end-date 2025-09-30
```

| Flag | Default | Description |
|---|---|---|
| `-project` | *(required)* | project to backtest |
| `-milestone` | | milestone within the project |
| `-replay-start-date` | earliest `started_at` in the issue set | first day to replay, inclusive |
| `-target-end-date` | *(required)* | completion deadline to forecast against |
| `-format` | `text` | `text` or `csv` |

[Exclusions](#exclusionsjson--holidays-and-time-off) apply per replayed day: on
each day, the ones between that day and the deadline reduce the forecast just
as they would have then.

### `exclusions.json` — holidays and time off

Pass `-exclusions <path>` to tell `sim` that nobody works on certain dates, or
that one engineer doesn't. There is no default file: without the flag nothing is
excluded, and a path that doesn't exist is an error.

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

Each entry is a `YYYY-MM-DD` day, an inclusive `YYYY-MM-DD/YYYY-MM-DD` range,
or an object with `date` (or `from` and `to`) and an optional `reason`. You can
mix forms freely. Every entry must parse: a malformed date, an inverted range,
an unknown key, or a range longer than 366 days stops the run rather than being
skipped. `global` entries apply to every engineer; `engineers` entries apply to
the engineer of that name.

One file serves both sides of a forecast. A date inside the sample window is
dropped from the history, so a holiday's zero isn't mistaken for a normal
zero-throughput day; a date inside the forecast window contributes no
completions, so `-days 30` is still 30 calendar days with some of them
unproductive. Which one a date affects depends only on where it falls, so the
file can be a long-lived company calendar plus PTO.

The repo ships a sample, [testdata/sample-exclusions.json](testdata/sample-exclusions.json),
matching the example above:

```bash
forecast sim items -input sample-issues.csv -engineers alice,bob -days 20 \
  -sample-start 2025-01-01 -sample-end 2025-04-01 -exclusions testdata/sample-exclusions.json
```

To check a file before relying on it, run
`forecast check -input sample-issues.csv -exclusions testdata/sample-exclusions.json`.

Per-engineer entries only take effect for engineers named in `-engineers
alice,bob`; with `-engineers N` or `-whole-team` only `global` entries apply.
`-whole-team` logs a warning if the file has per-engineer entries.

## Config files

Every command except `version` and `update` accepts `-config <file.yaml>`. Keys are flag names; precedence is **CLI flag > config
file > built-in default**. This is useful for a forecast you re-run weekly.

```yaml
# sim-items.yaml
input: linear.db
engineers: 4
days: 30
sample-start: "2025-01-01"
random-seed: 42
confidence: [50, 75, 85, 95]
```

```bash
forecast sim items -config sim-items.yaml              # uses every value above
forecast sim items -config sim-items.yaml -days 60     # the CLI's -days wins
```

- List flags (`-teams`, `-typical-engineers`, `-confidence`, `-items`) take a
  YAML list or a plain comma-separated string. So does `-engineers` when it
  holds names (`engineers: [alice, bob]`); `engineers: 4` is a count.
- Config values count as explicitly set, so `random-seed: 42` pins the seed
  exactly as `-random-seed 42` would.
- One config file serves one command; there's no per-command sectioning.
- The `config` key itself is ignored inside the file.
- In config files, prefer `input:` over the deprecated `db:`.

## Maintenance

**`forecast version`** prints the version (`(dev)` for local builds) plus git
SHA, build time, dirty flag, Go version, and module.

**`forecast update`** checks the latest GitHub release and, after confirming,
downloads it, verifies its checksum, and replaces the running binary.

```bash
forecast update -check   # report current vs. latest; install nothing
forecast update -yes     # install without prompting
```

| Flag | Default | Description |
|---|---|---|
| `-check` | `false` | report current vs. latest and exit |
| `-yes` | `false` | skip the confirmation prompt |
| `-force` | `false` | proceed even if already current, or the running build is a dev build |
| `-timeout` | `60s` | overall HTTP timeout |

## Using as a Go library

The `simulate`, `aging`, `cfd`, `counts`, and `history` packages are pure and
independent of Linear and SQLite, so you can import them on their own, e.g.
`github.com/commondatageek/delivery-forecast/simulate`. The `issues` package
provides the shared `Issue` record plus `ReadCSV` / `ReadJSON` / `ReadFile`.
[DATA_REQUIREMENTS.md](https://github.com/commondatageek/delivery-forecast/blob/main/DATA_REQUIREMENTS.md) describes what each package needs
and includes a short usage example.

## Building from source

Uses [Just](https://just.systems), or plain Go:

```bash
just build       # compiles bin/forecast
just test        # go test ./...

go build -o bin/forecast ./cmd/forecast
```

## License

Copyright 2026 Aaron Johnson.

Licensed under the Apache License, Version 2.0. See [LICENSE](https://github.com/commondatageek/delivery-forecast/blob/main/LICENSE) for the
full text.
