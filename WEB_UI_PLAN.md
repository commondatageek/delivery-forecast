# Implementation plan: `forecast serve`, a web UI with full CLI parity

Status: **Phase 0 done** (this document is committed on the `web-ui` bookmark). Phases 1–10 not started.

## Why this exists

`APP_LAYER_PLAN.md` moved the business logic out of `cmd/forecast` into
`internal/app` (pure) and `internal/source` (IO). That work was a
prerequisite for a second adapter. This plan builds that adapter:
`forecast serve`, which starts a local HTTP server and serves a browser UI.

**The goal:** everything you can do with `forecast <command> -flags`, you can
do in the browser. That covers every analysis command, every flag that
changes the analysis, and every output format, with output identical to the
CLI.

This plan does not hand-maintain a second copy of the flag set. It reaches
parity by construction:

1. Each command's flag registration, and its "flags → `app` request"
   resolution, moves out of `package main` into a new shared package,
   `internal/params`. The CLI and the web server both build the same
   `flag.FlagSet`. The CLI fills it with `fs.Parse(os.Args)`. The web server
   fills it with `fs.Set(name, value)` for each form field. Defaults,
   presence semantics (`-random-seed`, `-days` vs `-target-end-date`), date
   expressions and error messages are therefore identical, because they come
   from the same code.
2. The CLI's own text renderers (the `fmt.Printf`/`tabwriter` code for the
   sim reports, backtest and check) move into `internal/report`, writing to
   an `io.Writer`. The web server can then produce byte-identical text
   output and downloads.
3. The web UI's forms are generated from the `FlagSet` itself, plus a small
   table of UI hints. A test fails if a CLI flag has no UI decision, so a
   flag added later can't be silently missing from the browser.
4. A test replays **every** CLI golden case through the HTTP API and
   requires byte-identical output, identical warnings and identical errors.

---

## 0. How to use this document

**Rules for whoever implements this:**

1. **Work one phase at a time, in order.** Each phase must compile, pass
   `gofmt -l .` (prints nothing) and pass `just test` before the next one
   starts.
2. **Commit at the end of each phase** with `jj` (see §1). One phase = one
   commit.
3. **Write the tests each phase lists.** They're required, not optional.
4. **Do not relitigate the decisions in §2.** If you think one is wrong, stop
   and raise it with the user. Don't quietly do something else.
5. **Don't do work assigned to a later phase**, even when it would be
   convenient.
6. **The CLI golden tests are the contract.**
   - Never run `go test ./cmd/forecast -run TestGolden -update` in this plan.
   - If a golden test fails, your change altered CLI behaviour. Fix the
     change, not the golden file.
   - This plan allows exactly one CLI behaviour change, listed in §3, and it
     touches no golden file.
7. **Move code; don't rewrite it.**
   - When this plan says "move X", cut the code and paste it in its new
     home. Change only what the plan says to change (signature, receiver,
     `os.Stdout` becoming an `io.Writer` parameter, `logx` calls becoming
     returned warnings).
   - Keep comments, the order of operations, and error strings
     byte-for-byte.
8. If you can't meet a phase's acceptance criteria as written, stop and
   report what's blocking you. Don't improvise a different design.
9. **Frontend phases (6–9) also have a manual checklist.**
   - Run `forecast serve` and walk through the checklist in a browser (the
     Claude desktop app's built-in browser works).
   - Fix anything that fails before committing.
   - Report anything you could not check.

**Repo conventions to follow** (they already exist; match them):

- `internal/app` stays pure. It has no IO, no `time.Now`, no `flag`/`os`/
  `logx`, and it doesn't import `source`, `sqlite`, `linear` or `logx`. This
  plan adds a few pure functions and methods to it. Those obey the same
  rules.
- **`internal/report` is pure too.** It writes to an `io.Writer` it is given,
  and has no `os`, `time.Now` or `logx`.
- **`internal/params` may import `flag`, `internal/util`, `internal/app`,
  `internal/flagvals`, `internal/linear` (for `TeamKeyList`) and
  `internal/syncer` (for `syncer.Options`).** It must not touch `os.Stdout`,
  `os.Stderr`, `os.Args`, `time.Now` or `logx`. Anything it would log is
  returned as a warning string.
- Doc comments on every exported identifier. Match the surrounding comment
  density and naming.
- Go 1.26, standard library only for the server. Use `net/http` with Go
  1.22+ `ServeMux` method and wildcard patterns, and `embed`. Add no Go
  dependencies. `gopkg.in/yaml.v3` is already in `go.mod` and may be used.
- **Frontend: plain HTML, CSS and JavaScript ES modules.**
  - No npm, no bundler, no build step, no framework.
  - Plotly (already used by `cfd`'s HTML report) is the only third-party
    script. It is **vendored and embedded** (D14), at the same version
    `cfd/template.html` uses, so the UI works with no internet access.

---

## 1. Version control: `jj`, not git

This repo is a colocated **Jujutsu** repo (`.jj/` and `.git/` both exist).
Use `jj` for every mutation. Never run `git add`, `git commit`,
`git checkout` or `git rebase`; they desync jj's view of the repo.

- The working copy is itself a commit (`@`), and jj snapshots it
  automatically at the start of every `jj` command. There is no staging area.
- Never run a jj command that opens an editor or TUI. Always pass `-m` to
  `jj describe` / `jj commit`. Never use `-i`, `--interactive`, `--tool`,
  bare `jj split`, or `jj resolve`.
- Work happens on the bookmark **`web-ui`**.
  - It branches off **`app-layer-extraction`**, because this plan depends on
    `internal/app` and `internal/source`.
  - Its first commit adds this document.
  - If `app-layer-extraction` is merged into `main` before you finish,
    rebase with `jj rebase -b web-ui -o main` and re-run `just test`.

**At the end of each phase:**

```bash
gofmt -l .                      # must print nothing
go vet ./...                    # must print nothing
just test                       # must pass
jj status                       # review what changed
jj commit -m "web ui: Phase N — <short summary>"
jj bookmark set web-ui -r @-
jj log -n 5                     # confirm the bookmark sits on your new commit
```

**Don't push** (`jj git push`) unless the user asks you to. `jj undo`
reverts the last jj operation; `jj op log` lists earlier ones.

---

## 2. Decisions (binding)

**D1. One binary, one new subcommand.**
- `forecast serve` lives in `cmd/forecast/serve.go`. The server itself lives
  in a new package, `internal/web`.
- The frontend is embedded with `embed.FS`, so `forecast update` updates the
  UI too and there is nothing extra to install.

**D2. Parity by construction (see "Why this exists").**
- Each command's flags are registered and resolved by `internal/params`.
  `cmd/forecast` and `internal/web` both call it.
- The web server never parses a date expression, a list or a seed itself.
  It calls `fs.Set` and the same `Resolve` method the CLI calls.

**D3. Web parameters are flag values, as strings.**
- A request carries `params`, a JSON object mapping flag names to the
  string you would type after the flag on the command line. Example:
  `{"engineers": "3", "days": "30", "confidence": "50,85", "whole-team": "true"}`.
- The same representation is used for:
  - the URL (form state, shareable);
  - `-config` YAML import/export;
  - the "equivalent CLI command" display;
  - the run manifest's flag records.
- **An empty string means "not given"** and is skipped. `fs.Set` is never
  called with `""`.

**D4. The JSON API is the contract; the frontend is a client of it.** The
endpoints are listed in §4. Errors are JSON. Validation errors carry
`app.FieldError.Fields` so the UI can highlight inputs.

**D5. Runs stream NDJSON.**
- `POST /api/run/{id}` does all its fallible preparation synchronously:
  parameter parsing, `Resolve`, `Validate`, loading the data. Any failure in
  that stage returns **HTTP 400** with a JSON error body.
- Otherwise it returns **200** with `Content-Type: application/x-ndjson`:
  - zero or more `queued`, `progress` or `log` events;
  - then exactly one terminal `result` or `error` event.
- The CLI's progress bar maps onto `progress` events; `linear sync`'s log
  lines map onto `log` events.

**D6. Runs are stored and exports render from the stored result.**
- A successful run is kept in an in-memory store (the last 50 runs) under a
  random `run_id`.
- `GET /api/runs/{run_id}/export/{format}` renders a download from the
  stored result, using the same renderers the CLI uses.
- The simulation is never re-run, so the download always matches what the
  page shows, even with a time-based seed.

**D7. Data sources are server-side paths, plus uploads.**
- `input`, `exclusions` and `linear sync`'s `db` are paths on the server
  machine, resolved against the server's working directory, exactly like
  the CLI.
- `POST /api/uploads` stores an uploaded file in an upload directory and
  returns its path. That path is then used like any other.
- `-input -` (stdin) is not available in the web UI.

**D8. Loaded issue sets are cached.**
- The server keeps up to 4 loaded datasets in memory.
- Each is keyed by absolute path, size and modification time. For SQLite
  sources, the key also includes the size and modtime of the `-wal`
  sidecar.
- A changed file is reloaded automatically; `linear sync` invalidates the
  db it wrote.
- Cached `[]issues.Issue` slices are shared read-only across concurrent
  runs. Phase 5 adds a `-race` test that proves no command writes to them.

**D9. Local-only by default.**
- **Loopback only:** `serve` binds `127.0.0.1:8080` and refuses a
  non-loopback `-addr` unless `-allow-remote` is passed.
- **`-allow-remote` requires `-data-dir`:** paths are then restricted to
  that directory and the upload directory.
- **Every request** must carry a `Host` header naming a loopback host,
  unless `-allow-remote` is set.
- **Every non-GET request** must carry the header `X-Forecast-Client: web`.
  This forces a CORS preflight for cross-origin callers. The server never
  answers preflights and never sends CORS headers, so other websites can't
  drive it.
- There is no authentication; it is out of scope (§10).

**D10. One simulation at a time.**
- Simulations use every CPU (`-goroutines` defaults to `runtime.NumCPU()`),
  so runs pass through a semaphore of size 1.
- A waiting run emits a `queued` event first.
- The `app` run functions don't take a `context.Context`, so a run whose
  client disconnects finishes in the background, and its result is still
  stored. Cancellation is out of scope (§10).

**D11. Frontend shape.**
- A single page with hash routing: `#/<command-id>?<params>`.
- **Forms are generated from `GET /api/commands`**: the `FlagSet` metadata
  plus UI hints.
- **Every command first gets a generic result view (Phase 6):**
  - warnings;
  - the CLI-identical text output;
  - an embedded HTML report where the CLI has one;
  - download links;
  - the equivalent CLI command and `-config` YAML.
- That alone achieves full parity. Phases 7–8 then add native tables and
  charts per command.
- Form state lives in the URL. "Share link" pins the run's effective
  `random-seed`, so the link reproduces the run.

**D12. Commands and flags that are CLI-only.** Each has a stated reason, and
the Phase 5 tests enforce the list.

| CLI surface | Web equivalent |
| --- | --- |
| `version` | Footer and `GET /api/version`. |
| `update` | None. Replacing the running server binary from a browser request is out of scope; the footer says to run `forecast update`. |
| `serve` | N/A. |
| `-db` (deprecated alias of `-input`) | Hidden. Use `input`. (`linear sync`'s `-db` is *not* the alias; it is visible.) |
| `-stdin-format` | Hidden. There is no stdin; upload a file instead. |
| `-config` | Hidden as a field. Replaced by "Import config…" / "Export config" (Phase 9). |
| `-format`, `-out` | Hidden. Every format is offered as a download. |
| `-manifest` | Hidden. "Download manifest" is offered on sim items/days/probability. |

**D13. No new analysis behaviour.** This plan adds no analysis features,
validations or defaults. It reuses `internal/app` as is. The few additions
to analysis packages (exported formatting helpers in Phase 8, `Summarize`
in Phase 4) are pure, additive and change no existing output.

**D14. Plotly is vendored and embedded, so the UI works offline.**
- `internal/web/static/vendor/plotly-2.35.2.min.js` is checked in and
  embedded with the rest of `static/`. It is the version
  `cfd/template.html` loads from the CDN, so the CLI report and the web UI
  draw charts identically. Phase 4 adds it.
- The web UI never loads anything from the internet. The page CSP is
  `script-src 'self'`, with no CDN host.
- **The CLI's `cfd` HTML report is unchanged.** A standalone file saved by
  `forecast cfd` (or downloaded from the web UI) still loads Plotly from the
  CDN, byte-identical to today. Only the copy *shown inside* the web UI
  (`?inline=1`, §5.6) has its CDN `<script>` tag rewritten to the local
  copy.
- **Upgrading Plotly** is a deliberate change. Bump the file, the
  `plotlyVersion`/`plotlySHA256` constants (§4.9) and `cfd/template.html`
  together. A test (§5.10) fails if the template and the vendored version
  disagree.

---

## 3. Allowed CLI behaviour changes (the complete list)

1. `forecast` with no arguments lists the new `serve` command in its usage
   text.

Nothing else. Every golden case must stay byte-identical through every
phase.

---

## 4. Target layout and API

```
internal/flagvals/        [Phase 1] IntList, StringList, Engineers (flag.Value types, moved from cmd/forecast)
internal/report/          [Phase 1] CLI text renderers (moved from cmd/forecast), io.Writer-based
internal/params/          [Phases 2–3] per-command flag registration + Resolve, shared by CLI and web
  input.go sim.go sim_items.go sim_days.go sim_probability.go sim_backtest.go
  aging.go cfd.go count.go history.go check.go linear.go flags.go manifest.go cli.go
internal/web/             [Phases 4–9]
  server.go security.go paths.go datasets.go uploads.go files.go summary.go version.go static.go
  commands.go meta.go run.go store.go dto.go export.go cmd_sim.go cmd_flow.go cmd_data.go
  linear.go config.go
  static/index.html static/css/app.css static/js/*.js static/js/views/*.js
  static/vendor/plotly-2.35.2.min.js static/vendor/README.md
cmd/forecast/serve.go     [Phase 4]
```

**HTTP API** (all JSON unless noted; `{id}` is a command id from §6.1):

| Method and path | Phase | Purpose |
| --- | --- | --- |
| `GET /` and static assets | 4 | The embedded UI. |
| `GET /api/version` | 4 | `{"version": "...", "binary": {...}}` |
| `GET /api/defaults` | 4 | `{"input": "...", "exclusions": "..."}` from `serve`'s flags. |
| `GET /api/files?kind=input\|exclusions\|db` | 4 | Candidate files for path fields. |
| `POST /api/uploads` (multipart) | 4 | Store a file, return `{"path","name","size"}`. |
| `GET /api/datasets/summary?input=PATH` | 4 | Teams, projects, milestones, assignees, readiness. |
| `GET /api/commands` | 5 | Command list + generated form schema. |
| `POST /api/run/{id}` | 5 | Run a command (D5). |
| `GET /api/runs/{run_id}/export/{format}` | 5 | Download a stored run's output (D6). |
| `GET /api/linear/teams` | 9 | Shortcut for the teams list used by the sync form. |
| `POST /api/config/parse` | 9 | YAML config → params. |
| `POST /api/config/render` | 9 | Current params → `-config` YAML and the equivalent CLI command. |
| `POST /api/cli/parse` | 9 | Pasted CLI command → params. |

**Error body** (HTTP 400/403/404/413/500):

```json
{"error": {"message": "-days must be positive", "fields": ["days"]}, "warnings": []}
```

`fields` is present (possibly empty) only for `*app.FieldError`. `warnings`
is always present, possibly empty.

---

## Phase 0: commit this plan

```bash
jj new app-layer-extraction
# (WEB_UI_PLAN.md is already in the working copy if you are reading it there;
#  otherwise create it)
jj commit -m "web ui: add WEB_UI_PLAN.md"
jj bookmark create web-ui -r @-
jj log -n 3
```

---

## Phase 1: `internal/flagvals` and `internal/report`

Pure moves. The CLI behaves identically.

### 1.1 `internal/flagvals`

Move these three `flag.Value` types out of `cmd/forecast`:

| From | To |
| --- | --- |
| `cmd/forecast/intlist.go` `intList` | `internal/flagvals/intlist.go` `IntList` |
| `cmd/forecast/stringlist.go` `stringList` | `internal/flagvals/stringlist.go` `StringList` |
| `cmd/forecast/engineers.go` `engineerSpec` | `internal/flagvals/engineers.go` `Engineers` (methods `Count()`, `Names()` unchanged) |

- Move their tests too (`intlist_test.go`, `stringlist_test.go`,
  `engineers_test.go`) into `internal/flagvals`, renaming types.
- **Exception:** a test that exercises the CLI rather than the type (for
  example `TestEngineerSpec_ConfigListForm`, if it goes through a
  `FlagSet` + `util.ApplyConfig`) still moves, but builds its own
  `flag.FlagSet` in the test.
- Update every reference in `cmd/forecast`. Keep the error strings
  byte-identical (`invalid integer %q: %w`, and whatever
  `simulate.ParseEngineers` returns).

### 1.2 `internal/report`

Create `internal/report/doc.go`:

```go
// Package report renders internal/app results as the forecast CLI's plain
// text (and CSV) output. Every function writes to the io.Writer it is given,
// so the CLI (os.Stdout) and the web server (a buffer) produce byte-identical
// output. Pure: no os, no clock, no logging.
package report
```

Move these out of `cmd/forecast`. Replace `fmt.Printf(...)` with
`fmt.Fprintf(w, ...)`, `fmt.Println` with `fmt.Fprintln(w, ...)`, and
`tabwriter.NewWriter(os.Stdout, ...)` with `tabwriter.NewWriter(w, ...)`.

| From (`cmd/forecast`) | To (`internal/report`) |
| --- | --- |
| the printing tail of `cmdSimItems` (from the header `Printf` through `w.Flush()`) | `func SimItems(w io.Writer, res *app.ItemsResult)` |
| the printing tail of `cmdSimDays` including `printTrajectoryReport` | `func SimDays(w io.Writer, res *app.DaysResult, items []int)`: branch on `len(items) > 1` exactly as the CLI does; trajectory → unexported `trajectory(w, res)` |
| the printing tail of `cmdSimProbability` | `func SimProbability(w io.Writer, res *app.ProbabilityResult)` |
| `printBacktestText` | `func BacktestText(w io.Writer, res *app.BacktestResult, sampleStart, sampleEnd, today time.Time)` |
| `printBacktestCSV` | `func BacktestCSV(w io.Writer, res *app.BacktestResult) error` |
| the printing in `cmdCheck` | `func Check(w io.Writer, n int, input string, readiness []app.CheckResult)` (the "Read %d issues from %s" line and the per-command lines) and `func Exclusions(w io.Writer, path string, r app.ExclusionsReport)` (the "Exclusions: %s" line and the indented lines) |
| `checkExclusionsLines` | `func ExclusionsLines(r app.ExclusionsReport) []string` (keep it exported; `Exclusions` calls it) |
| `describeWindow` | unexported `describeWindow` in `report` |

Move `TestDescribeWindow` and the `checkExclusionsLines` tests
(`TestCheckExclusions_*` in `check_test.go`) to `internal/report`.
`cmd/forecast/check_test.go` keeps the `TestCmdCheck_*` tests.

### 1.3 `source.ReadExclusions`

`cmdCheck` reads the exclusions file with `os.ReadFile` +
`simulate.ParseExclusions`, and keeps the error as a *finding*. Move those
lines into `internal/source/source.go` so the web server can share them:

```go
// ReadExclusions reads and parses an exclusions file, returning the read or
// parse error unwrapped. Unlike LoadExclusions it is meant for `check`,
// which reports the error as a finding rather than failing.
func ReadExclusions(path string) (simulate.Exclusions, error)
```

`cmdCheck` calls it. Add a test with a missing file, an invalid file and a
valid file. Expected error text equals `os.ReadFile`'s / `ParseExclusions`'s
own.

### 1.4 Acceptance

- `just test` passes and `TestGolden` is unchanged.
- `cmd/forecast` no longer contains `tabwriter` or `encoding/csv` imports in
  `sim.go`, `backtest.go` or `check.go`.

---

## Phase 2: `internal/params` for the sim commands

This is the core of the plan. Read all of it before starting.

### 2.1 What a "params bundle" is

For every command, `internal/params` has three things:

- **A struct holding the command's flag pointers.**
- **A constructor `NewX(fs *flag.FlagSet, ...)`** that registers exactly the
  flags `cmdX` registers today, with identical names, defaults and usage
  strings, *including* `-config`.
- **A method `Resolve(now time.Time)`** that turns the parsed flags into a
  validated `app` request. It runs the steps `cmdX` runs today between
  `util.ApplyConfig` and the data load, **in the same order with the same
  error strings**.

The shared result type, in `internal/params/params.go`:

```go
// Resolved is what a command's Resolve produces: the input path to load, any
// warnings to show before anything else, and the validated request.
// Warnings are valid even when Resolve also returns an error.
type Resolved[R any] struct {
	InputPath string
	Warnings  []string
	Request   R
}
```

Errors that blame one flag become `*app.FieldError`.
- `Msg` is the **exact** string the CLI produces today.
- Turn `fmt.Errorf("invalid -sample-start date: %w", err)` into
  `&app.FieldError{Fields: []string{"sample-start"}, Msg: fmt.Sprintf("invalid -sample-start date: %v", err)}`.
  `%v` and `%w` render identically.
- Errors returned by `req.Validate()` are already `FieldError`s; return them
  unchanged.

### 2.2 `params.go`, `input.go`, `flags.go`

Move these helpers from `cmd/forecast/common.go` and `source.go`, exporting
them:

```go
// IsSet reports whether flag name was explicitly set on fs: on the command
// line, through -config (util.ApplyConfig uses fs.Set), or by the web server
// (which also uses fs.Set).
func IsSet(fs *flag.FlagSet, name string) bool            // was isFlagSet

// ResolveSeed returns seed if -random-seed was set, otherwise now.UnixNano().
func ResolveSeed(fs *flag.FlagSet, seed int64, now time.Time) int64   // was resolveSeed

// AddConfig registers -config.                              // was addConfigFlag
func AddConfig(fs *flag.FlagSet) *string
// AddTeams registers -teams with the given usage.           // was addTeamsFlag
func AddTeams(fs *flag.FlagSet, usage string) *linear.TeamKeyList

// FlagRecords snapshots every flag in fs for a run manifest. // was flagRecords
func FlagRecords(fs *flag.FlagSet) []app.FlagRecord
```

`input.go`:

```go
// Input is the -input / -db / -stdin-format block. DB is nil for commands
// that never had the deprecated -db alias (history, check).
type Input struct {
	Path        *string
	DB          *string
	StdinFormat *string
}

// AddInput registers -input and -stdin-format, plus -db when withDB is set,
// with the CLI's existing usage strings.
func AddInput(fs *flag.FlagSet, withDB bool) Input

// Resolve returns -input, or the deprecated -db with the warning
// "-db is deprecated; use -input instead". With neither set it returns
// &app.FieldError{Fields: []string{"input"}, Msg: "-input is required"}.
func (in Input) Resolve() (path string, warnings []string, err error)
```

`addDBFlag` and `requireDB` stay where `linear sync` can use them; Phase 3
moves them into `params/linear.go`.

### 2.3 `sim.go`

Move `simFlags` → `params.SimFlags` (exported, same fields), `addSimFlags`
→ `params.AddSim`, `newSimBase` → `(sf *SimFlags) Base(fs *flag.FlagSet,
sampleStart, sampleEnd, now time.Time) app.SimBase`, and
`parseTargetWindowFlags` → unexported `parseTargetWindow` in `params`.
`SimFlags.Engineers` becomes `flagvals.Engineers` and `TypicalEngineers`
becomes `flagvals.StringList`.

Move `simManifest`, but change its signature so the adapter supplies what
only it knows:

```go
// ManifestContext is what only an adapter knows about a run: how it was
// invoked, the binary, the input file's fingerprint and the clock.
type ManifestContext struct {
	Invocation  app.Invocation
	Binary      app.BinaryInfo
	Data        app.DataFile
	GeneratedAt time.Time
}

// SimManifest assembles the manifest inputs for a finished sim run.
func SimManifest(subcommand string, fs *flag.FlagSet, sf *SimFlags, run app.SimRun, extra map[string]any, mc ManifestContext) app.ManifestInputs
```

`cmd/forecast` keeps `writeManifest`, `fingerprintForManifest` and
`writeSimManifest`. `writeSimManifest` now builds the `ManifestContext`
(`app.Invocation{Subcommand, os.Args, wd}`, `buildInfo()`, the fingerprint,
`nowFunc()`) and calls `params.SimManifest`.

### 2.4 Manifest extras move into `app`

The `map[string]any` literals passed to `writeSimManifest` in `sim.go`
become pure methods. Their keys and values must be identical:

```go
// ManifestExtra returns the sim items-specific manifest fields.
func (r *ItemsResult) ManifestExtra() map[string]any        // effective_confidence_levels, target_start_date, target_end_date, effective_days
func (r *DaysResult) ManifestExtra() map[string]any         // effective_confidence_levels, target_start_date
func (r *ProbabilityResult) ManifestExtra() map[string]any  // target_start_date, target_end_date, effective_days
```

Add a test per method in `internal/app`.

### 2.5 The four sim bundles

**`sim_items.go`:**

```go
// SimItems holds the flags of `forecast sim items`.
type SimItems struct {
	Input       Input
	Sim         *SimFlags
	Days        *int
	TargetStart *string // -target-start-date
	TargetEnd   *string // -target-end-date
	Confidence  flagvals.IntList
	Manifest    *string
	Config      *string
	fs          *flag.FlagSet
}

func NewSimItems(fs *flag.FlagSet) *SimItems
func (c *SimItems) Resolve(now time.Time) (Resolved[app.ItemsRequest], error)
```

`Resolve` order (today's `cmdSimItems`):
1. `c.Input.Resolve()`
2. Parse `-sample-start`, then `-sample-end`.
3. `parseTargetWindow`.
4. Build the request with `c.Sim.Base(...)`.
5. `req.Validate()`.

**`sim_days.go`:** `SimDays` has fields `Input`, `Sim`, `Items
flagvals.IntList`, `TargetStart`, `Confidence`, `Manifest` and `Config`.

`Resolve` order:
1. input;
2. sample-start, then sample-end;
3. `-target-start-date` (`invalid -target-start-date: %w`);
4. build;
5. `Validate`.

**`sim_probability.go`:** `SimProbability` has fields `Input`, `Sim`,
`Days`, `TargetStart`, `TargetEnd`, `Items *int` (default `-1`), `Manifest`
and `Config`. `Resolve` follows the same order as items.

**`sim_backtest.go`:** `SimBacktest` has fields `Input`, `Sim`, `Project`,
`Milestone`, `ReplayStart`, `TargetEnd`, `Format` and `Config`.
- `NewSimBacktest` keeps the `-simulations` usage override.
- `Resolve` order:
  1. input;
  2. `-target-end-date` (if non-empty);
  3. `-sample-start`, then `-sample-end`. Note this command's messages are
     `invalid -sample-start: %w` with no "date"; keep them;
  4. `-replay-start-date` (if non-empty);
  5. build, with `Today: util.LocalDay(now)`;
  6. `Validate`;
  7. the format check, returning
     `&app.FieldError{Fields: []string{"format"}, Msg: `-format must be "text" or "csv"`}`.

### 2.6 Rewire the CLI

Each `cmdSimX` becomes:

```go
cmd := flag.NewFlagSet("sim items", flag.ExitOnError)
c := params.NewSimItems(cmd)
cmd.Parse(args)
if err := util.ApplyConfig(cmd, *c.Config); err != nil {
	return err
}
r, err := c.Resolve(nowFunc())
printWarnings(r.Warnings)
if err != nil {
	return err
}
req := r.Request
// ... loadSimInputs(&req.SimBase, r.InputPath, *c.Input.StdinFormat, *c.Sim.ExclusionsFile)
// ... fingerprint, progress bar, app.SimItems, printWarnings, writeSimManifest(..., res.ManifestExtra()),
// ... report.SimItems(os.Stdout, res)
```

Move `TestParseTargetWindowFlags`, `TestResolveSeed` and `TestFlagRecords`
to `internal/params`, adapted to the new names.

### 2.7 Tests (new, in `internal/params`)

- **For each sim bundle, a table test:**
  - Build a `FlagSet`, call `NewX`, then `fs.Parse(args)`, then
    `Resolve(fixedNow)`.
  - Assert the request fields for a happy path.
  - Assert the exact error string and `FieldError.Fields` for:
    - one bad date;
    - `-days` + `-target-end-date` together;
    - no mode.
- **`TestResolve_SetEqualsParse`:** for `sim items`, configure one
  `FlagSet` with `fs.Parse([]string{"-engineers","3","-days","30"})` and
  another with `fs.Set("engineers","3"); fs.Set("days","30")`. Both must
  resolve to the same request, compared with `reflect.DeepEqual` on the
  request minus `Seed`, `Issues` and `Progress`. This is the property the
  web server relies on.
- **Input warning:** `-db x.db` alone resolves with
  `Warnings == []string{"-db is deprecated; use -input instead"}`.

### 2.8 Acceptance

- `TestGolden` unchanged and `just test` green.
- `grep -n 'fs.String\|cmd.String\|cmd.Int\|cmd.Var\|cmd.Bool' cmd/forecast/sim.go cmd/forecast/backtest.go`
  prints nothing: no flag registration is left in the sim adapters.

---

## Phase 3: `internal/params` for the other commands

Same pattern as Phase 2. Constructors register exactly today's flags.
`Resolve` runs exactly today's steps in today's order.

| Bundle | Constructor | Resolve order (today's) | Request type | Adapter-only fields |
| --- | --- | --- | --- | --- |
| `Aging` | `NewAging(fs)` | input → `-min-cycle-time` (only if non-empty; `invalid -min-cycle-time %q: %w`) → `-sample-end` → `-sample-start` → build (`Today: util.LocalDay(now)`) → `Validate` | `app.AgingRequest` | `Format` (checked at render, as today) |
| `CFD` | `NewCFD(fs)` | input → `-end` → `-start` → build → `Validate` | `app.CFDRequest` | `Format`, `Out` |
| `Count` | `NewCount(fs, now)` (the `-updated-since` default is `now.AddDate(0,-3,0).Format("2006-01-02")`, computed from the `now` argument) | input → `-updated-since` → build → `Validate` | `app.CountRequest` | `Milestones` |
| `History` | `NewHistory(fs)` (Input without DB) | format check (`-format must be "csv", "json", or "text"` → FieldError `format`) → input → `-end` → `-start` (if non-empty) → build → `Validate` | `app.HistoryRequest` | `Format`, `Out` |
| `Check` | `NewCheck(fs)` (Input without DB) | input | `params.CheckRequest{Exclusions string}` | — |
| `LinearSync` | `NewLinearSync(fs)` | `-db` required (`-db is required` → FieldError `db`) | `syncer.Options` (`InputPath` = the db path) | — |
| `LinearTeams` | `NewLinearTeams(fs)` (only `-config`) | — | — | — |

Notes:

- `cmdCount` computes `defaultSince` from `nowFunc()` before creating the
  `FlagSet`. Pass `nowFunc()` to `NewCount`, and pass the same `now` value
  to `Resolve`. Today `cmdCount` calls `nowFunc()` twice; keeping that is
  fine because the golden clock is fixed.
- `LinearSync` and `LinearTeams` have no `Resolve` that touches the API.
  `linear.GetAPIKey()`, `linear.New` and `sqlite.Open` stay in the adapter.
- Move `addDBFlag`/`requireDB` into `params/linear.go`. `-db` registration
  for the other commands goes through `AddInput(fs, true)`.
- Rewire every `cmdX` exactly as in §2.6. After this phase, `cmd/forecast`
  contains no `fs.String`/`fs.Int`/`fs.Bool`/`fs.Var` calls except in
  `serve.go` (Phase 4), `update.go` and `version.go`.

**Tests:** one table test per bundle (happy path + every error branch's
exact string and `Fields`). Also extend `TestResolve_SetEqualsParse` to
`aging` (with `-teams eng`, which must upper-case through `fs.Set` too) and
`history`.

**Acceptance:** `TestGolden` unchanged; `just test` green; the grep from
§2.8 extended to `aging.go cfd.go count.go history.go check.go linear.go`
prints nothing.

---

## Phase 4: `internal/web` skeleton, `forecast serve`, data endpoints

### 4.1 `app.Summarize` (pure)

`internal/app/summary.go`:

```go
// DatasetSummary describes a loaded issue set for the web UI's pickers.
type DatasetSummary struct {
	Issues          int
	Teams           []string           // DistinctTeamKeys
	Projects        []ProjectSummary   // sorted by name; "" project omitted
	Assignees       []string           // sorted, non-empty
	EarliestCreated time.Time          // zero if none
	LatestUpdated   time.Time          // zero if none
	Readiness       []CheckResult      // CheckReadiness
}

// ProjectSummary is one project and its milestone names (sorted, "" omitted).
type ProjectSummary struct {
	Name       string
	Milestones []string
}

// Summarize computes a DatasetSummary. Pure.
func Summarize(items []issues.Issue) DatasetSummary
```

Test it against a hand-built slice: two teams, two projects (one with two
milestones), one unassigned issue.

### 4.2 `cmd/forecast/serve.go`

Add `{Name: "serve", Summary: "Start the local web UI.", Run: cmdServe}` to
`topCommands` (this is §3's one allowed change). Flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `-addr` | `127.0.0.1:8080` | listen address |
| `-input` | `""` | default data source pre-filled in the UI |
| `-exclusions` | `""` | default exclusions file pre-filled in sim forms and check |
| `-upload-dir` | `""` → `filepath.Join(os.UserCacheDir(), "delivery-forecast", "uploads")` | where uploads are stored (created with `0o700` if missing) |
| `-max-upload-mb` | `512` | upload size limit |
| `-allow-remote` | `false` | allow a non-loopback `-addr` |
| `-data-dir` | `""` | with `-allow-remote`: the only directory paths may point into (required then) |
| `-open` | `false` | open the UI in the default browser after the listener is up (`open` on darwin, `xdg-open` on linux, `rundll32 url.dll,FileProtocolHandler` on windows; failure is a warning) |
| `-config` | | as every command |

Steps:
1. Parse.
2. `util.ApplyConfig`.
3. Validate the flags:
   - If the host of `-addr` is not a loopback host and `-allow-remote` is
     false, fail with
     `-addr <addr> is not a loopback address; pass -allow-remote to listen on it`.
     Loopback hosts are `localhost`, `127.0.0.0/8` and `::1`; an empty host
     means all interfaces, so it is non-loopback.
   - If `-allow-remote` is set without `-data-dir`, fail with
     `-allow-remote requires -data-dir`.
4. Build `web.Options` (§4.3) with:
   - `Binary: buildInfo()`;
   - `Version`: the same `version`-or-`(dev)` string `cmdVersion` prints;
   - `Now: nowFunc`.
5. Listen with `net.Listen` *before* logging, so a port conflict fails
   fast.
6. Log `logx.Infof("forecast web UI: http://%s/", ln.Addr())`.
7. Serve until `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`
   fires, then shut down with `http.Server.Shutdown` and a 5s timeout.

### 4.3 `internal/web/server.go`

```go
// Options configures a Server. Zero values get the defaults noted.
type Options struct {
	DefaultInput      string
	DefaultExclusions string
	UploadDir         string          // required
	MaxUploadBytes    int64           // default 512 MiB
	AllowRemote       bool
	DataDir           string          // required when AllowRemote
	Binary            app.BinaryInfo
	Version           string
	Now               func() time.Time // default time.Now
	Logf              func(format string, args ...any) // default: discard
}

// Server is the forecast web UI's HTTP server. Create it with New.
type Server struct { /* opts, mux, datasets, runs, runSlots, ... */ }

func New(opts Options) (*Server, error)
// Handler returns the server's root handler with the security middleware applied.
func (s *Server) Handler() http.Handler
```

`cmd/forecast/serve.go` wraps `s.Handler()` in an `http.Server`. Tests use
`httptest.NewServer(s.Handler())`, or call the handler directly with
`httptest.NewRecorder`.

### 4.4 `security.go`

Middleware applied to every request, in this order:

1. **Host check** (skipped when `AllowRemote`). `r.Host` without its port
   must be `localhost`, `127.0.0.1` or `[::1]`/`::1`. Otherwise **403**
   with message `forbidden host`. This blocks DNS-rebinding.
2. **Client header.** For any method other than `GET` and `HEAD`, require
   `X-Forecast-Client: web`. Otherwise **403** with message
   `missing X-Forecast-Client header`. `OPTIONS` therefore always gets 403,
   so there is no CORS.
3. **Response headers** on every response:
   - `X-Content-Type-Options: nosniff`
   - `Referrer-Policy: no-referrer`
   - `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-src 'self'`

   Export responses override CSP (§5.6). If Plotly charts fail with a CSP
   error in the console, add `'unsafe-eval'` to `script-src` and note it in
   your phase report. Do not loosen anything else.

### 4.5 `paths.go`

```go
// resolvePath validates a user-supplied path parameter (field names the flag,
// for FieldError). "-" is rejected ("-<field> -: stdin is not available in
// the web UI; upload the file instead"). Relative paths are resolved against
// the server's working directory, exactly as the CLI resolves them. With
// AllowRemote, the cleaned, symlink-evaluated path must lie inside DataDir or
// UploadDir ("-<field>: path is outside -data-dir"). Returns the absolute path.
func (s *Server) resolvePath(field, p string) (string, error)
```

### 4.6 `datasets.go`

```go
type datasetKey struct {
	path             string
	size             int64
	mod              time.Time
	walSize          int64     // SQLite only: path+"-wal", zero if absent
	walMod           time.Time
}

type dataset struct {
	key    datasetKey
	issues []issues.Issue  // shared read-only
	file   app.DataFile    // source.Fingerprint at load time
}

// get returns the cached dataset for absPath, loading it (source.Load with
// format "" and nil stdin, then source.Fingerprint) on a miss or when the
// key changed. Holds at most 4 entries, evicting the least recently used.
// Loads are serialized by one mutex.
func (c *datasetCache) get(ctx context.Context, absPath string) (*dataset, error)

// invalidate drops absPath's entry (linear sync calls this after writing).
func (c *datasetCache) invalidate(absPath string)
```

A missing file is an error. Wrap it as
`&app.FieldError{Fields: []string{field}, Msg: err.Error()}` at the call
site, where `field` is `input` or `db`.

### 4.7 `uploads.go`: `POST /api/uploads`

- Request: multipart form with a field named `file`. Wrap the body with
  `http.MaxBytesReader(MaxUploadBytes)`. A too-large body returns **413**
  `upload exceeds -max-upload-mb`.
- **Extension check:** lower-cased, the extension must be one of `.csv`,
  `.json`, `.jsonl`, `.ndjson`, `.db`, `.sqlite`, `.sqlite3`. Otherwise
  **400** `unsupported file type %q`.
- **Storing the file:**
  1. Stream it to a temp file in `UploadDir` while hashing it with sha256.
  2. The final name is `<first 12 hex chars of the hash>-<sanitized base name>`.
     Sanitizing replaces every rune outside `[A-Za-z0-9._-]` with `_`.
  3. If the final name already exists, delete the temp file: it's the same
     content. Otherwise rename the temp file into place.
- Response: `{"path": "<absolute path>", "name": "<original file name>", "size": <bytes>}`.

### 4.8 `files.go`: `GET /api/files?kind=input|exclusions|db`

- **Root:** `DataDir` when `AllowRemote`, else the working directory.
- **Walk:** depth ≤ 2. Skip directories whose name starts with `.`. Stop
  after 500 matches.
- **Extensions:**
  - `input`: all data extensions from §4.7;
  - `exclusions`: `.json`;
  - `db`: `.db`, `.sqlite`, `.sqlite3`.
- Also list `UploadDir`'s files (not recursive) under `uploads`.
- **Paths:** relative to the working directory when inside it, else
  absolute.
- Response:

  ```json
  {"root": "...", "files": [{"path": "...", "size": 1, "modified": "RFC3339"}], "uploads": [...]}
  ```

### 4.9 Other handlers

- **`GET /api/version`:** `{"version": opts.Version, "binary": {...}}`. Use
  snake_case keys for the binary fields: `version_override`, `git_sha`,
  `git_time`, `dirty`, `go_version`, `module`. Write a small DTO; don't add
  JSON tags to `app.BinaryInfo`.
- **`GET /api/defaults`:** `{"input": ..., "exclusions": ...}`.
- **`GET /api/datasets/summary?input=PATH`:** `resolvePath("input", …)` →
  `datasets.get` → `app.Summarize`. Response:

  ```json
  {"path": "...", "issues": 0, "teams": [], "projects": [{"name": "...", "milestones": []}],
   "assignees": [], "earliest_created": "YYYY-MM-DD" | null, "latest_updated": "YYYY-MM-DD" | null,
   "readiness": [{"command": "...", "excluded": 0, "status": "..."}]}
  ```

- **Static files (`static.go`):**
  - `//go:embed static` serves `static/` at `/` through
    `http.FileServerFS(fs.Sub(...))`, with `Cache-Control: no-cache`.
  - For this phase, `static/index.html` is a placeholder page titled
    "Forecast" that fetches `/api/version` and prints it.
- **Vendored Plotly (D14):**
  1. Download `plotly-2.35.2.min.js` from **two** independent sources:
     `https://cdn.plot.ly/plotly-2.35.2.min.js` and
     `https://cdn.jsdelivr.net/npm/plotly.js-dist-min@2.35.2/plotly.min.js`.
     Put each in its own new, empty scratch directory.
  2. Compute `shasum -a 256` of both. **They must be identical.** If they
     differ, stop and report; don't pick one.
  3. Copy the file to `internal/web/static/vendor/plotly-2.35.2.min.js`.
     Never edit it.
  4. Write `internal/web/static/vendor/README.md` recording the version,
     both source URLs, the sha256, Plotly's licence (MIT) and the upgrade
     rule from D14.
  5. In `static.go`, add:

     ```go
     // plotlyVersion and plotlySHA256 pin the vendored Plotly bundle (D14).
     const (
     	plotlyVersion = "2.35.2"
     	plotlySHA256  = "<the sha256 from step 2>"
     )

     // plotlyPath is where the UI loads Plotly from.
     const plotlyPath = "/vendor/plotly-" + plotlyVersion + ".min.js"
     ```

  6. Serve `/vendor/` with `Cache-Control: public, max-age=31536000, immutable`
     instead of `no-cache`; the file name carries the version.

### 4.10 Shared helpers (`server.go`)

```go
// writeJSON writes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any)
// writeError writes the §4 error body; a *app.FieldError contributes "fields".
func writeError(w http.ResponseWriter, status int, err error, warnings []string)
```

### 4.11 Tests (`internal/web/*_test.go`)

The test working directory is `internal/web`, so the sample data is at
`../../testdata/sample-issues.csv`.

- **Security:**
  - a `Host: evil.example` GET returns 403;
  - a `Host: localhost:8080` GET returns 200;
  - a POST without `X-Forecast-Client` returns 403;
  - `OPTIONS` returns 403;
  - the CSP header is present.
- **Version:** `/api/version` returns the injected values.
- **Uploads:**
  - success, and the file content is stored;
  - the same content twice gives the same path;
  - a bad extension returns 400;
  - an oversize upload returns 413 (set `MaxUploadBytes` to 10).
- **Files:** a temp dir with `a.csv`, `.hidden/b.csv` and `x/y/z/deep.csv`
  lists only `a.csv` (depth > 2 is skipped).
- **Paths:**
  - `resolvePath` rejects `-`;
  - with `AllowRemote` and `DataDir=tmp`, it rejects `/etc/hosts` and
    accepts `tmp/a.csv`.
- **Dataset cache:**
  - two gets return the same pointer;
  - rewriting the file with different content (and a bumped modtime via
    `os.Chtimes`) returns a new pointer;
  - a fifth path evicts the least recently used one.
- **Summary:** for the sample CSV, `issues` equals the CSV's data-row
  count, and `teams` equals the distinct `team_key` values in the file.
  Read the file to get the expected values, and hard-code them in the test.
- **Vendored Plotly:** `TestPlotly_Vendored` reads `static/vendor/plotly-<plotlyVersion>.min.js`
  from the embedded FS and asserts its sha256 equals `plotlySHA256`, and that
  `GET /vendor/plotly-<plotlyVersion>.min.js` returns 200 with the
  `immutable` cache header.
- **Serve flags** (in `cmd/forecast`): `-addr 0.0.0.0:8080` without
  `-allow-remote` returns the exact error. Test the validation function
  directly; don't start a listener.

**Manual check:** `just build && bin/forecast serve -open`. The placeholder
shows the version.

---

## Phase 5: command schema, runs, exports, parity tests

### 5.1 Command registry (`commands.go`)

```go
// commandDef is one CLI command exposed through the web API.
type commandDef struct {
	ID       string   // URL segment, e.g. "sim-items"
	Name     string   // CLI name, e.g. "sim items"
	Summary  string   // identical to the CLI's command-table Summary
	Group    string   // "forecast", "flow" or "data"
	Title    string   // page title
	Exports  []string // formats, first = the one shown as "Text output"
	Stream   bool     // reports progress events
	newFlags func(fs *flag.FlagSet, now time.Time) any        // calls params.NewX, returns the bundle
	prepare  func(rc *runContext, bundle any) (*runner, error) // see 5.3
}
```

The registry, in this order (it is also the navigation order):

| ID | CLI name | Group | Title | Exports | Stream |
| --- | --- | --- | --- | --- | --- |
| `sim-days` | sim days | forecast | When will it be done? | text, json, manifest | yes |
| `sim-items` | sim items | forecast | How much can we finish? | text, json, manifest | yes |
| `sim-probability` | sim probability | forecast | What are the odds? | text, json, manifest | yes |
| `sim-backtest` | sim backtest | forecast | How did past forecasts hold up? | text, csv, json | yes |
| `history` | history | flow | Day-by-day history | text, csv, json | no |
| `cfd` | cfd | flow | Cumulative flow | html, json | no |
| `aging` | aging | flow | What's aging in progress? | text, html, json | no |
| `count` | count | flow | What's still open? | text, json | no |
| `check` | check | data | Check my data | text, json | no |
| `linear-sync` | linear sync | data | Sync from Linear | — | yes (log) |
| `linear-teams` | linear teams | data | Linear teams | json | no |

`linear-sync` and `linear-teams` are registered in Phase 9. Until then they
are absent from the registry, and the Phase 5 parity test lists them as
pending.

### 5.2 UI hints (`meta.go`)

```go
// fieldMeta is the web UI's decision about one flag.
type fieldMeta struct {
	Hidden  string   // non-empty = CLI-only; the reason (shown nowhere, enforced by tests)
	Kind    string   // widget kind, see table
	Section string   // "data", "basic" or "advanced"
	Label   string
	Choices []string // Kind "choice" only
}

var metaByFlag = map[string]fieldMeta{ ... }           // applies to every command
var metaOverrides = map[string]map[string]fieldMeta{ ... } // command ID → flag → meta
```

`metaByFlag` (complete):

| Flag | Kind | Section | Label |
| --- | --- | --- | --- |
| `input` | `input-path` | data | Data source |
| `db` | Hidden: "deprecated alias of -input" | | |
| `stdin-format` | Hidden: "no stdin in the web UI" | | |
| `config` | Hidden: "replaced by config import/export" | | |
| `format` | Hidden: "every format is a download" | | |
| `out` | Hidden: "downloads replace -out" | | |
| `manifest` | Hidden: "Download manifest replaces -manifest" | | |
| `exclusions` | `exclusions-path` | data | Exclusions file |
| `engineers` | `engineers` | basic | Engineers (count or names) |
| `whole-team` | `bool` | basic | Whole team |
| `sample-start` | `date-start` | basic | Sample from |
| `sample-end` | `date-end` | basic | Sample to |
| `simulations` | `int` | advanced | Simulations |
| `goroutines` | `int` | advanced | Worker goroutines |
| `random-seed` | `int` | advanced | Random seed |
| `typical-engineers` | `assignee-list` | advanced | Typical engineers |
| `days` | `int` | basic | Days |
| `target-start-date` | `date-start` | basic | Forecast starts |
| `target-end-date` | `date-end` | basic | Forecast ends (inclusive) |
| `confidence` | `int-list` | basic | Confidence levels (%) |
| `items` | `int` | basic | Items |
| `project` | `project` | basic | Project |
| `milestone` | `milestone` | basic | Milestone |
| `replay-start-date` | `date-start` | basic | Replay from |
| `min-cycle-time` | `duration` | advanced | Minimum cycle time |
| `percentile` | `int` | basic | Percentile |
| `show-completed` | `bool` | basic | Show completed sample |
| `teams` | `team-list` | basic | Teams |
| `start` | `date-start` | basic | Start |
| `end` | `date-end` | basic | End |
| `updated-since` | `date-end` | basic | Updated since |
| `milestones` | `bool` | basic | Per-milestone breakdown |
| `window` | `int` | basic | Rolling window (days) |
| `all-teams` | `bool` | basic | All accessible teams |
| `full-reload` | `bool` | basic | Full reload |

`metaOverrides`:
- `sim-days.items` → `{Kind: "int-list", Section: "basic", Label: "Items (comma-separated for a grouped trajectory)"}`
- `linear-sync.db` → `{Kind: "db-path", Section: "data", Label: "Database file"}`
- `linear-sync.teams` → `{Kind: "string", Section: "basic", Label: "Teams"}`

### 5.3 Running a command (`run.go`)

```go
// runContext carries one run request through preparation.
type runContext struct {
	ctx      context.Context
	s        *Server
	def      *commandDef
	fs       *flag.FlagSet
	now      time.Time
	warnings []string // pre-run warnings (Resolve's), prepended to the result's
}

// loadInput resolves and loads an input-path parameter (field "input" or "db").
func (rc *runContext) loadInput(field, path string) (*dataset, error)

// runner executes a prepared run.
type runner struct {
	run func(ev events) (*runOutput, error)
}

// events is how a running command reports progress and log lines.
type events interface {
	Progress(done, total int)
	Log(msg string)
}

// runOutput is a finished run. Non-nil even alongside an error (Warnings valid).
type runOutput struct {
	Warnings []string
	Result   any               // JSON DTO (dto.go)
	Text     string            // the first export's output; "" if that export is html
	Exports  map[string]export // keyed by format
}

// export renders one download from a stored result.
type export struct {
	ContentType string
	Ext         string // file extension, e.g. ".txt"
	Write       func(w io.Writer) error
}
```

`POST /api/run/{id}`:

1. **Look up the command.** Unknown id → **404** `unknown command %q`.
2. **Decode the body.** `{"params": {...}}`, limited to 1 MiB with
   `http.MaxBytesReader`. A decode error → **400**.
3. **Build the flag set.** Create
   `fs := flag.NewFlagSet(def.Name, flag.ContinueOnError)` with
   `fs.SetOutput(io.Discard)`, then `bundle := def.newFlags(fs, now)`, where
   `now := s.opts.Now()` is taken once per request.
4. **Apply the params** in sorted key order, skipping empty values.
   - Hidden flag → **400** FieldError `{k}`:
     `-<k> is not available in the web UI`.
   - `fs.Lookup(k) == nil` → **400** FieldError `{k}`:
     `unknown parameter -<k>`.
   - `fs.Set(k, v)` fails → **400** FieldError `{k}`:
     `invalid value %q for -<k>: <err>`.
   - Path kinds (`input-path`, `exclusions-path`, `db-path`): call
     `s.resolvePath(k, v)` to validate. The flag keeps the user's original
     string, so the CLI command and manifest show what the user typed.
5. **Prepare.** Call `def.prepare(rc, bundle)`. An error →
   **400** `writeError(err, rc.warnings)`.
6. **Start the stream.** Status 200, `Content-Type: application/x-ndjson`,
   `Cache-Control: no-store`. Write each event as one JSON line, followed by
   `Flush()`.
7. **Acquire a run slot** (`s.runSlots`, capacity 1). If it isn't
   immediately free, emit `{"type": "queued"}`. If the request context is
   done while waiting, return without emitting anything more.
8. **Run.** Call `runner.run(ev)`.
   - **Progress events:** at most one every 100 ms, plus always the final
     `done == total`. Guard with a mutex, because progress callbacks arrive
     from several goroutines.
   - **Log events:** always emitted.
9. **Emit the terminal event.**
   - On error:
     `{"type": "error", "warnings": [...], "error": {"message", "fields"}}`.
   - On success: store the run (§5.5) and emit:

     ```json
     {"type": "result", "run_id": "...", "command": "sim-items", "warnings": [...],
      "result": {...}, "text": "...", "exports": ["text","json","manifest"],
      "cli": "forecast sim items -engineers 3 ...", "config": "engineers: \"3\"\n...",
      "flags": [{"name","value","default","set"}]}
     ```

   `warnings` = `rc.warnings` followed by `out.Warnings`. `flags` =
   `params.FlagRecords(fs)`, in snake_case JSON.

Event JSON shapes:
- `{"type": "progress", "done": 1200, "total": 10000}`
- `{"type": "log", "message": "..."}`

### 5.4 `params/cli.go`: CLI command and config text

```go
// CommandLine returns the argv equivalent to fs's explicitly set flags, in
// fs.VisitAll (sorted) order, prefixed by "forecast" and the command name
// split on spaces. Bool flags render as -name when true and -name=false
// when false. -config is never included.
func CommandLine(name string, fs *flag.FlagSet) []string

// ShellJoin quotes args for a POSIX shell: an arg made only of
// [A-Za-z0-9_./:=,@%+-] is left bare; anything else is single-quoted with
// embedded ' written as '\''.
func ShellJoin(args []string) string

// ConfigYAML renders fs's explicitly set flags (except -config) as a
// -config YAML document: a map of flag name → string value, marshaled
// with gopkg.in/yaml.v3 (which sorts keys).
func ConfigYAML(fs *flag.FlagSet) (string, error)
```

Tests:
- `CommandLine` + `ShellJoin` for `-project "Search Revamp" -whole-team`.
- **`ConfigYAML` round trip:** write the YAML to a temp file and apply it to
  a fresh `FlagSet` with `util.ApplyConfig`. Every flag value must be equal
  to the original.

### 5.5 Run store (`store.go`)

```go
// runStore keeps the last 50 successful runs, evicting the oldest.
type runStore struct { mu sync.Mutex; order []string; runs map[string]*storedRun }
type storedRun struct { ID, Command string; Created time.Time; Out *runOutput }
```

IDs are `"r"` + 12 lowercase hex chars from `crypto/rand`.

### 5.6 Exports (`export.go`)

`GET /api/runs/{run_id}/export/{format}`:
- An unknown run returns **404** `unknown run`. An unknown format returns
  **404** `format %q not available for this run`.
- **Headers:**
  - `Content-Type` comes from the export:
    - `text/plain; charset=utf-8` for text;
    - `text/csv; charset=utf-8` for csv;
    - `application/json` for json and manifest;
    - `text/html; charset=utf-8` for html.
  - `Content-Disposition` is
    `attachment; filename="forecast-<command>-<run_id><ext>"`. With
    `?inline=1` it is `inline` instead.
- **HTML exports, as downloads** (no `inline`) are served exactly as the
  renderer wrote them. That includes `cfd`'s CDN `<script>` tag, so the
  file is byte-identical to `forecast cfd`'s output.
- **HTML exports with `?inline=1`** are what the UI embeds in an iframe.
  For these:
  - Replace every occurrence of
    `<script src="https://cdn.plot.ly/plotly-` + plotlyVersion + `.min.js"></script>`
    with `<script src="` + plotlyPath + `"></script>`, using
    `bytes.ReplaceAll` on the rendered output.
  - Send the CSP
    `default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:`.
    `'unsafe-inline'` is needed because the report templates use inline
    scripts. They are rendered by `html/template`, so issue data is
    escaped.
  - The UI embeds them in a plain same-origin `<iframe>` with no `sandbox`
    attribute. A sandboxed iframe gets an opaque origin, so it couldn't load
    `/vendor/...` under `'self'`.

### 5.7 Per-command prepare functions

Each prepare function follows this shape: assert the bundle type, call
`Resolve(rc.now)`, append its warnings to `rc.warnings`, return any error,
load data, and return a runner whose `run` calls the `app` function and
builds the `runOutput`.

**`cmd_sim.go`:**

- **`sim-items`:**
  - Load the data with `rc.loadInput("input", r.InputPath)`.
  - Exclusions: `source.LoadExclusions(path)` after
    `resolvePath("exclusions", …)` when non-empty. An error becomes a
    FieldError on `exclusions` with the message unchanged.
  - `req.Issues` = `dataset.issues`; `req.Exclusions` = the loaded
    exclusions.
  - In `run`: set `req.Progress = ev.Progress`, then call `app.SimItems`.
  - **Exports:**
    - `text`: `report.SimItems`.
    - `json`: the DTO, indented 2 spaces.
    - `manifest`: `app.NewManifest(params.SimManifest("sim items", fs, c.Sim, res.Run, res.ManifestExtra(), mc))`,
      encoded like the CLI's `writeManifest` (json.Encoder,
      `SetIndent("", "  ")`).
  - The `ManifestContext` is:
    - `Invocation{Subcommand: "sim items", Args: params.CommandLine("sim items", fs), WorkingDir: os.Getwd()}`;
    - `Binary: s.opts.Binary`;
    - `Data: dataset.file`;
    - `GeneratedAt: rc.now`.

    Compute it when the run finishes, and encode lazily inside `Write`.
- **`sim-days`:** like items. `req.Progress = ev.Progress` only when
  `len(req.Items) == 1`, mirroring the CLI. Text is
  `report.SimDays(w, res, req.Items)`.
- **`sim-probability`:** like items. Text is `report.SimProbability`.
- **`sim-backtest`:**
  - Pass `req.Progress = ev.Progress`. The UI shows whatever `done`/`total`
    the engine reports; it may restart per replayed day.
  - **Exports:**
    - `text`: `report.BacktestText(w, res, req.SampleStart, req.SampleEnd, req.Today)`.
    - `csv`: `report.BacktestCSV`.
    - `json`: the DTO.
  - The `Format` field was already validated by `Resolve`. Ignore its value
    otherwise.

**`cmd_flow.go`:**

- **`aging`:**
  - `app.RunAging`.
  - **Exports:**
    - `text`: `aging.RenderText(w, res.InProgress, res.Completed, req.ShowCompleted, res.Meta)`.
    - `html`: `aging.RenderHTML(...)`, same arguments.
    - `json`: `aging.RenderJSON(w, res.InProgress, res.Meta)`. That is the
      CLI's JSON, not the DTO.
- **`cfd`:**
  - `app.RunCFD`.
  - **Exports:**
    - `html`: `cfd.RenderHTML(w, res.Rows, res.Health, res.Health.TotalIssues, res.Health.SkippedIssues, req.Start, req.End)`.
    - `json`: `cfd.RenderJSON(w, res.Rows, res.Health)`.
  - `Text` is `""`, because the first export is html.
- **`history`:** `app.RunHistory`. Exports: `text` = `history.RenderText`,
  `csv` = `history.RenderCSV`, `json` = `history.RenderJSON`, each on
  `res.Result`.
- **`count`:** `app.RunCount`.
  - `text`: `counts.RenderGrouped` when `*c.Milestones` is true, else
    `counts.RenderSummary`.
  - `json`: the DTO.

**`cmd_data.go`:**

- **`check`:**
  - **Load the input.**
  - **Exclusions** (when set):
    1. `resolvePath`. A failure here is a 400, because it is a bad
       parameter.
    2. `source.ReadExclusions`. Its error is a finding, as in the CLI.
    3. `app.CheckExclusions(exc, err, app.Assignees(issues), util.LocalDay(rc.now))`.
  - **Text:** `report.Check(w, len(issues), r.InputPath, app.CheckReadiness(issues))`,
    then `report.Exclusions(w, path, rep)` when set.

    Use `r.InputPath` exactly as the user typed it, not the absolute path,
    so the text matches the CLI.
  - **JSON:** the DTO.

### 5.8 DTOs (`dto.go`)

The rules for every DTO:
- Dates are `"YYYY-MM-DD"` strings.
- Field names are snake_case.
- `NaN` becomes `null`.
- No JSON tags are added to `app` or analysis types; DTOs are separate
  structs.

```
run (shared by sim results):
  {"mode_label", "seed", "sample_start", "sample_end", "pool_issues": len(Run.PoolIssues)}

histogram: [{"value": v, "count": n}]   // from a sorted distribution, one entry per distinct value, ascending

sim-items:       {"run", "target_start", "target_end", "effective_days", "confidences",
                  "rows": [{"confidence", "items"}], "histogram"}
sim-days:        {"run", "target_start", "confidences", "items": [..],
                  "single": [{"confidence", "days", "date"}] | null, "histogram": [...] | null,
                  "groups": [{"items", "cells": [{"confidence", "marginal_days", "cumulative_days", "date"}]}] | null,
                  "total": {"items", "cells": [...]} | null}
sim-probability: {"run", "target_start", "target_end", "effective_days", "items",
                  "probability": p | null, "table": [{"items", "probability"}] | null, "histogram"}
sim-backtest:    {"run", "scope", "issue_count", "replay_start", "replay_start_inferred", "today",
                  "rows": [{"date", "completed", "remaining", "probability", "projected"}]}
aging:           {"report": <aging.RenderJSON output as raw JSON>}            // Phase 8 extends it
cfd:             {"report": <cfd.RenderJSON output as raw JSON>, "start", "end"}
history:         {"report": <history.RenderJSON output as raw JSON>, "start", "start_inferred"}
count:           {"total", "show_teams", "projects": [{"name", "team", "total", "milestone_count",
                  "last_updated", "milestones": [{"name", "count"}]}]}
check:           {"input", "issues", "readiness": [{"command", "excluded", "status"}],
                  "exclusions": null | {"path", "invalid": "msg" | null, "global_entries",
                  "engineers": [{"name", "entries"}], "has_dates", "first", "last", "past", "future",
                  "today", "unmatched_names": [...]}}
```

For `count`, read `counts.Project` and `counts.Milestone` for the source
fields (`TeamName` → `team`, `Total` → `total`, `MilestoneCount()` →
`milestone_count`). For the embedded reports, render into a
`bytes.Buffer` and store it as `json.RawMessage`.

### 5.9 `GET /api/commands`

For each registered command:
1. Build a fresh `FlagSet` with `newFlags(fs, s.opts.Now())`.
2. `fs.VisitAll` gives every flag's name, usage, default (`DefValue`) and
   whether it is a bool flag (the value implements
   `interface{ IsBoolFlag() bool }` returning true).
3. Merge in the meta (overrides win), and drop hidden flags.

```json
{"commands": [{"id", "name", "summary", "group", "title", "exports", "stream",
  "fields": [{"name", "label", "kind", "section", "usage", "default", "bool", "choices"}]}]}
```

`fields` is in `VisitAll` order (sorted). The frontend orders by section.

### 5.10 Tests

All in `internal/web` unless noted.

1. **`TestMeta_CoversEveryFlag`:**
   - For every registered command, every flag in its `FlagSet` must resolve
     to a `fieldMeta`, and that meta must be either hidden or have a
     non-empty `Kind`, `Section` and `Label`.
   - Every key in `metaOverrides` must name an existing flag of that
     command.
   - A new CLI flag therefore fails this test until someone decides how it
     appears in the UI.
2. **`TestCommands_Schema`:** `/api/commands` lists the registry IDs in
   order. `sim-items` contains `engineers`, `days` and `confidence`, and
   contains none of `db`, `config`, `stdin-format` or `manifest`.
3. **`TestRun_ParamErrors`:**
   - an unknown param;
   - a hidden param;
   - a bad int (`days=abc`);
   - a bad date (`sample-start=yesterdayish`);
   - `days` + `target-end-date` together;
   - `input=-`.

   Each returns 400 with the exact message and `fields`.
4. **`TestRun_StreamShape`:** `sim-items` with seed 42, 2000 simulations and
   4 goroutines:
   - the stream ends with exactly one `result`;
   - progress events are monotonic, and the last progress has
     `done == total == 2000`;
   - `run_id` is exportable;
   - `cli` starts with `forecast sim items`.
5. **`TestRun_ExportMatchesText`:** for every export, the body is
   byte-identical across two GETs, and the `text` export equals the result
   event's `text`.
6. **`TestRun_Concurrent`** (run with `go test -race ./internal/web/...`):
   - Use one server and one cached dataset.
   - Start 3 goroutines per command (every registered command except linear
     ones), all at once, then wait.
   - All succeed. The race detector must report nothing. If it reports a
     write to a cached `[]issues.Issue`, clone the slice in the web layer
     (`slices.Clone`) before handing it to `app`, and report it. Do not
     change `internal/app` here.
7. **`TestStore_Evicts`:** the 51st run evicts the first.
7a. **`TestInlineHTML_UsesVendoredPlotly`:**
    - Run `cfd` on the sample data.
    - The `?inline=1` html export contains `plotlyPath` and does not
      contain `cdn.plot.ly`.
    - The plain download *does* contain
      `https://cdn.plot.ly/plotly-<plotlyVersion>.min.js`.
    - This also fails if `cfd/template.html` and the vendored version ever
      drift apart, because the rewrite would then find nothing to
      replace.
8. **`cmd/forecast/web_parity_test.go`** (package `main`, so it can reach
   `goldenCases()`). This is the parity contract.
   - Pin `time.Local` and `nowFunc` exactly as `TestGolden` does. Extract
     the four setup lines in `TestGolden` into a helper, `pinGoldenClock(t)`,
     and call it from both tests. The behaviour of `TestGolden` must not
     change.
   - Map each `goldenCase.run` to a web command ID by function pointer:
     `reflect.ValueOf(c.run).Pointer()`, keyed against
     `reflect.ValueOf(cmdSimItems).Pointer()` and so on.
   - Convert args to params:
     1. `web.ParamsFromArgs(id, args, now)` builds the command's `FlagSet`,
        calls `fs.Parse(args)`, and returns `{name: f.Value.String()}` for
        every flag `fs.Visit` reports as set.
     2. Remove `format` from the returned map. It names the export to
        compare; when absent, use the command's first export.

     `ParamsFromArgs` is exported from `internal/web`; Phase 9 reuses it.
   - POST `/api/run/{id}` against `httptest.NewServer` (with
     `Now: nowFunc`). The test's working directory is `cmd/forecast`, as in
     `TestGolden`, so the golden relative paths resolve.
   - Split the golden file into stdout, stderr and error sections. Then:
     - **Error:** when golden error ≠ `<nil>`, the 400 body's or error
       event's message must equal it exactly, and golden stdout must be
       empty.
     - **Output:** when golden error is `<nil>`, the export body must equal
       golden stdout exactly.
     - **Warnings:** the response's `warnings` must equal the golden
       stderr lines that start with `WARN  `, with that 6-character prefix
       removed, in order. Ignore `INFO` lines; they come from
       `ApplyConfig`'s effective-flags table, which the web server doesn't
       produce.
   - Every golden case must map to a command. Fail on any unmapped case.

**Acceptance:**
- All of the above pass, including `go test -race ./internal/web/...`.
- The parity test covers every golden case.
- `TestGolden` is unchanged.

---

## Phase 6: frontend shell with generic result views (full parity)

When this phase is done, every command is usable from the browser with
CLI-identical output. Phases 7–8 make it nicer.

### 6.1 Files

```
static/index.html
static/css/app.css
static/js/main.js       boot: fetch /api/commands, /api/version, /api/defaults; build nav; start router
static/js/api.js        apiGet, apiPost, upload, runCommand
static/js/router.js     parse/serialize #/<id>?<params>
static/js/databar.js    global data source picker + dataset summary chip
static/js/form.js       schema-driven form rendering, reading, error display
static/js/result.js     generic result view (tabs, warnings, downloads, CLI/config, share)
static/js/views/index.js  registry: id → render(container, result, ctx); empty in this phase
static/js/util.js       el() DOM helper, escapeHTML, copyToClipboard, formatDate
```

Rules:
- Every script is an ES module loaded from `index.html` via
  `<script type="module" src="/js/main.js">`. Imports use relative paths
  ending in `.js`.
- No inline scripts and no inline event handlers (the CSP forbids them).
- No URL outside the server's own origin anywhere in `static/`, apart from
  `static/vendor/` (D14). `TestStatic_ImportsResolve` enforces this.
- Build DOM with `document.createElement` through `el()`. Never assign
  `innerHTML` from data; data can contain HTML characters (issue titles).

### 6.2 Layout and style (`index.html`, `app.css`)

- **Header:** the app name "Forecast" and the data bar (§6.4).
- **Left nav:** groups "Forecast", "Flow", "Data". Each entry shows the
  command title, with the CLI name as a muted subtitle.
- **Main:** the page title, the summary, the form, then the result.
- **Footer:** the version, plus "Update with `forecast update`".
- **Colour tokens** on `:root`, redefined for dark mode under
  `@media (prefers-color-scheme: dark)`. Give `body` an explicit
  background.
- **Width:** at ≤ 760px the nav collapses into a `<select>` above the main
  area. No horizontal page scroll; wide tables scroll inside their own
  container (`overflow-x: auto`).
- **Classes:**
  - `.invalid` (red outline) on inputs and `.field-error` text under them;
  - `.warnings` box (amber);
  - `.error-banner` (red);
  - `progress` bar styling;
  - `.high`, `.medium` and `.normal` text colours, matching
    `aging`'s HTML report (`#b91c1c`, `#c2410c`, inherit) and adjusted for
    dark mode.

### 6.3 `api.js`

```js
export async function apiGet(path)            // fetch → JSON; non-2xx → throw ApiError{status, message, fields, warnings}
export async function apiPost(path, body)     // adds headers {"Content-Type": "application/json", "X-Forecast-Client": "web"}
export async function upload(file)            // multipart POST /api/uploads with X-Forecast-Client → {path, name, size}
// runCommand POSTs /api/run/{id} and reads the NDJSON stream. Resolves with the
// terminal event ({type: "result", ...} or {type: "error", ...}); a 400 resolves
// as {type: "error", error, warnings}. Calls onEvent for queued/progress/log.
export async function runCommand(id, params, onEvent)
```

To read NDJSON, use `response.body.getReader()` with a `TextDecoder`.
Buffer the partial line, then split on `\n` and `JSON.parse` each line.

### 6.4 Data bar (`databar.js`)

- **The input field:** a text input for `input`.
  - It has a `<datalist>` filled from `/api/files?kind=input` (files and
    uploads).
  - An "Upload…" button opens a hidden `<input type=file>`, calls
    `upload()`, and sets the field to the returned path.
- **Initial value:** the URL's `input` param, else `localStorage`
  (`forecast.input`, wrapped in try/catch), else `/api/defaults.input`.
- **On change** (debounced 300 ms):
  1. Fetch `/api/datasets/summary`.
  2. Show a chip: `<N> issues · <teams> · created <earliest>–updated <latest>`.
  3. Clicking the chip expands the readiness lines.
  4. Errors show inline in red.
- **Exposes** `getInput()`, `getSummary()` and an `onChange(callback)`
  subscription. The summary feeds the form's pickers.
- Forms never render an `input-path` field. `form.js` adds
  `params.input = getInput()` when reading the form, for every command
  whose schema contains `input`.

### 6.5 Router (`router.js`)

- **Route syntax:** `#/<id>?<query>`. An empty hash redirects to
  `#/check`.
- **Serialization:** `URLSearchParams`, with keys sorted and empty values
  dropped.
- **`navigate(id, params, {replace})`** sets `location.hash`.
- **On `hashchange`:**
  1. Render the command page: title, summary, form populated from the
     params.
  2. If the params contain `run=1`, submit immediately. `run` is a UI-only
     key that is never sent to the API.

### 6.6 Forms (`form.js`)

```js
export function renderForm(container, command, params, summary)  // returns {form, read(), showErrors(error), clearErrors()}
```

- **Sections:** fields are grouped by section, in the order "data" (minus
  `input-path`), "basic", then "advanced". "Advanced" sits inside a
  collapsed `<details>`.
- **Each field has:**
  - a `<label for>` with the label text;
  - the widget;
  - the usage text in a `<small>`;
  - the default as the input's `placeholder`, written `default: <value>`
    when the default is non-empty.

  A value shows only when the URL or the user set it. Unset fields stay
  empty, so the CLI default applies, as in D3.
- **Widgets by kind:**

| Kind | Widget |
| --- | --- |
| `bool` | checkbox. `read()` emits `"true"` when checked and the default is `"false"`, `"false"` when unchecked and the default is `"true"`, and nothing otherwise |
| `int` | `<input type=text inputmode=numeric>`. Leave validation to the server, so error messages stay the CLI's |
| `int-list`, `string`, `engineers`, `duration` | text input |
| `date-start` | text input with a `<datalist>` of `today`, `tomorrow`, `yesterday`, `-3 months`, `-6 months`, plus a sibling `<input type=date>` that writes `YYYY-MM-DD` into the text input on change |
| `date-end` | as `date-start`, with `now` added to the datalist |
| `team-list` | text input. Below it, one toggle chip per team in the summary; clicking adds or removes the key in the comma list |
| `assignee-list` | as `team-list`, using the summary's assignees |
| `project` | text input with a `<datalist>` of the summary's project names |
| `milestone` | text input with a `<datalist>` of the selected project's milestones; it updates when `project` changes |
| `exclusions-path`, `db-path` | text input with a `<datalist>` from `/api/files?kind=exclusions\|db` and an "Upload…" button. Pre-fill `exclusions` from `/api/defaults` only when the URL doesn't set it |
| `choice` | `<select>` with an empty "(default)" option |

- **Errors:** `showErrors({message, fields})` adds `.invalid` and
  `aria-invalid="true"` to each named field, and puts the message under the
  first named field. With no fields, or a field that isn't rendered (for
  example `input`), it shows an `.error-banner` above the form instead. An
  `input` error highlights the data bar's field.
- **Buttons:** "Run" (primary) and "Reset" (clears the params from the URL).

### 6.7 Result view (`result.js`)

On submit:

1. `clearErrors()`.
2. `navigate(id, params, {replace: true})`, without `run=1`.
3. Disable "Run".
4. Show a progress area:
   - `queued` → "Waiting for another run to finish…";
   - `progress` → `<progress max=total value=done>` plus "done / total";
   - `log` → append to a `<pre class="log">`.
5. On the terminal event:
   - **`error`:** `showErrors(error)` and render the warnings above the
     form.
   - **`result`:** render the result in this order:
     - **The warnings box,** if any. Each warning is one `<li>`.
     - **Tabs:**
       - **"Result":** `views[id](container, result)` if a view is
         registered, else the same content as "Output".
       - **"Output":** `<pre>` of `result.text`. If `text` is empty and the
         command has an `html` export, show the "Report" content instead.
       - **"Report"** (only when `exports` includes `html`):
         `<iframe src="/api/runs/<run_id>/export/html?inline=1">` (no
         `sandbox`; see §5.6),
         full width, 80vh tall.
       - **"CLI":** `result.cli` and `result.config` in `<pre>` blocks,
         each with a "Copy" button.
     - **Download links:** one `<a download href="/api/runs/<run_id>/export/<format>">`
       per export, labelled e.g. "Download text", "Download CSV",
       "Download manifest".
     - **"Share link":** copies `location.href` with `run=1` added. For
       commands in group `forecast`, it also adds `random-seed=<result.result.run.seed>`
       unless the params already contain `random-seed`.

### 6.8 Static asset test

`TestStatic_ImportsResolve` in `internal/web`:
- Walk the embedded `static/` tree.
- For every `.js` file, match `from\s+["'](\.[^"']+)["']` and
  `import\(["'](\.[^"']+)["']\)`. For `index.html`, match `src="/..."` and
  `href="/..."`.
- Every referenced path must exist in the embedded FS.
- No `.js` or `.html` file may contain `innerHTML =` or `onclick=`.
- No file outside `static/vendor/` may contain `http://` or `https://`.
  Everything loads from the server itself (D14).

### 6.9 Manual checklist

Run `bin/forecast serve -input testdata/sample-issues.csv -exclusions cmd/forecast/testdata/golden/exclusions.json -open`.

1. The data bar shows the sample's issue count and teams.
2. For each command, run it with params from a golden case. "Output"
   matches the corresponding `cmd/forecast/testdata/golden/*.golden`
   stdout.
3. `sim items` with `days` and `target-end-date` both set highlights both
   fields with the CLI's message.
4. A 10,000-simulation `sim items` run shows a moving progress bar.
5. The `cfd` "Report" tab shows the Plotly chart, and the `aging` "Report"
   tab shows the HTML table.
6. Every download link downloads a file with the expected content.
7. "Share link" opened in a new tab reproduces the same numbers.
8. Dark mode (switch the OS appearance) is legible.
9. A 375px-wide window has no horizontal scroll.
10. The browser console shows no errors.
11. **Offline:** turn off Wi-Fi or networking and reload. The page, the
    `cfd` "Report" chart and every download still work. The network panel
    shows no request leaving `localhost`.

---

## Phase 7: native views for the sim commands

Each view is a module in `static/js/views/` and is registered in
`views/index.js`. A view renders into the "Result" tab using
`result.result` (the DTO) only. Charts go through one helper module:

`static/js/charts.js`:
- **`loadPlotly()`** injects `<script src="/vendor/plotly-2.35.2.min.js">`
  once, the embedded copy (D14), and returns a promise resolving to
  `window.Plotly`. Keep the path in one `const` at the top of the module.
  If the script fails to load, the promise resolves to `null`, which should
  never happen with the embedded copy.
- **`histogram(el, histogram, markers)`** draws a bar chart of the
  `histogram` entries, plus a vertical dashed line and label per marker
  (`{value, label}`).
- **`lineChart(el, series, opts)`** draws a generic multi-series line
  chart.
- **Theme:** charts read the CSS colour tokens via
  `getComputedStyle(document.documentElement)`, so they follow dark mode.
- **Failure:** when `loadPlotly()` resolves to `null`, every chart helper
  renders a muted "Chart unavailable" note. Tables still render.

Views:

- **`sim-items.js`:**
  - Header: `<mode_label>, <target_start> to <target_end> (<effective_days> days)`.
  - Table: Confidence | Items. Cells read "at least N".
  - Histogram titled "Items completed across simulations", with a marker
    per confidence row at its `items` value (`label: "85%"`).
- **`sim-days.js`:**
  - **Single mode:**
    - Table: Confidence | Days | Date. Dates are formatted
      `YYYY-MM-DD (Mon)`; use the weekday from the date string, computed in
      UTC so the timezone can't shift it.
    - Histogram of days, with a marker per confidence row.
  - **Trajectory mode:**
    - The same two-row header as the CLI: confidence levels spanning
      Days/Date pairs.
    - One row per group, then a bold Total row.
    - A line chart with one series per confidence: x = cumulative date,
      y = cumulative items.
- **`sim-probability.js`:**
  - **When `probability` is set:** show it large (`72.4%`) with the
    sentence "You can commit to N items here at ~72% confidence."
  - **When `table` is set:** a table Items | Probability, and a bar chart of
    probability by item count with a horizontal dashed line at 85.
- **`sim-backtest.js`:**
  - Header: `Backtest: <scope> (<issue_count> issues, <mode_label>)` and
    the replay start (append "(inferred)" when `replay_start_inferred`).
  - Line chart: probability over date. The projected rows are a dashed
    segment, and a vertical line marks `today`.
  - A second line chart shows remaining vs completed.
  - Table: Date | Completed | Remaining | Probability. Projected rows are
    muted and italic, with a divider row "today: <date> — rows below are
    projected".

**Tests:** extend `TestStatic_ImportsResolve` (automatic).
**Manual checklist:** each sim command's view agrees number-for-number with
its "Output" tab, for a single-mode and a trajectory `sim days`, and for
both probability modes. Charts follow dark mode.

---

## Phase 8: native views for aging, cfd, history, count, check

### 8.1 Export the aging formatting helpers

In `aging/aging.go`, rename these and update their callers:
- `ageClass` → `AgeClass`;
- `formatMultiplier` → `FormatMultiplier`;
- `formatState` → `FormatState`.

Give each a doc comment; keep the existing ones. The output doesn't change,
and `aging`'s tests and the goldens must pass unchanged.

Extend the `aging` DTO:

```
aging: {"report": <unchanged>,
        "meta": {"label", "percentile", "threshold", "has_threshold", "completed_count", "sample_start", "sample_end"},
        "in_progress": [item], "completed": [item] | null,
        "cycle_times": [...]}
item:  {"identifier", "title", "assignee", "project_name", "state": FormatState(...),
        "started_at": "YYYY-MM-DD", "age_days", "percentile", "percentile_label": "85th",
        "multiplier": x | null (null when !has_threshold), "multiplier_label": FormatMultiplier(...),
        "age_class": AgeClass(...)}
```

`percentile_label` uses `util.OrdinalSuffix`. `completed` is non-null only
when `show-completed` was set.

### 8.2 Views

- **`aging.js`:**
  - Meta line, the same sentence as the text header:
    `Cycle time distribution: N completed issues (start to end) · P85: 8.0 days`.
  - **In-progress table:** Identifier, Title, Days, Percentile, Multiplier,
    State, Start date, Assignee.
    - Days, Percentile and Multiplier cells carry the item's `age_class`.
    - Click a header to sort; the default is Days descending.
    - Show the full title, with CSS ellipsis and a `title` attribute.
  - **A second table** for `completed`, when present.
  - **Histogram of `cycle_times`**, bucketed to whole days in JS, with a
    marker at `meta.threshold` labelled with `meta.label`.
- **`cfd.js`:** the "Result" tab shows the same inline iframe as
  "Report". The CLI's HTML report already is the rich view; don't
  re-implement it. Below it, show a compact table of the health numbers from
  `report.health`. Pick the keys from what `cfd.RenderJSON` emits.
- **`history.js`:**
  - Header: total issues, skipped, rolling window, start (append
    "(inferred)" when `start_inferred`).
  - Three line charts from `report.series`:
    - "Scope": total, completed, remaining;
    - "Throughput": 7-day and window throughput;
    - "Cycle time": cycle p50 and p85.
  - A scrollable table of every series column, with a sticky header and
    sticky first column. `null` renders as "–".
- **`count.js`:**
  - Without `milestones`: a table Project | (Team, only when `show_teams`)
    | Issues | Milestones | Last updated.
  - With `milestones`: each project row expands, using `<details>`, into
    milestone rows.
  - A total row in both cases.
- **`check.js`:**
  - One card per readiness line, green when `excluded == 0` and amber
    otherwise.
  - An exclusions card when present: invalid (red) or the entries, span and
    unmatched names.
  - Each readiness card links to that command's page (`history` → `#/history`,
    and `sim` → `#/sim-days`).

### 8.3 Tests

- `aging` package tests stay green, and `TestGolden` and the web parity
  test are unchanged.
- **DTO test:** add a `TestDTO_Aging` that runs aging with
  `show-completed`. Assert `age_class` ∈ {high, medium, normal}, and that
  `multiplier_label` equals what `aging.FormatMultiplier` returns.

**Manual checklist:**
- Each view agrees with its "Output" tab.
- Sorting works.
- The history charts render for the sample data.
- The check cards link correctly.

---

## Phase 9: Linear sync and teams, config import/export, paste a CLI command

### 9.1 `syncer` logging hook

Add to `syncer.Options`:

```go
// Logf, when non-nil, receives the progress lines Run would otherwise log
// through logx.Infof (same format strings and arguments). The CLI leaves it
// nil; the web server streams the lines to the browser.
Logf func(format string, args ...any)
```

Inside `Run`, replace each `logx.Infof(...)` with a local
`logf := opts.Logf; if logf == nil { logf = logx.Infof }` call. Add a
`syncer` test using the existing client stub: with `Logf` set, the
expected lines are captured.

### 9.2 Register `linear-sync` and `linear-teams`

- **`linear-teams`:**
  - prepare: `linear.GetAPIKey()`. An error becomes **400** with the same
    message.
  - run: `client.ListTeams(ctx)`.
  - DTO: `{"teams": [{"key", "name"}]}`.
  - Exports: `json`. `Text` is the CLI's exact text. Move the two print
    lines of `cmdLinearTeams` into `report.LinearTeams(w, teams)` and have
    the CLI call it too.
- **`linear-sync`:**
  - **prepare:**
    1. `Resolve`.
    2. `resolvePath("db", path)`.
    3. `linear.GetAPIKey()`.
    4. Open the store with `sqlite.Open`, because sync may create the db,
       like the CLI.
  - **run:** `syncer.Run(ctx, client, store, opts)` with
    `opts.Logf = func(f string, a ...any) { ev.Log(fmt.Sprintf(f, a...)) }`.
  - **Context:** use a context that is **not** the request context, so a
    closed tab doesn't abort a half-finished team. The syncer already
    commits per team.
  - **After the run:** close the store, then
    `s.datasets.invalidate(absDB)`.
  - DTO: `{"db": "<path as typed>"}`. No exports. `Text` is the joined log
    lines.
  - The UI shows the log stream live (Phase 6 already renders `log`
    events).
- **`GET /api/linear/teams`:** the same as running `linear-teams`, but
  returns the DTO as plain JSON. The sync form's `teams` field uses it to
  show team chips. If the API key is missing, the chips area shows the 400
  message.

Extend `TestMeta_CoversEveryFlag` and the parity test's "pending" list:
nothing is pending any more. The tests can't hit the Linear API, so test
`linear-sync` prepare's error path only: with `LINEAR_API_KEY` unset (use
`t.Setenv("LINEAR_API_KEY", "")`), it returns 400 with
`LINEAR_API_KEY environment variable is not set`.

### 9.3 Config import/export

- **`util.ParseConfig`.** Refactor `internal/util/config.go`:
  `loadConfig` + `stringify` gain an exported
  `ParseConfig(data []byte) (map[string]string, error)`. It returns each
  key's stringified value. `loadConfig` and `applyConfig` keep their
  behaviour; add a test.
- **`POST /api/config/parse`.**
  - Request body: `{"command": "sim-items", "yaml": "..."}`.
  - It drops the `config` key (reserved, as in `ApplyConfig`).
  - It checks every other key against the command's `FlagSet`:
    - unknown keys go to `unknown`;
    - hidden keys go to `ignored`, except `input`, which is kept.
  - Response: `{"params": {...}, "unknown": [...], "ignored": [...]}`.
- **Form header buttons:**
  - **"Import config…":** a file input → `/api/config/parse`. The form is
    filled with the params, `input` goes to the data bar, and `unknown` and
    `ignored` keys are listed in a dismissible note.
  - **"Export config":** downloads a `<command-id>.yaml` built from the
    *current form*. Call `POST /api/config/render` with
    `{"command", "params"}`, which applies the params to a fresh `FlagSet`
    exactly as `/api/run` does (same 400 errors) and returns
    `{"yaml": params.ConfigYAML(fs), "cli": params.ShellJoin(params.CommandLine(...))}`.
  - **"Copy CLI command":** uses the same endpoint's `cli`.

### 9.4 Paste a CLI command

- **`POST /api/cli/parse`.**
  - Request body: `{"line": "forecast sim items -input x.csv -engineers 3 -days 30"}`.
  - **Splitting:** split the line with a small POSIX-ish splitter, in a new
    `params.SplitCommandLine(line string) ([]string, error)` handling
    single quotes, double quotes and backslash escapes. Unterminated quotes
    are an error.
  - **Matching:** strip a leading `forecast` (or any path ending in
    `/forecast`), then match the longest command name. Unknown command →
    400 `unknown command`.
  - **Parsing:** call `ParamsFromArgs`. Hidden flags are reported in
    `ignored` and removed; `input` is kept.
  - Response: `{"command": "sim-items", "params": {...}, "ignored": [...]}`.
- **UI:** a "Paste CLI command…" button in the header opens a small dialog
  with a textarea. On submit, it navigates to the parsed command with its
  params, without running.

**Tests:**
- `SplitCommandLine` table test;
- `/api/cli/parse` happy path and unknown command;
- `/api/config/parse` with an unknown key;
- `/api/config/render` YAML round trip.

**Manual checklist:**
- With `LINEAR_API_KEY` set, sync a team into a scratch db. The log streams,
  and the data bar's summary for that db refreshes.
- Export, then import, a config for `sim days` and check the form is
  unchanged.
- Paste a golden case's command line and get the same form.

---

## Phase 10: documentation and audit

### 10.1 Audits (each must print nothing)

```bash
grep -rn 'os\.Std\|os\.Args\|time\.Now\|logx' internal/report internal/params
grep -rn 'innerHTML\s*=' internal/web/static
grep -rn '"os"\|"flag"\|time\.Now\|logx' internal/app --include='*.go' | grep -v _test.go
go test -race ./internal/web/... ./internal/params/...
```

The `internal/app` audit is the same purity rule as before. `summary.go` and
the `ManifestExtra` methods must keep it clean.

### 10.2 Docs

- **README.md:** a "Web UI" section covering:
  - `forecast serve`, its flags, and that it's local-only by default;
  - uploads vs paths;
  - downloads, share links, config import/export and paste-a-command;
  - that `forecast update` updates the UI too.
- **CLAUDE.md:**
  - Add `internal/flagvals`, `internal/report`, `internal/params` and
    `internal/web` paragraphs to Architecture, written like the existing
    `internal/app`/`internal/source` paragraphs.
  - Update the diagram so `internal/params` sits between both adapters
    and `internal/app`.
  - Add a `forecast serve` bullet to Commands.
  - Add the parity tests to "Conventions worth knowing" (§5.10 1 and 8).
    State the rule: **a new flag needs a `meta.go` entry; a new command
    needs a registry entry or a D12 justification.**
- **Status block at the top of this file:** mark it done and list any
  deviations, as `APP_LAYER_PLAN.md` does.

---

## 7. What "done" looks like

- `forecast serve` starts a local UI from the same binary.
- Every CLI command except `version`/`update`/`serve` has a page:
  - every non-hidden flag is a form field;
  - every hidden flag has a stated web equivalent (D12);
  - every CLI output format is a download.
- **The output matches the CLI.** Every golden case, replayed over HTTP,
  gives byte-identical output, identical warnings and identical errors.
  This is checked by `web_parity_test.go`.
- **New flags can't be forgotten.** Adding a flag to the CLI without a UI
  decision fails a test.
- **Determinism is unchanged.** `TestGolden` is unchanged from the start
  of this plan.

---

## 8. Out of scope (don't do these here)

- Authentication, TLS, multi-user deployments. `-allow-remote` is a
  trusted-network escape hatch, not a hosting story.
- Cancelling a running simulation. That needs `context.Context` plumbed
  through `internal/app` and `simulate`; it gets its own plan.
- Changing the CLI's standalone `cfd` HTML report to inline or bundle
  Plotly. It keeps loading from the CDN (D14).
- Merging pages, such as items + probability on one page or backtest +
  history on one page. The `APP_LAYER_PLAN.md` §9 idea of question-first
  pages can build on this later.
- Persisting runs across server restarts, and cleaning up the upload
  directory.
- Playwright/headless-browser end-to-end tests. They'd add a Node
  toolchain; the manual checklists stand in for now.
- `forecast update` from the browser.
- Any change to analysis behaviour, defaults or messages.
