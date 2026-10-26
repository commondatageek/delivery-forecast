# Implementation plan: extract business logic out of `cmd/forecast`

Status: **done.** All seven phases (0–6) are implemented, one `jj` commit each
on the `app-layer-extraction` bookmark. The golden tests from Phase 0 pass
unchanged against the final tree.

Deviations from the plan as written:

- `grep` audits: the literal §6.1 patterns matched a few prose comments
  (the `internal/app` package doc and `BlendingTeamsWarning`'s comment, plus
  two comments in `cmd/forecast`). Those comments were reworded so the audits
  print nothing; no code needed to change.
- `writeSimManifest` (CLI) wraps `writeManifest` and returns early when
  `-manifest` is empty, so the new `source.Fingerprint` (which hashes the
  input file) is not computed on runs that don't write a manifest, matching
  the old behaviour.
- `HistoryResult`'s inferred-start check keeps the old
  `LocalDay(EarliestCreatedAt(...))` then `IsZero` ordering verbatim.
- The `sim days` progress bar is created only on the single-value path, as
  the plan specified; trajectory runs leave `Progress` nil.

## Why this exists

We want to add a web GUI (an HTML frontend with a Go backend) that can do
everything the CLI can do. Today it can't do that without copying code. The
analysis packages (`simulate`, `aging`, `cfd`, `counts`, `history`) are clean
and reusable. But the code that wires them together lives in `package main`
under `cmd/forecast`, and no other Go package can import `package main`.

That wiring code is real business logic. It covers:

- choosing which issues feed each analysis;
- converting `issues.Issue` into each package's input struct;
- defaults and validation;
- computing result tables (confidence rows, trajectory groups, probability
  tables, backtest "projected" flags);
- warnings, which today are written straight to stderr through `logx`.

This plan moves all of it into two new packages:

- **`internal/app`**: pure application logic. It takes typed requests and
  returns typed results, plus warnings as data. It does no IO and never calls
  `time.Now()`.
- **`internal/source`**: loading issues and exclusions from disk or stdin.

After the move, `cmd/forecast` is a thin adapter: parse flags, build a
request, call `app`, render the result. The future web server will be a
second thin adapter over the same `app` calls. The GUI is **not** part of this
plan; §9 sketches it only so you can see why the API is shaped the way it is.

---

## 0. How to use this document

**Rules for whoever implements this:**

1. **Work one phase at a time, in order.** Each phase must compile and pass
   `just test` before the next one starts. Never begin Phase N+1 while Phase N
   is red.
2. **Commit at the end of each phase** with `jj` (see §1). One phase = one
   commit. Don't squash phases together.
3. **Write the tests each phase lists.** They're required, not optional.
4. **Do not relitigate the decisions in §2.** If you think one is wrong, stop
   and raise it with the user. Don't quietly do something else.
5. **Don't do work assigned to a later phase**, even when it would be
   convenient.
6. **The golden tests from Phase 0 are the contract.** After Phase 0, never run
   them with `-update`. If a golden test fails, your change altered CLI
   behaviour: fix the change, not the golden file. The only allowed behaviour
   changes are listed in §3. None of them touch an existing golden file.
7. If you can't meet a phase's acceptance criteria as written, stop and report
   what's blocking you. Don't improvise a different design.
8. **Move code; don't rewrite it.** When this plan says "move X", cut the
   function body and paste it in its new home, changing only what the plan
   says to change (signature, return values, `logx` calls becoming returned
   warnings). Keep comments, keep the order of operations, and keep error
   strings byte-for-byte identical. Many existing tests and the golden files
   compare exact strings.

**Repo conventions to follow** (they already exist; match them):

- Root packages (`simulate`, `aging`, `cfd`, `counts`, `history`, `issues`) are
  pure and IO-free. `internal/app` follows the same rule.
- Packages inside the module may import `internal/util`, and `internal/app`
  will.
- Every subcommand calls `util.ApplyConfig` right after `fs.Parse`. That stays
  in `cmd/forecast`.
- User date expressions (`"-3 months"`, `"tomorrow"`, `"now"`) are parsed with
  `util.ParseFlexibleDate` / `util.ParseFlexibleStartDate`. That parsing stays
  in the adapters (CLI today, web later). `internal/app` only ever sees
  `time.Time`.
- Match the surrounding code: comment density, naming, doc comments on every
  exported identifier.
- Run `gofmt -l .` before every commit; it must print nothing.

---

## 1. Version control: `jj`, not git

This repo is a colocated **Jujutsu** repo (`.jj/` and `.git/` both exist).
Use `jj` for every mutation. Never run `git add`, `git commit`,
`git checkout` or `git rebase`; they desync jj's view of the repo.

- The working copy is itself a commit (`@`), and jj snapshots it automatically
  at the start of every `jj` command. There is no staging area.
- Never run a jj command that opens an editor or TUI. Always pass `-m` to
  `jj describe` / `jj commit`. Never use `-i`, `--interactive`, `--tool`,
  bare `jj split`, or `jj resolve`.
- Work happens on the bookmark **`app-layer-extraction`**. It branches off
  `main`, and its first commit adds this document.

**At the end of each phase:**

```bash
gofmt -l .                      # must print nothing
just test                       # must pass
jj status                       # review what changed
jj commit -m "app layer: Phase N — <short summary>"
jj bookmark set app-layer-extraction -r @-
jj log -n 5                     # confirm the bookmark sits on your new commit
```

`jj commit` describes the current change and starts a new empty one on top.
Bookmarks don't move on their own, which is why `jj bookmark set` follows it.
**Don't push** (`jj git push`) unless the user asks you to.

If you make a mistake, `jj undo` reverts the last jj operation, and
`jj op log` lists earlier ones.

---

## 2. Decisions (binding)

**D1. Two new packages: `internal/app` and `internal/source`.** `app` is pure.
`source` does the IO (files, SQLite, stdin). `app` must never import
`source`, `internal/sqlite`, `internal/linear`, `internal/logx`, `flag`, or
`os`. `source` may import `app` (it needs one type, `app.DataFile`, in
Phase 5).

**D2. `internal/app` takes `[]issues.Issue`, not a path.** The caller loads
issues (through `source`) and passes them in. The web server will load once
and cache, so it must be able to call `app` many times against the same slice
without reloading.

**D3. Every result struct has `Warnings []string`.** Each warning is the exact
text the CLI logs today, without the `WARN ` prefix (`logx` adds that). The
CLI prints every warning with `logx.Warnf("%s", w)`, in order, and then
renders the result.

**D4. An `app` run function always returns a non-nil result pointer, even
when it also returns an error.** Its `Warnings` are valid either way; every
other field is meaningful only when `err == nil`. The CLI prints warnings
**before** checking the error. This keeps today's behaviour where, for
example, "typical engineer not found" is printed just before a "no completed
items" error. Example:

```go
res, err := app.SimItems(req)
for _, w := range res.Warnings {
	logx.Warnf("%s", w)
}
if err != nil {
	return err
}
```

**D5. Requests carry resolved, typed values.** That means `time.Time`, not
`"-3 months"`; a concrete seed, not "seed if the flag was set"; and
`Days int` plus `TargetEnd time.Time` rather than flag-presence checks. The
mapping is:

- **Unset:** a zero value means "not given". `Days == 0` is unset, a zero
  `TargetEnd` is unset, and a zero `ReplayStart` / history `Start` means
  "infer it".
- **Seed:** the CLI keeps `resolveSeed` and passes the result in.
- **Now/today:** anything that needs "today" or "now" takes it as a request
  field (`Today time.Time`). `app` never calls `time.Now()`.

**D6. Each request type has a `Validate() error` method.** It checks
everything that doesn't need `Issues` (mode, mutually exclusive inputs,
ranges, required fields). Each run function calls `req.Validate()` first. The
CLI also calls `req.Validate()` itself, before loading issues, so a bad flag
fails fast without reading a large database.

**D7. Errors that blame a specific input are `*app.FieldError`.**

```go
// FieldError is a validation error attributable to one or more request
// fields. Fields holds the CLI flag names (== -config YAML keys) without the
// leading dash, e.g. []string{"days", "target-end-date"}. Error returns Msg
// unchanged, so CLI output is identical to the pre-refactor plain errors.
type FieldError struct {
	Fields []string
	Msg    string
}

func (e *FieldError) Error() string { return e.Msg }
```

Use it for every error that `Validate()` returns, and for the run-time errors
the table in each phase marks as field errors. Keep `Msg` exactly the string
used today (it mentions flags, like `-days`; that's fine). The web layer will
use `Fields` to highlight form inputs. Errors that aren't about an input
(`ValidatePool` failures, invariant violations) stay plain errors and are
returned unchanged.

**D8. Rendering stays where it is.** The `Render*` functions in
`aging`/`cfd`/`counts`/`history` don't move. The CLI's own `fmt.Printf` /
`tabwriter` code for the sim reports, backtest text/CSV, check output and
`describeWindow` stays in `cmd/forecast`, but it now reads from result
structs instead of computing values inline.

**D9. Conversion adapters move to `app`.** `toAgingIssues`, `toCFDIssues`,
`toHistoryIssues`, `toCountsIssues`, `issuesToCompletions` and
`issuesToBacktestItems` become unexported functions in `internal/app`. The
analysis packages still know nothing about `issues.Issue`. `app` is the one
place that knows both sides, which replaces the `cmd/forecast` role that
CLAUDE.md describes today.

**D10. Rule for where logic goes.** Logic that needs `issues.Issue` (issue
selection, `IsCompleted`/`IsCanceled`/`IsInProgress`, team filtering) goes in
`internal/app`. Logic that works only on a package's own neutral structs goes
in that package. Flag parsing, config, progress bars, printing and file IO
stay in `cmd/forecast` or `internal/source`.

**D11. Shared defaults become constants in `app`.** The CLI flag
definitions must reference them, so the GUI shows the same defaults:

```go
const (
	DefaultSimulations      = 10_000
	DefaultAgingPercentile  = 85
	DefaultHistoryWindow    = 28
)

// DefaultConfidences returns a fresh copy of the default confidence levels
// for sim items/days (a fresh slice so callers can't mutate a shared one).
func DefaultConfidences() []int { return []int{50, 75, 85, 95} }
```

Don't touch any other flag default (date-expression defaults, `count`'s
`defaultSince`, `-goroutines`); that's out of scope.

**D12. No new behaviour except §3.** Don't add validations, change messages,
reorder output, or "fix" anything you notice along the way. Write such things
down in your phase report instead.

---

## 3. Allowed behaviour changes (the complete list)

1. **Error precedence when several inputs are wrong at once may change.**
   Example: today, a run with no mode *and* a malformed `-sample-start`
   reports the mode error first. Afterwards it may report the date error
   first, because the CLI parses dates before it calls `Validate()`. Any
   run with only one error must report exactly the same message as before.
2. **`sim items/probability -days 0`** (passed explicitly) used to fail with
   `-days must be positive`. Because of D5 it now fails with
   `one of -days or -target-end-date must be provided`. Negative values still
   fail with `-days must be positive`.
3. **`sim days -confidence` now gets the same `(0, 100]` range check that
   `sim items` already has**, with the same message format:
   `-confidence: values must be in (0, 100], got %d`. Today `sim days` doesn't
   check at all.
4. **Exclusions-file read errors** now surface after the input file loads but
   before any typical-engineer warning. Today they come after that warning.
   This is a consequence of (1) and D2.
5. **(Accepted after the fact.) The `-manifest` flag record for an unset
   `-confidence` now reads `"value": ""`** instead of `"50,75,85,95"`. The old
   CLI wrote the defaults back into the flag variable before the manifest
   recorded it; defaults are now applied in `internal/app`, leaving the flag
   untouched. The levels actually used are still in
   `resolved.extra.effective_confidence_levels`, and replaying from `flags`
   resolves to the same defaults.

---

## 4. Target layout

```
internal/source/
  source.go          Load (was cmd/forecast loadIssues), LoadExclusions (was loadExclusions)
  fingerprint.go     Fingerprint (was dbFingerprint)                         [Phase 5]
  source_test.go
internal/app/
  doc.go             package doc: purity rules, D3/D4/D5 conventions
  errors.go          FieldError
  defaults.go        D11 constants
  convert.go         to*Issues / issuesTo* adapters (unexported)
  teams.go           DistinctTeamKeys, BlendingTeamsWarning
  window.go          resolveTargetWindow (unexported)
  simpool.go         SimBase, SimRun, preparePool, ParseEngineers wrapper
  sim_items.go       ItemsRequest/Result, SimItems
  sim_days.go        DaysRequest/Result, SimDays
  sim_probability.go ProbabilityRequest/Result, SimProbability
  sim_backtest.go    BacktestRequest/Result, SimBacktest                      [Phase 3]
  aging.go cfd.go count.go history.go                                         [Phase 4]
  check.go manifest.go                                                        [Phase 5]
  *_test.go
```

Nothing under `internal/app` may reference `os.Stdout`, `os.Stderr`,
`os.Args`, `os.Getwd`, `time.Now`, `logx`, or `flag`. Phase 6 checks this with
grep.

---

## Phase 0: freeze CLI behaviour with golden tests

**Goal:** before anything moves, capture the exact stdout, stderr and error of
a fixed set of CLI invocations, so every later phase can prove it changed
nothing.

### 0.1 Inject the clock

In `cmd/forecast/common.go`, add:

```go
// nowFunc is the CLI's clock. Tests replace it to make date defaults
// ("now", "today", "-3 months") and aging/backtest "today" deterministic.
var nowFunc = time.Now
```

Replace **every** `time.Now()` in non-test files under `cmd/forecast` with
`nowFunc()`. Today these are at:

- `aging.go` (`now := time.Now()`)
- `backtest.go`
- `cfd.go`
- `check.go` (inside `util.LocalDay(time.Now())`)
- `count.go`, twice (`defaultSince` and the `ParseFlexibleDate` call)
- `history.go`
- `manifest.go` (`GeneratedAt`)
- `sim.go`, three times

Verify with `grep -n "time.Now()" cmd/forecast/*.go | grep -v _test.go`,
which must print nothing. Leave `resolveSeed` alone: it receives `now` as a
parameter, and seeds are pinned with `-random-seed` in every golden case.

### 0.2 Fixtures

Create `cmd/forecast/testdata/golden/`. The root `.gitignore` ignores
`*.json` and `*.html` everywhere except root `testdata/*.json`, so first add
this line as the **last line** of `.gitignore`:

```
!cmd/forecast/testdata/**
```

It must be last. Gitignore rules are applied in order, and the later
`*.html` rule would otherwise re-ignore files the negation un-ignored.
(This was verified while the plan was written.)

Name every golden **output** file with the `.golden` extension; never
`.json` or `.html`.

Create `cmd/forecast/testdata/golden/multiteam.csv` with exactly:

```csv
identifier,team_key,team_name,assignee,project_name,project_milestone_name,state_type,created_at,started_at,completed_at,canceled_at,updated_at
ENG-1,ENG,Engineering,alice,Search Revamp,Indexing,completed,2025-01-02,2025-01-03,2025-01-08,,2025-01-08
ENG-2,ENG,Engineering,bob,Search Revamp,Indexing,completed,2025-01-06,2025-01-07,2025-01-15,,2025-01-15
ENG-3,ENG,Engineering,,Search Revamp,,completed,2025-01-10,2025-01-12,2025-01-20,,2025-01-20
ENG-4,ENG,Engineering,alice,Search Revamp,Ranking,started,2025-02-01,2025-02-03,,,2025-03-01
ENG-5,ENG,Engineering,bob,,,backlog,2025-02-10,,,,2025-02-10
DATA-1,DATA,Data Platform,dana,Warehouse,,completed,2025-01-05,2025-01-06,2025-01-14,,2025-01-14
DATA-2,DATA,Data Platform,erin,Warehouse,Ingest,completed,2025-01-09,2025-01-10,2025-01-21,,2025-01-21
DATA-3,DATA,Data Platform,dana,Warehouse,Ingest,completed,2025-01-20,2025-01-22,2025-02-04,,2025-02-04
DATA-4,DATA,Data Platform,erin,Warehouse,Ingest,started,2025-02-05,2025-02-07,,,2025-03-03
DATA-5,DATA,Data Platform,dana,Warehouse,,canceled,2025-02-06,,,2025-02-12,2025-02-12
DATA-6,DATA,Data Platform,,,,backlog,2025-02-20,,,,2025-02-20
DATA-7,DATA,Data Platform,erin,Warehouse,Ingest,completed,2025-02-10,2025-02-11,2025-03-06,,2025-03-06
```

Create `cmd/forecast/testdata/golden/exclusions.json`:

```json
{
  "global": ["2025-02-17", {"from": "2025-03-24", "to": "2025-03-28", "reason": "offsite"}],
  "engineers": {
    "alice": ["2025-02-03/2025-02-07", {"date": "2025-03-20", "reason": "PTO"}],
    "zed": ["2025-03-18"]
  }
}
```

Create `cmd/forecast/testdata/golden/bad-exclusions.json`:

```json
{"global": ["not-a-date"]}
```

The main fixture is the existing `testdata/sample-issues.csv` at the repo
root. Tests run with cwd `cmd/forecast`, so reference it as
`../../testdata/sample-issues.csv`.

### 0.3 The harness: `cmd/forecast/golden_test.go`

Requirements, in order:

1. `var updateGolden = flag.Bool("update", false, "rewrite golden files")` at
   package level. (`go test` parses test flags before tests run.)
2. `TestGolden` iterates a table of cases:
   `{name string; run func([]string) error; args []string}`.
3. Before the loop, and restored with `t.Cleanup`:
   - `nowFunc = func() time.Time { return time.Date(2025, 3, 15, 10, 30, 0, 0, time.Local) }`
   - `time.Local = time.FixedZone("GOLDEN", -5*60*60)`. Set this **before**
     building the `nowFunc` value, so it's evaluated in that zone. Swapping
     `time.Local` makes the goldens independent of the machine's timezone.
     This is safe because no test in this package calls `t.Parallel()`; keep
     it that way.
4. For each case, run it as a subtest `t.Run(c.name, ...)` that captures
   **stdout and stderr at the same time** while calling `c.run(c.args)`:
   - swap `os.Stdout` and `os.Stderr` for the write ends of two `os.Pipe()`s;
   - **drain both read ends in goroutines while the command runs.** Some
     outputs (HTML) are bigger than the OS pipe buffer, and reading only
     after the command returns would deadlock. Don't reuse the existing
     `captureStdout` helper for this; it reads afterwards;
   - restore the originals, close the write ends, then wait for both
     goroutines.
5. Build one combined text document per case and compare it with
   `testdata/golden/<name>.golden`:

   ```
   ==== stdout
   <stdout bytes>
   ==== stderr
   <stderr bytes>
   ==== error
   <err.Error(), or the literal text "<nil>">
   ```

   With `-update`, write the file. Otherwise read it and, on mismatch, fail
   with a message that names the file and shows both texts. A simple
   line-by-line first-difference report is enough.
6. Every sim case pins `-random-seed 42` **and** `-goroutines 4`.
   Reproducibility depends on the `(seed, workers)` pair; see
   `simulate.RunSimulations`. Every sim case also passes `-simulations 2000`
   to keep the test fast.
7. Never use `-out` or `-manifest` in golden cases. File outputs and
   manifests (which contain `generated_at`, cwd and git SHA) are covered by
   their own unit tests.

stderr will include the `INFO effective flag values for this run:` table that
`util.ApplyConfig` logs. That's expected and deterministic; keep it in the
golden files.

### 0.4 The cases

Shorthand used below (expand it literally in the Go table):

- `S` = `../../testdata/sample-issues.csv`
- `M` = `testdata/golden/multiteam.csv`
- `X` = `testdata/golden/exclusions.json`
- `B` = `testdata/golden/bad-exclusions.json`
- `SIM` = `-sample-start 2025-01-01 -sample-end 2025-03-15 -simulations 2000 -random-seed 42 -goroutines 4`

All of these were run against the current binary while this plan was written.
They all succeed, or fail with the error noted.

| name | run | args |
|---|---|---|
| `sim-items-count` | `cmdSimItems` | `-input S -engineers 3 SIM -target-start-date 2025-03-16 -days 30` |
| `sim-items-defaults` | `cmdSimItems` | `-input S -engineers 3 -sample-start 2025-01-01 -simulations 2000 -random-seed 42 -goroutines 4 -days 30` (uses the clock: `-sample-end now`, `-target-start-date tomorrow`) |
| `sim-items-named-exclusions` | `cmdSimItems` | `-input S -engineers alice,bob -exclusions X SIM -target-start-date 2025-03-16 -target-end-date 2025-04-30 -confidence 50,90` (warns about `zed`) |
| `sim-items-wholeteam-exclusions` | `cmdSimItems` | `-input S -whole-team -exclusions X SIM -target-start-date 2025-03-16 -days 30` (warns that per-engineer entries are ignored) |
| `sim-items-typical-unknown` | `cmdSimItems` | `-input S -engineers 2 -typical-engineers alice,zed SIM -target-start-date 2025-03-16 -days 30` (warns about `zed`) |
| `sim-days-single` | `cmdSimDays` | `-input S -engineers 2 SIM -target-start-date 2025-03-16 -items 10` |
| `sim-days-trajectory` | `cmdSimDays` | `-input S -engineers 2 SIM -target-start-date 2025-03-16 -items 5,4,3 -confidence 50,85` |
| `sim-days-exclusions` | `cmdSimDays` | `-input S -engineers alice,bob -exclusions X SIM -target-start-date 2025-03-16 -items 8` |
| `sim-prob-items` | `cmdSimProbability` | `-input S -engineers 3 SIM -target-start-date 2025-03-16 -days 21 -items 12` |
| `sim-prob-table` | `cmdSimProbability` | `-input S -whole-team SIM -target-start-date 2025-03-16 -days 14` |
| `sim-backtest-text` | `cmdSimBacktest` | `-input S -engineers 3 SIM -project "Search Revamp" -target-end-date 2025-03-31` (replay start inferred; shows the "today" divider) |
| `sim-backtest-csv` | `cmdSimBacktest` | `-input S -engineers 3 SIM -project "Search Revamp" -milestone Indexing -replay-start-date 2025-02-01 -target-end-date 2025-03-31 -format csv` |
| `sim-backtest-exclusions` | `cmdSimBacktest` | `-input S -engineers alice,bob,carol -exclusions X SIM -project "Search Revamp" -replay-start-date 2025-03-01 -target-end-date 2025-04-15` |
| `aging-text` | `cmdAging` | `-input S -sample-start 2025-01-01 -sample-end 2025-03-15` |
| `aging-show-completed` | `cmdAging` | `-input S -sample-start 2025-01-01 -sample-end 2025-03-15 -show-completed -percentile 70 -min-cycle-time 1d` |
| `aging-json` | `cmdAging` | `-input S -sample-start 2025-01-01 -sample-end 2025-03-15 -format json` |
| `aging-html` | `cmdAging` | `-input S -sample-start 2025-01-01 -sample-end 2025-03-15 -format html -show-completed` |
| `aging-defaults` | `cmdAging` | `-input S` (clock-dependent default window) |
| `aging-multiteam` | `cmdAging` | `-input M -sample-start 2025-01-01 -sample-end 2025-03-15` (blending warning) |
| `aging-multiteam-scoped` | `cmdAging` | `-input M -sample-start 2025-01-01 -sample-end 2025-03-15 -teams eng` (lower-case on purpose: tests upper-casing) |
| `cfd-json` | `cmdCFD` | `-input S -start 2025-01-01 -end 2025-03-15 -format json` |
| `cfd-html` | `cmdCFD` | `-input S -start 2025-01-01 -end 2025-03-15` |
| `cfd-multiteam` | `cmdCFD` | `-input M -start 2025-01-01 -end 2025-03-15 -format json` (blending warning) |
| `cfd-multiteam-scoped` | `cmdCFD` | `-input M -start 2025-01-01 -end 2025-03-15 -format json -teams DATA` |
| `count-summary` | `cmdCount` | `-input S -updated-since 2025-01-01` |
| `count-milestones` | `cmdCount` | `-input S -updated-since 2025-01-01 -milestones` |
| `count-multiteam` | `cmdCount` | `-input M -updated-since 2025-01-01` (TEAM column plus warning) |
| `count-multiteam-grouped` | `cmdCount` | `-input M -updated-since 2025-01-01 -milestones` |
| `count-defaults` | `cmdCount` | `-input S` (clock-dependent `-updated-since`) |
| `history-csv` | `cmdHistory` | `-input S -end 2025-03-15` |
| `history-json-scoped` | `cmdHistory` | `-input S -project "Search Revamp" -milestone Indexing -window 14 -end 2025-03-15 -format json` |
| `history-text` | `cmdHistory` | `-input S -start 2025-02-01 -end 2025-03-01 -format text` |
| `history-multiteam` | `cmdHistory` | `-input M -end 2025-03-15` (blending warning) |
| `check-clean` | `cmdCheck` | `-input S` |
| `check-exclusions` | `cmdCheck` | `-input M -exclusions X` |
| `check-bad-exclusions` | `cmdCheck` | `-input S -exclusions B` |
| `err-sim-no-mode` | `cmdSimItems` | `-input S SIM -days 30` → `one of -engineers or -whole-team must be specified` |
| `err-sim-days-and-end` | `cmdSimItems` | `-input S -engineers 2 SIM -days 30 -target-end-date 2025-04-30` → `-days and -target-end-date are mutually exclusive` |
| `err-sim-days-zero-throughput` | `cmdSimDays` | `-input S -whole-team -sample-start 2025-03-10 -sample-end 2025-03-15 -items 5 -random-seed 1 -goroutines 1` → `whole-team throughput was 0 ...` |
| `err-sim-items-bad-confidence` | `cmdSimItems` | `-input S -engineers 2 SIM -days 30 -confidence 0` |
| `err-sim-days-no-items` | `cmdSimDays` | `-input S -engineers 2 SIM` → `-items is required` |
| `err-backtest-unknown-project` | `cmdSimBacktest` | `-input S -engineers 2 SIM -project Nope -target-end-date 2025-03-31` |
| `err-backtest-no-project` | `cmdSimBacktest` | `-input S -engineers 2 SIM -target-end-date 2025-03-31` → `-project is required` |
| `err-aging-percentile` | `cmdAging` | `-input S -percentile 0` |
| `err-aging-window` | `cmdAging` | `-input S -sample-start 2025-03-01 -sample-end 2025-02-01` |
| `err-cfd-window` | `cmdCFD` | `-input S -start 2025-03-01 -end 2025-02-01` |
| `err-history-unknown-project` | `cmdHistory` | `-input S -project Nope` |
| `err-history-window` | `cmdHistory` | `-input S -window 0` |

A few cases use `-items` / `-confidence` with list values. Pass each flag and
its value as **two separate** `[]string` elements, e.g.
`"-items", "5,4,3"`. Never use a shell-style single string.

### 0.5 Generate, then inspect

```bash
go test ./cmd/forecast -run TestGolden -update
go test ./cmd/forecast -run TestGolden        # must pass
```

**Open at least five golden files and read them.** Confirm that:

- `aging-text` shows in-progress ages of about 11–13 days, not hundreds. If
  you see hundreds, the clock wasn't injected;
- the warning cases contain their `WARN ` lines;
- the error cases contain the expected error text;
- `sim-backtest-text` contains the `---- today: 2025-03-15 ...` divider.

Run `jj status` and confirm every new file under
`cmd/forecast/testdata/golden/` is listed. If one is missing, the
`.gitignore` exception is wrong.

**Acceptance:** `just test` passes. Golden files are committed. No production
behaviour changed (the `nowFunc` swap is a no-op at runtime).

Commit: `app layer: Phase 0 — golden CLI tests and injectable clock`.

---

## Phase 1: `internal/source` and the `internal/app` skeleton

### 1.1 `internal/source/source.go`

```go
// Package source loads issue sets and exclusions files for the forecast CLI
// (and, later, the web server). It is the IO boundary in front of
// internal/app, which is pure.
package source

// Load reads every issue from path. It is the former cmd/forecast loadIssues,
// moved verbatim, except that stdin is passed in rather than read from
// os.Stdin directly, so callers and tests control it.
func Load(ctx context.Context, path, stdinFormat string, stdin io.Reader) ([]issues.Issue, error)

// LoadExclusions is the former cmd/forecast loadExclusions, moved verbatim.
func LoadExclusions(path string) (simulate.Exclusions, error)
```

- Move the bodies exactly. Keep every error string, including
  `` `-stdin-format is required when reading from stdin (-input -): "csv" or "json"` ``.
- In `cmd/forecast/source.go`, keep `loadIssues` as a one-line wrapper,
  `return source.Load(ctx, path, format, os.Stdin)`, so existing call sites
  and `TestLoadIssues_Stdin` (which swaps `os.Stdin`) keep working.
- In `cmd/forecast/common.go`, delete `loadExclusions` and call
  `source.LoadExclusions` from `loadPool`.
- Move `TestLoadExclusions_*` from `cmd/forecast/common_test.go` to
  `internal/source/source_test.go` and rename them `TestLoadExclusions_*`
  against the new function. Add `TestLoad_StdinRequiresFormat` and
  `TestLoad_StdinCSV`, both using `strings.NewReader`.

### 1.2 `internal/app` skeleton

Create `doc.go`, `errors.go` (D7), `defaults.go` (D11), `convert.go` and
`teams.go`.

`doc.go`'s package comment must state the rules from D1–D5 in a short
paragraph each. This is the document future contributors will read.

`convert.go`: move these from `cmd/forecast` **verbatim**, as unexported
functions:

| function | from |
|---|---|
| `toAgingIssues` | `aging.go` |
| `toCFDIssues` | `cfd.go` |
| `toHistoryIssues` | `history.go` |
| `toCountsIssues` | `count.go` (keeps its terminal-`StateType` backfill and its doc comment) |
| `issuesToCompletions` | `common.go` |
| `issuesToBacktestItems` | `backtest.go` |

They're unexported in `app`, but in this phase `cmd/forecast` still calls
them. So temporarily add exported shims in `app/convert.go`:

```go
// Deprecated shims for cmd/forecast during the migration; removed in Phase 4.
func ToAgingIssues(items []issues.Issue) []aging.Issue { return toAgingIssues(items) }
// ...one per function
```

Update the `cmd/forecast` call sites to use the shims. Phase 4 deletes them.

`teams.go`:

```go
// DistinctTeamKeys is the former cmd/forecast distinctTeamKeys, moved verbatim.
func DistinctTeamKeys(items []issues.Issue) []string

// BlendingTeamsWarning is the former cmd/forecast blendingTeamsWarning, moved
// verbatim except that teams is []string rather than linear.TeamKeyList
// (internal/app must not import internal/linear). linear.TeamKeyList's
// underlying type is []string, so call sites pass it unchanged.
func BlendingTeamsWarning(teams []string, allTeams []string) string
```

Update the call sites in `aging.go`, `cfd.go`, `count.go` and `history.go`.
Delete the originals from `cmd/forecast`.

**Tests to move:**

- `TestBlendingTeamsWarning`: `cmd/forecast/blendteams_test.go` →
  `internal/app/teams_test.go`.
- `TestDistinctTeamKeys`: `cmd/forecast/source_test.go` →
  `internal/app/teams_test.go`.
- `TestToHistoryIssues`: `cmd/forecast/history_test.go` →
  `internal/app/convert_test.go`.
- `TestToCountsIssues_KeepsExplicitTerminalStateType`:
  `cmd/forecast/count_test.go` → `internal/app/convert_test.go`.

Adjust package names and calls; don't change assertions.

**Acceptance:** `just test` passes, including every golden test, unchanged.

Commit: `app layer: Phase 1 — internal/source and internal/app skeleton`.

---

## Phase 2: sim items / days / probability

This is the biggest phase. Read §2 again before you start.

### 2.1 Engineers parsing → `simulate`

Add to `simulate/mode.go`:

```go
// ParseEngineers parses an -engineers value: a positive integer is a count
// of anonymous equivalent engineers; anything else is a comma-separated list
// of unique names (count = len(names)). It is the parsing rule formerly
// inside cmd/forecast's engineerSpec.Set, moved verbatim with its error
// messages.
func ParseEngineers(v string) (count int, names []string, err error)
```

Also move `isAllDigits` (unexported) into `simulate`. Then change
`engineerSpec.Set` in `cmd/forecast/engineers.go` so that it calls
`simulate.ParseEngineers` and assigns `e.count, e.names` only on success.
`engineerSpec` stays in the CLI because it's a `flag.Value`. Add
`simulate/mode_test.go` cases for `ParseEngineers`, copying the input/output
pairs from `cmd/forecast/engineers_test.go`. Keep the CLI tests too.

### 2.2 `app/window.go`

```go
// resolveTargetWindow resolves a forecast horizon from a start date and
// exactly one of days (> 0) / end (non-zero). end is inclusive and is
// truncated to its local day first ("now" carries a time of day).
// effectiveDays is the calendar-day count of [start, end].
func resolveTargetWindow(start time.Time, days int, end time.Time) (resolvedEnd time.Time, effectiveDays int, err error)
```

The logic and messages come from `cmd/forecast/common.go`'s
`resolveTargetWindow`, minus flag parsing:

| condition, checked in this order | error |
|---|---|
| `days != 0 && !end.IsZero()` | `&FieldError{Fields: []string{"days", "target-end-date"}, Msg: "-days and -target-end-date are mutually exclusive"}` |
| `days == 0 && end.IsZero()` | `&FieldError{Fields: []string{"days", "target-end-date"}, Msg: "one of -days or -target-end-date must be provided"}` |
| `days < 0` | `&FieldError{Fields: []string{"days"}, Msg: "-days must be positive"}` |
| days given | return `start.AddDate(0, 0, days-1), days, nil` |
| `end = util.LocalDay(end)`; `!end.After(start)` | `&FieldError{Fields: []string{"target-end-date"}, Msg: "-target-end-date must be after -target-start-date"}` |
| otherwise | return `end, util.DayIndex(end, start) + 1, nil` |

In the CLI, `resolveTargetWindow(cmd, days, startStr, endStr, now)` becomes a
parser only. It parses `-target-start-date` (error
`invalid -target-start-date: %w`), passes `days` through **only if**
`isFlagSet(cmd, "days")` (otherwise 0), and parses `-target-end-date` **only
if** `isFlagSet(cmd, "target-end-date")` (error
`invalid -target-end-date: %w`). Rename it `parseTargetWindowFlags`, returning
`(start time.Time, days int, end time.Time, err error)`.

Rewrite `TestResolveTargetWindow` in `cmd/forecast/common_test.go` as a
table test of `app.resolveTargetWindow` in `internal/app/window_test.go`.
Keep every case, and adapt the `-days 0` case to §3 item 2. Assert the
`FieldError.Fields` as well as the message. Keep a small CLI test for the
flag parsing.

### 2.3 `app/simpool.go`

```go
// SimBase is the request block shared by every sim subcommand: the flags
// addSimFlags registers, resolved to typed values.
type SimBase struct {
	Issues           []issues.Issue
	Exclusions       simulate.Exclusions // already loaded; empty = none
	TypicalEngineers []string            // empty = every assignee
	Engineers        int                 // > 0 iff -engineers given
	EngineerNames    []string            // len == Engineers when names were given
	WholeTeam        bool
	SampleStart      time.Time
	SampleEnd        time.Time
	Simulations      int
	Workers          int
	Seed             int64
	Progress         func(done, total int) // optional
}

// SimRun is what a sim run resolved and built: everything a report header or
// a run manifest needs besides the subcommand's own results.
type SimRun struct {
	Mode            simulate.Mode
	ModeLabel       string
	Seed            int64
	SampleStart     time.Time
	SampleEnd       time.Time
	Pool            *simulate.SamplePool
	PoolIssues      []issues.Issue      // the completed issues that fed the pool
	Skipped         int                 // dropped by simulate.FilterInvalid
	Assignees       map[string]bool     // engineers with >= 1 completion in the window
	Exclusions      simulate.Exclusions
	SampleCalendar  *simulate.Calendar  // anchored at SampleStart
	HorizonCalendar *simulate.Calendar  // anchored at the target start; nil for backtest
}
```

`func (b SimBase) mode() (simulate.Mode, error)` returns
`simulate.ResolveMode(b.Engineers > 0, b.WholeTeam)`, wrapping any error as
`&FieldError{Fields: []string{"engineers", "whole-team"}, Msg: err.Error()}`.

```go
// preparePool selects the completed issues, builds the sample pool, and
// validates it for mode. requireProgress is forwarded to
// simulate.ValidatePool. Warnings are returned in the order the CLI used to
// log them.
func preparePool(b SimBase, mode simulate.Mode, requireProgress bool) (SimRun, []string, error)
```

The body is today's `loadPool` + `warnExclusionMismatches` +
`simulate.ValidatePool`, in this exact order:

1. `completed := completedForPool(b.Issues, b.SampleStart, b.SampleEnd, b.TypicalEngineers)`.
   Move `completedForPool` verbatim into `simpool.go`, with its doc comment.
2. Build `engineerSeen` exactly as `loadPool` does. Then, for each typical
   engineer not seen, append the warning
   `fmt.Sprintf("-typical-engineers engineer %q not found in data", name)`.
   This replaces `warnUnmatchedTypicalEngineers`.
3. `records, skipped := simulate.FilterInvalid(issuesToCompletions(completed))`.
   If `skipped > 0`, append
   `fmt.Sprintf("skipped %d completed issue(s) with no assignee or completion date", skipped)`.
4. `cal := simulate.NewCalendar(b.Exclusions, b.SampleStart)`, then
   `pool := simulate.BuildPool(records, cal, b.SampleStart, b.SampleEnd, b.WholeTeam)`.
5. Exclusion-mismatch warnings, moved from `warnExclusionMismatches`:
   - if `b.WholeTeam` and `len(b.Exclusions.Scopes()) > 0`, append
     `"exclusions: per-engineer entries are ignored in -whole-team mode"`;
   - otherwise, for each name in
     `unmatchedExclusionNames(b.Exclusions, engineerSeen, b.EngineerNames)`,
     append
     `fmt.Sprintf("exclusions: engineer %q matches no assignee in the sample data and no -engineers name", n)`.
   - Move `unmatchedExclusionNames` verbatim into `simpool.go`.
6. `if err := simulate.ValidatePool(pool, mode, requireProgress); err != nil`,
   return the run, the warnings so far, and the error unchanged.
7. Return the populated `SimRun`. Leave `HorizonCalendar` nil; each
   subcommand sets it. Set `ModeLabel` with
   `simulate.ModeLabel(mode, b.Engineers, b.EngineerNames)`.

Move `TestUnmatchedExclusionNames` to `internal/app`. Add
`TestPreparePool_WarningsInOrder`: a fixture that triggers a typical-engineer
warning and an exclusion-name warning, asserting the exact slice. Add
`TestPreparePool_ValidateErrorKeepsWarnings`.

### 2.4 `app/sim_items.go`

```go
type ItemsRequest struct {
	SimBase
	TargetStart time.Time
	Days        int       // exactly one of Days / TargetEnd
	TargetEnd   time.Time
	Confidences []int     // empty = DefaultConfidences()
}

type ItemsRow struct {
	Confidence int // e.g. 85
	Items      int // simulate.ItemsAtConfidence(dist, 85)
}

type ItemsResult struct {
	Warnings      []string
	Run           SimRun
	TargetStart   time.Time
	TargetEnd     time.Time // inclusive, resolved
	EffectiveDays int
	Confidences   []int     // effective (defaults applied)
	Rows          []ItemsRow
	Distribution  []int     // sorted simulate.ItemsInDays output
}

func (r ItemsRequest) Validate() error
func SimItems(req ItemsRequest) (*ItemsResult, error)
```

`Validate()` checks, in order:

1. `req.mode()`.
2. `resolveTargetWindow(req.TargetStart, req.Days, req.TargetEnd)`.
3. Each confidence `c` must satisfy `0 < c <= 100`, else
   `&FieldError{Fields: []string{"confidence"}, Msg: fmt.Sprintf("-confidence: values must be in (0, 100], got %d", c)}`.

`SimItems`:

1. `res := &ItemsResult{}`, which is what's returned in every path (D4).
2. `Validate()`; on error, return `res, err`.
3. Resolve the mode, the window (`res.TargetEnd`, `res.EffectiveDays`), and
   `Confidences` (defaults if empty).
4. `run, warns, err := preparePool(req.SimBase, mode, false)`. Set
   `res.Warnings = warns`. On error, return `res, err`.
5. `run.HorizonCalendar = simulate.NewCalendar(req.Exclusions, req.TargetStart)`.
6. `dist := simulate.ItemsInDays(run.Pool, simulate.Params{...})`, with the
   same fields `cmdSimItems` passes today, including
   `Progress: req.Progress`.
7. Build `Rows` in the confidence order the caller gave.
8. Fill in `Run`, `TargetStart`, `Distribution`, and return.

### 2.5 `app/sim_days.go`

```go
type DaysRequest struct {
	SimBase
	TargetStart time.Time
	Items       []int // one value = single report; several = grouped trajectory
	Confidences []int // empty = DefaultConfidences()
}

type DaysRow struct {
	Confidence int
	Days       int       // simulate.PercentileValue(dist, c)
	Date       time.Time // TargetStart + Days
}

type TrajectoryCell struct {
	Confidence     int
	MarginalDays   int
	CumulativeDays int
	Date           time.Time // TargetStart + CumulativeDays
}

type TrajectoryGroup struct {
	Items int              // this group's own size (not cumulative)
	Cells []TrajectoryCell // one per confidence, in order
}

type TrajectoryTotal struct {
	Items int              // sum of all groups
	Cells []TrajectoryCell // MarginalDays == CumulativeDays == totals[pi]
}

type DaysResult struct {
	Warnings    []string
	Run         SimRun
	TargetStart time.Time
	Confidences []int
	// Exactly one of Single / Groups is set, depending on len(Items).
	Single       []DaysRow
	Distribution []int // single mode only
	Groups       []TrajectoryGroup
	Total        *TrajectoryTotal
}

func (r DaysRequest) Validate() error
func SimDays(req DaysRequest) (*DaysResult, error)
```

`Validate()` checks, in order:

1. `req.mode()`.
2. `len(Items) == 0` →
   `&FieldError{Fields: []string{"items"}, Msg: "-items is required"}`.
3. Any `n <= 0` →
   `&FieldError{Fields: []string{"items"}, Msg: fmt.Sprintf("-items: group sizes must be positive, got %d", n)}`.
4. The confidence range check (§3 item 3), with the same code as items.

`SimDays`:

1. Same opening as `SimItems`, but with `preparePool(..., true)`.
2. `run.HorizonCalendar = simulate.NewCalendar(req.Exclusions, req.TargetStart)`.
3. Build the base `simulate.Params` as `cmdSimDays` does today.
4. **If `len(Items) > 1`:** port the computation half of
   `printTrajectoryReport`:
   - compute the cumulative thresholds;
   - call `simulate.DaysToComplete` once per threshold, with
     **`Progress` left nil**, exactly as today;
   - call `simulate.ComputeTrajectoryTable(dists, confidences)`;
   - map the cells into `Groups` and `Total`, computing
     `Date = TargetStart.AddDate(0, 0, CumulativeDays)`.
5. **Otherwise:** set `params.Items = Items[0]` and
   `params.Progress = req.Progress`, call `simulate.DaysToComplete`, and
   build `Single`.

### 2.6 `app/sim_probability.go`

```go
type ProbabilityRequest struct {
	SimBase
	TargetStart time.Time
	Days        int
	TargetEnd   time.Time
	Items       int // < 0 = report the full table instead of one probability
}

type ProbabilityRow struct {
	Items       int
	Probability float64 // percent, 0–100
}

type ProbabilityResult struct {
	Warnings      []string
	Run           SimRun
	TargetStart   time.Time
	TargetEnd     time.Time
	EffectiveDays int
	Items         int     // echoes the request
	Probability   float64 // set when Items >= 0
	Table         []ProbabilityRow // set when Items < 0: n = 1, 2, ... up to and including the first row with Probability == 0
	Distribution  []int
}
```

`Validate()`: `req.mode()`, then `resolveTargetWindow`. The run body mirrors
`SimItems`. The table loop is exactly today's: `for n := 1; ; n++`, append,
and break after appending a 0 row.

### 2.7 Rewire `cmd/forecast/sim.go`

Each `cmdSim*` becomes:

1. Flag registration (unchanged, except `-simulations` uses
   `app.DefaultSimulations` and the confidence default comes from
   `app.DefaultConfidences()` where it's applied).
2. `ApplyConfig`, then `resolveInput`.
3. Parse every date flag with its existing error strings
   (`invalid -sample-start date: %w`, etc.), plus `parseTargetWindowFlags`
   where relevant.
4. Build the request, with `Issues` and `Exclusions` still empty. Call
   `req.Validate()`; return on error.
5. `loadIssues(...)` (error `load issues: %w`), then
   `source.LoadExclusions(*sf.ExclusionsFile)`. Assign both to the request.
6. `seed := resolveSeed(cmd, *sf.RandomSeed, now)`. Set `req.Seed`,
   `req.Workers`, `req.Simulations`, and `req.Progress = bar.update`, with
   `bar := newProgressBar(*sf.Simulations)` created here.
   - **`sim days` exception:** today the bar is only created on the
     single-value path. Keep it that way: create it, and set `req.Progress`,
     only when `len(items) == 1`.
7. Call the app function, print the warnings (D4), and return on error.
8. `writeManifest(...)`, filled from `res.Run` and `res`, with the same
   `Extra` maps as today. The manifest is written after the simulation now,
   instead of before. That's fine: output order on stdout is still manifest,
   then report.
9. Print the report from the result, with exactly today's format strings.
   `printTrajectoryReport` keeps its name and printing code, but takes
   `(*app.DaysResult)` and reads `Groups` / `Total` instead of computing.

Delete from `cmd/forecast`: `poolData`, `loadPool`, `completedForPool`,
`issuesToCompletions` (and its shim), `warnUnmatchedTypicalEngineers`,
`unmatchedExclusionNames`, `warnExclusionMismatches`, and the old
`resolveTargetWindow`.

`cmd/forecast/backtest.go` still needs a pool in this phase. Have it call a
**temporary** exported wrapper, `app.PreparePoolForBacktest(b SimBase)`,
which calls `preparePool(b, mode, false)` after resolving the mode. Add a
`// Removed in Phase 3.` comment. The backtest CLI prints the returned
warnings itself.

**Tests:**

- `internal/app/sim_items_test.go`:
  - `TestSimItems_DefaultConfidences`;
  - `TestSimItems_RowsMatchItemsAtConfidence` (hand-check against
    `simulate.ItemsAtConfidence` on the returned `Distribution`);
  - `TestSimItems_ValidateFieldErrors` (table: no mode, both window inputs,
    neither, negative days, bad confidence; assert `Fields` and `Msg`);
  - `TestSimItems_WarningsReturnedOnError`.
- `internal/app/sim_days_test.go`:
  - `TestSimDays_SingleDates`;
  - `TestSimDays_TrajectoryInvariants` (marginal days are all `>= 0`, and each
    confidence's marginals sum to `Total`);
  - `TestSimDays_ConfidenceRangeChecked` (§3 item 3).
- `internal/app/sim_probability_test.go`:
  - `TestSimProbability_TableEndsAtFirstZero`;
  - `TestSimProbability_SingleMatchesProbabilityAtLeast`.
- Use a tiny constant-throughput fixture like `writeConstantFixture` in
  `cmd/forecast/sim_test.go`, built in memory as `[]issues.Issue`, with
  `Simulations: 200`, `Workers: 1`, `Seed: 42`.
- Every existing `cmd/forecast/sim_test.go` and `manifest_test.go` test, and
  every golden test, must pass unchanged.

**Acceptance:** `just test` passes, and
`grep -n "simulate.BuildPool\|FilterInvalid\|ItemsAtConfidence\|ComputeTrajectoryTable\|ProbabilityAtLeast" cmd/forecast/*.go | grep -v _test.go`
prints nothing.

Commit: `app layer: Phase 2 — sim items/days/probability in internal/app`.

---

## Phase 3: sim backtest

### 3.1 `app/sim_backtest.go`

```go
type BacktestRequest struct {
	SimBase
	Project     string    // required, exact match
	Milestone   string    // optional, exact match within Project
	ReplayStart time.Time // zero = earliest started_at across the issue set, as a local day
	TargetEnd   time.Time // required
	Today       time.Time // local midnight; rows after it are Projected
}

type BacktestRow struct {
	Date      time.Time
	Completed int
	Remaining int
	Prob      float64 // percent
	Projected bool    // Date.After(Today)
}

type BacktestResult struct {
	Warnings            []string
	Run                 SimRun // HorizonCalendar is nil
	Scope               string // "Project" or "Project / Milestone"
	IssueCount          int    // len(backtested set)
	ReplayStart         time.Time
	ReplayStartInferred bool
	Rows                []BacktestRow
}
```

`Validate()` checks, in order:

1. `Project == ""` →
   `&FieldError{Fields: []string{"project"}, Msg: "-project is required"}`.
2. `TargetEnd.IsZero()` →
   `&FieldError{Fields: []string{"target-end-date"}, Msg: "-target-end-date is required"}`.
3. `req.mode()`.

`SimBacktest`:

1. Validate.
2. `preparePool(req.SimBase, mode, false)`.
3. `scoped := backtestIssueSet(req.Issues, req.Project, req.Milestone)`. Move
   `backtestIssueSet` verbatim. If it comes back empty, return a
   `FieldError` with `Fields: []string{"project", "milestone"}` when a
   milestone was given (otherwise `[]string{"project"}`), using today's two
   messages verbatim.
4. Resolve the replay start:
   - if `ReplayStart` is non-zero, use it;
   - otherwise use `util.LocalDay(simulate.EarliestStartedAt(items))`, and if
     that's zero, return `&FieldError{Fields: []string{"replay-start-date"}, Msg: "no started_at found in issue set; provide -replay-start-date explicitly"}`.
     Set `ReplayStartInferred`.
   - Note the existing ordering: today's code checks `IsZero` on the raw value
     **before** calling `LocalDay`. Preserve that.
5. If `!TargetEnd.After(start)`, return
   `&FieldError{Fields: []string{"target-end-date", "replay-start-date"}, Msg: fmt.Sprintf("-target-end-date must be after -replay-start-date (inferred: %s)", start.Format("2006-01-02"))}`.
   The message says "(inferred: ...)" even when the start was explicit;
   that's today's text, so keep it.
6. Call `simulate.RunBacktest(...)` with
   `Calendar: simulate.NewCalendar(req.Exclusions, start)`, and the same
   `Params` as today.
7. Map the rows, computing `Projected = r.Date.After(req.Today)`.

### 3.2 Rewire `cmd/forecast/backtest.go`

- The CLI keeps the `-format` check and parses dates as before. It parses
  `-replay-start-date` only if it's non-empty (zero otherwise), and passes
  `Today: util.LocalDay(now)`.
- Keep the CLI's own `-target-end-date` empty-string check **before** parsing,
  so an empty value doesn't hit `invalid -target-end-date`. Its message is
  identical to the app's.
- `printBacktestText` and `printBacktestCSV` take `*app.BacktestResult`.
  Their dividers and the CSV `projected` column read `Row.Projected` instead
  of comparing with `today`.
- Delete `issuesToBacktestItems`'s shim, `backtestIssueSet`, and
  `app.PreparePoolForBacktest`.

**Tests** (`internal/app/sim_backtest_test.go`):

- `TestSimBacktest_ExcludesCanceled` (port the logic of
  `TestCmdSimBacktest_ExcludesCanceledIssues`);
- `TestSimBacktest_InfersReplayStart`;
- `TestSimBacktest_ProjectedFlag`;
- `TestSimBacktest_UnknownProjectFieldError`;
- `TestSimBacktest_ValidateOrder` (no project *and* no target end → project
  error).

**Acceptance:** `just test` passes; the golden tests are unchanged.

Commit: `app layer: Phase 3 — sim backtest in internal/app`.

---

## Phase 4: aging, cfd, count, history

Each command follows the same pattern: a request that embeds the package's
existing `Options` where there is one, a result, `Validate()`, and a run
function. Move logic verbatim from the `cmd*` body. Delete the Phase 1
exported conversion shims at the end of this phase.

### 4.1 `app/aging.go`

```go
type AgingRequest struct {
	Issues []issues.Issue
	aging.Options       // Teams, SampleStart, SampleEnd, MinCycleTime, Percentile
	ShowCompleted bool
	Today         time.Time // local midnight
}

type AgingResult struct {
	Warnings   []string
	Meta       aging.Meta
	InProgress []aging.Item // sorted by AgeDays desc
	Completed  []aging.Item // nil unless ShowCompleted; sorted by AgeDays desc
	CycleTimes []float64    // sorted asc
}
```

- `Validate()`:
  - `Percentile` outside 1..100 →
    `FieldError{"percentile"}` with today's message;
  - `!SampleStart.Before(SampleEnd)` →
    `FieldError{"sample-start", "sample-end"}`,
    `"-sample-start must be before -sample-end"`.
- `RunAging` is the body of `cmdAging` from `blendingTeamsWarning` through
  the `len(cycleTimes) == 0` warning. Move `completedBetween` and
  `inProgress` verbatim, with their doc comments; `completedBetween`'s
  comment explains a deliberate bug fix and must survive.
- Warnings, in order: the blending warning (computed on the **unfiltered**
  issues, as today), then
  `"no completed issues found in the sample window; percentiles will be 0 and multipliers blank"`.
- The CLI keeps: the `-min-cycle-time` parse, date parsing, the `-format`
  switch, and rendering.

### 4.2 `app/cfd.go`

```go
type CFDRequest struct {
	Issues []issues.Issue
	cfd.Options // Teams, Start, End
}

type CFDResult struct {
	Warnings []string
	Rows     []cfd.DayRow
	Health   cfd.FlowHealth // TotalIssues and SkippedIssues already set
}
```

- `Validate()`: `!Start.Before(End)` → `FieldError{"start", "end"}`,
  `"-start must be before -end"`.
- `RunCFD`:
  - blending warning (unfiltered issues);
  - filter;
  - `"no issues found for the given team filter"` if the filtered set is
    empty;
  - the normalize loop with its skip count;
  - `BuildGrid`;
  - `AssertInvariants`, wrapped as `fmt.Errorf("CFD invariant violated: %w", err)`
    (a plain error, not a FieldError);
  - `ComputeHealth`, then set `TotalIssues` / `SkippedIssues`.
- The CLI calls
  `cfd.RenderHTML(out, res.Rows, res.Health, res.Health.TotalIssues, res.Health.SkippedIssues, opts.Start, opts.End)`.
  The values are identical to today's arguments.

### 4.3 `app/count.go`

```go
type CountRequest struct {
	Issues []issues.Issue
	counts.Options // Teams, Since
}

type CountResult struct {
	Warnings  []string
	Projects  []counts.Project
	Total     int
	ShowTeams bool // no -teams given and the loaded set has > 1 team
}
```

- No `Validate()` checks are needed, but define the method so the API is
  uniform; it returns nil.
- `RunCount` is the body of `loadCountProjects`, minus loading, plus the
  `showTeams` computation from `cmdCount`. Warnings: blending, then
  `"no outstanding (non-terminal) issues found for the given filters"`.
- Delete `loadCountProjects`.

### 4.4 `app/history.go`

```go
type HistoryRequest struct {
	Issues     []issues.Issue
	Teams      []string
	Project    string
	Milestone  string
	Start      time.Time // zero = earliest created_at in scope (local day)
	End        time.Time
	WindowDays int       // the CLI passes its -window value; 0 is rejected by Validate
}

type HistoryResult struct {
	Warnings      []string
	Result        history.Result
	Start         time.Time
	StartInferred bool
}
```

- `Validate()`: `WindowDays <= 0` → `FieldError{"window"}`,
  `"-window must be positive"`.
- `RunHistory`, in today's order:
  1. Filter.
  2. If empty, return the three messages verbatim, as `FieldError`s:
     `{"project", "milestone"}`, `{"project"}`, and `{"teams"}` for the
     no-project case.
  3. Blending warning, computed on the **filtered** set. Today's `history`
     differs from the other commands here; preserve it, and add a comment
     saying so.
  4. Infer the start: the existing `history.EarliestCreatedAt` logic and its
     `"no created_at found in issue set; provide -start explicitly"` error,
     as `FieldError{"start"}`.
  5. `End.Before(start)` → `FieldError{"end", "start"}`, today's message.
  6. `history.Compute`.
  7. `history.AssertInvariants`, wrapped `"history invariant violated: %w"`.
- The CLI keeps: the `-format` check, the `-input` check, date parsing (it
  now parses `-end` **before** loading; see §3 item 1), `-out`, and
  rendering.

### 4.5 Cleanup

Delete the exported conversion shims from Phase 1.

`grep -n "issues.Filter\|aging.CycleTimes\|aging.RankItems\|cfd.Normalize\|cfd.BuildGrid\|counts.Aggregate\|history.Compute\|app.To" cmd/forecast/*.go | grep -v _test.go`
must print nothing.

**Tests**, one `_test.go` per file in `internal/app`:

- **aging:** unassigned completed issue included (port
  `TestCmdAging_UnassignedCompletedIssueIncluded`'s fixture); percentile
  validation `FieldError`; empty-window warning; in-progress sorted desc.
- **cfd:** blending warning only when `Teams` is empty and there's more than
  one team; `Health.TotalIssues` / `SkippedIssues` set; window `FieldError`.
- **count:** `ShowTeams` true/false matrix; terminal-from-timestamps (port
  `TestCmdCount_TerminalFromTimestampsWithoutStateType`).
- **history:** inferred start; the three empty-scope `FieldError`s; the
  blending warning uses the filtered set.

**Acceptance:** `just test` passes; the golden tests are unchanged.

Commit: `app layer: Phase 4 — aging, cfd, count, history in internal/app`.

---

## Phase 5: check and manifest

### 5.1 `app/check.go`

```go
type CheckResult struct {
	Command  string // "history", "cfd", "aging", "count", "sim"
	Excluded int    // issues the command will silently drop; 0 = ok
	Status   string // "ok" or the exact sentence check prints today
}

// CheckReadiness is the former cmd/forecast checkResults, moved, with
// Excluded added. Status strings are unchanged.
func CheckReadiness(items []issues.Issue) []CheckResult

// Assignees returns the set of non-empty Assignee values in items
// (formerly inline in cmdCheck).
func Assignees(items []issues.Issue) map[string]bool

type ExclusionsEngineer struct {
	Name    string
	Entries int
}

type ExclusionsReport struct {
	ParseErr       error // non-nil: file unreadable/unparseable; other fields zero
	GlobalEntries  int
	Engineers      []ExclusionsEngineer // sorted by name (exc.Scopes() order)
	HasDates       bool
	First, Last    time.Time
	Past, Future   int
	Today          time.Time
	UnmatchedNames []string // engineer scopes with no matching assignee, in Scopes() order
}

// CheckExclusions computes what cmd/forecast's checkExclusions used to
// format directly. parseErr is the read-or-parse error, if any.
func CheckExclusions(exc simulate.Exclusions, parseErr error, assignees map[string]bool, today time.Time) ExclusionsReport
```

- Move `excludedStatus` with `CheckReadiness`. For the "no issues in input"
  case, `Excluded` is 0 and `Status` is `"no issues in input"`, as today.
- In `cmd/forecast/check.go`, keep a function `checkExclusionsLines(r app.ExclusionsReport) []string`
  that reproduces **exactly** today's lines from the report: `invalid`,
  `entries`, `span` (with its `none` variant), one `engineers` line per
  unmatched name, or `engineers ok`.
- `TestCheckResults_*` and `TestExcludedStatus_Pluralization` move to
  `internal/app/check_test.go`. `TestCheckExclusions_*` stay in
  `cmd/forecast`, pointed at `checkExclusionsLines(app.CheckExclusions(...))`,
  with unchanged assertions. Add `internal/app` tests for the report fields
  (`Past` / `Future` counting, `UnmatchedNames`).

### 5.2 `app/manifest.go`

Move from `cmd/forecast/manifest.go` into `internal/app`:

- the types `Manifest`, `BinaryInfo`, `Invocation`, `FlagRecord`, `Resolved`,
  `DataSection`, `ExclusionsSection`, `DaysByScope`, `DataFile`,
  `PoolSection`, `IssuesSection` and `IssueRecord`, with their JSON tags
  unchanged;
- `modeName`, `issueTime`, `resolveDays` and `formatDays`.

Replace `manifestInputs` / `newManifest` with:

```go
type ManifestInputs struct {
	Subcommand       string
	Run              SimRun
	TypicalEngineers []string
	Engineers        int
	EngineerNames    []string
	WholeTeam        bool
	ExclusionsPath   string
	Extra            map[string]any
	// Supplied by the caller because they need IO, the flag package, or the
	// clock, none of which internal/app may touch:
	Flags       []FlagRecord
	Invocation  Invocation
	Binary      BinaryInfo
	DB          DataFile
	GeneratedAt time.Time
}

// NewManifest assembles a Manifest. Pure: same inputs, same output.
func NewManifest(in ManifestInputs) *Manifest
```

`NewManifest`'s body is today's `newManifest`, reading from `in.Run`
(`Mode`, `Seed`, `SampleStart`, `SampleEnd`, `Exclusions`,
`SampleCalendar`, `HorizonCalendar`, `Pool`, `PoolIssues` → `Issues`,
`Skipped`). Use `GeneratedAt: in.GeneratedAt.UTC().Format(time.RFC3339)`.
`SchemaVersion` stays `2`. **The JSON output must be byte-identical to
today's for the same inputs.**

Supporting changes:

- `dbFingerprint` → `internal/source/fingerprint.go` as
  `func Fingerprint(path string) app.DataFile`, moved verbatim.
  `TestDBFingerprint*` moves with it.
- `cmd/forecast/manifest.go` keeps:
  - the `version` variable and `buildInfo()`, which now returns
    `app.BinaryInfo`;
  - a new `flagRecords(fs *flag.FlagSet) []app.FlagRecord`, the `VisitAll`
    loop from today's `newManifest`;
  - `writeManifest(path string, in app.ManifestInputs) error`, which encodes
    `app.NewManifest(in)`.
  - The sim commands build `ManifestInputs`, filling in:
    - `Flags: flagRecords(cmd)`;
    - `Invocation: app.Invocation{Subcommand: ..., Args: os.Args, WorkingDir: wd}`
      (with `wd, _ := os.Getwd()`);
    - `Binary: buildInfo()`;
    - `DB: source.Fingerprint(inputPath)`;
    - `GeneratedAt: nowFunc()`.
- `TestNewManifest_Assembly` and
  `TestNewManifest_NoExclusionsMarshalsEmptyLists` move to
  `internal/app/manifest_test.go`, adapted to build `ManifestInputs` /
  `SimRun` directly, with the same assertions. `TestBuildInfo_Degrades`
  stays in `cmd/forecast`.
- Keep `TestCmdSimItems_SampleExclusionsInManifest` in `cmd/forecast`; it's
  the end-to-end check that the wiring is right.

**Acceptance:** `just test` passes; the golden tests are unchanged.

Commit: `app layer: Phase 5 — check and manifest in internal/app`.

---

## Phase 6: audit and documentation

### 6.1 Purity audit

Each of these must print nothing:

```bash
grep -rn "time.Now\|os.Stdout\|os.Stderr\|os.Args\|os.Getwd\|\"flag\"\|internal/logx\|internal/linear\|internal/sqlite\|internal/source" internal/app --include=*.go | grep -v _test.go
grep -n "issues.Filter\|simulate.BuildPool\|FilterInvalid\|ItemsAtConfidence\|PercentileValue\|ProbabilityAtLeast\|ComputeTrajectoryTable\|RunBacktest\|aging.CycleTimes\|aging.RankItems\|cfd.Normalize\|cfd.BuildGrid\|cfd.ComputeHealth\|counts.Aggregate\|counts.Compute\|history.Compute\|IsCompleted\|IsCanceled\|IsInProgress" cmd/forecast/*.go | grep -v _test.go
```

`internal/app` *test* files may use `time.Date`, but not `time.Now`.

Then read every `cmd*` function in `cmd/forecast` from top to bottom. Each
should be only these steps:

1. flags;
2. config;
3. input resolution;
4. date and duration parsing;
5. request construction plus `Validate()`;
6. loading;
7. the app call;
8. warnings;
9. manifest;
10. render.

If any loop or conditional computes a domain value (anything except picking
an output format or a writer), it belongs in `app`. Move it, and add a test.

### 6.2 Documentation

- `CLAUDE.md`:
  - **Architecture:** add `internal/app` and `internal/source` paragraphs,
    in the style of the existing `internal/sqlite` paragraph.
  - **Replace** the claim that "`cmd/forecast` is the only place that
    converts `issues.Issue`" with `internal/app`. Update the ASCII diagram
    (`cmd/forecast.loadIssues` → `source.Load` → `internal/app` → CLI
    renderers).
  - Note D3/D4 (warnings as data) and D7 (`FieldError`) under "Conventions
    worth knowing".
  - Update the tests list in the first section (add `internal/app`, and
    `internal/source`).
- `DATA_REQUIREMENTS.md` line 66: `completedBetween` now lives in
  `internal/app/aging.go`.
- At the top of this file, set **Status: done**, listing any deviations, in
  the style of `HISTORY_PLAN.md`'s status block.

**Acceptance:** `just test` passes, `gofmt -l .` prints nothing, and both
greps are empty.

Commit: `app layer: Phase 6 — audit and docs`.

---

## 7. What "done" looks like

- `cmd/forecast` contains flag definitions, config application, date
  parsing, progress bars, writers and printing, and nothing else.
- Every CLI invocation in the golden set produces byte-identical stdout,
  stderr and errors compared with `main` (the only exceptions are §3, and
  none of them touch an existing golden case).
- `internal/app` exposes, per command, a `XRequest` with `Validate()`, an
  `XResult` with `Warnings`, and a run function, all pure and unit-tested.
- A second adapter (the web server) could implement every command without
  importing `cmd/forecast` or copying any logic out of it.

---

## 8. Out of scope (don't do these here)

- The web server, HTML, JSON DTOs, or JSON tags on `app` types.
- Changing any flag's name, default, or help text (apart from referencing
  the D11 constants with identical values).
- Replacing `flag.ExitOnError`.
- `forecast linear sync` / `teams`, `version`, and `update`. They have no
  analysis logic, and `syncer` / `selfupdate` are already packages.
- Caching issue loads, or splitting pool preparation into a separate
  reusable call. The web phase will decide that with real usage in hand.

---

## 9. Context: where this is headed (not part of this plan)

This shapes the API above; don't implement any of it now.

- **`forecast serve`:** the same binary, with the frontend embedded through
  `embed.FS`, so `forecast update` updates the GUI too.
- **JSON API:** e.g. `POST /api/sim/days`, whose request DTOs map onto
  `app.*Request`, with field names that equal the CLI flag names / config
  keys. `FieldError.Fields` drives per-input error highlighting, and
  `Warnings` render inline next to the result. `SimBase.Progress` streams
  over server-sent events.
- **Pages organised by question, not by command:**
  - "When will it be done?" (days + trajectory);
  - "How much can we fit?" (items and probability share one distribution, so
    one page shows both);
  - "Is this project on track?" (backtest + history);
  - "Flow health" (cfd + aging + count);
  - "Check my data" as the first-run screen.

  Advanced options (seed, workers, typical engineers) sit behind a toggle.
  Form state lives in the URL, so a forecast is shareable.
- **Testing:** `httptest` for handlers and `internal/app` unit tests for
  logic make up most of the coverage, plus a few Playwright (headless
  Chromium) end-to-end tests of the main user flows.
