# sim

Monte Carlo throughput simulator. Reads historical issue completion data from
the SQLite store (`linear.db`, populated by `sync`) and runs simulations to
answer three types of questions.

## Build

```sh
go build -o sim .
```

## Input

Reads completed issues from the `issues` table of a SQLite database (default
`linear.db`), built by the `sync` command. Point at a different file with `-db`.

## Subcommands

### `items` — how many items can N engineers complete in D days?

```sh
./sim items \
  -db linear.db \
  -engineers 4 \
  -days 30 \
  -simulations 10000 \
  -sample-start 2025-02-01 \
  -sample-end 2025-08-01 \
  -percentile 50,75,90,95 \
  -include "alice,bob,carol"
```

| Flag | Default | Description |
|---|---|---|
| `-db` | `linear.db` | Path to the SQLite database |
| `-engineers` | `3` | Number of engineers |
| `-days` | `30` | Number of days to simulate |
| `-simulations` | `10000` | Number of Monte Carlo simulations |
| `-sample-start` | 6 months ago | Start of historical sample window (YYYY-MM-DD) |
| `-sample-end` | today | End of historical sample window (YYYY-MM-DD) |
| `-percentile` | `5,10,...,95` | Comma-separated percentiles to output |
| `-include` | all | Comma-separated engineer names to include |
| `-manifest` | (disabled) | Write a run-provenance JSON manifest to this path (`-` for stdout) |

### `days` — how many days for N engineers to complete I items?

```sh
./sim days \
  -db linear.db \
  -engineers 4 \
  -items 80 \
  -simulations 10000 \
  -sample-start 2025-02-01 \
  -sample-end 2025-08-01 \
  -percentile 50,75,90,95 \
  -include "alice,bob,carol"
```

| Flag | Default | Description |
|---|---|---|
| `-db` | `linear.db` | Path to the SQLite database |
| `-engineers` | `3` | Number of engineers |
| `-items` | `50` | Number of items to complete |
| `-simulations` | `10000` | Number of Monte Carlo simulations |
| `-sample-start` | 6 months ago | Start of historical sample window (YYYY-MM-DD) |
| `-sample-end` | today | End of historical sample window (YYYY-MM-DD) |
| `-percentile` | `5,10,...,95` | Comma-separated percentiles to output |
| `-include` | all | Comma-separated engineer names to include |
| `-start-date` | today | Report start date for the grouped trajectory report (YYYY-MM-DD) |
| `-manifest` | (disabled) | Write a run-provenance JSON manifest to this path (`-` for stdout) |

#### Grouped trajectory report

Pass `-items` a comma-separated list to forecast a *sequence* of work groups
(e.g. milestones) completed in order, instead of a single total:

```sh
./sim days \
  -db linear.db \
  -engineers 2 \
  -items 13,12,9,5,2 \
  -start-date 2026-06-17 \
  -percentile 5,25,50,85,95
```

```
2 equivalent engineers, starting 2026-06-17 -> grouped trajectory

Group    Items  p5 Days  p5 Date     ...  p95 Days  p95 Date
Group 1  13     4        2026-06-21  ...  8         2026-06-25
Group 2  25     4        2026-06-25  ...  6         2026-07-01
...
Total    41     14       2026-07-01  ...  21        2026-07-08
```

Each group's `Days` is the marginal days to finish that group after all
earlier groups; its `Date` is `-start-date` plus the cumulative days through
that group. The `Total` row's `Days`/`Date` are for the full list. A single
value (the default) keeps the original one-line-per-percentile output.

### `probability` — what is the probability of completing I items in D days?

```sh
./sim probability \
  -db linear.db \
  -engineers 4 \
  -days 30 \
  -items 80 \
  -simulations 10000 \
  -sample-start 2025-02-01 \
  -sample-end 2025-08-01 \
  -include "alice,bob,carol"
```

| Flag | Default | Description |
|---|---|---|
| `-db` | `linear.db` | Path to the SQLite database |
| `-engineers` | `3` | Number of engineers |
| `-days` | `30` | Number of days to simulate |
| `-items` | `50` | Number of items to complete |
| `-simulations` | `10000` | Number of Monte Carlo simulations |
| `-sample-start` | 6 months ago | Start of historical sample window (YYYY-MM-DD) |
| `-sample-end` | today | End of historical sample window (YYYY-MM-DD) |
| `-include` | all | Comma-separated engineer names to include |
| `-manifest` | (disabled) | Write a run-provenance JSON manifest to this path (`-` for stdout) |

## Run manifest

All three subcommands accept `-manifest <path>` (`-manifest -` for stdout) to
write a JSON document capturing every input that fed the run: the binary's
git SHA/dirty state, every flag (and whether it was explicitly set), the
resolved sampling mode/seed/sample window, a sha256 fingerprint of the DB
file, the applied exclusions, the built sample pool's per-engineer totals,
and a full dump of the completed issues the pool was built from. It's opt-in
and off by default. Use it to diagnose why two runs disagree — diff two
manifests and the difference (a different DB snapshot, a different seed, a
narrower sample window, an engineer with no completions) shows up directly.
