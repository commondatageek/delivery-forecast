# Implementation plan: mapped CSV import (`forecast import`)

Status: **implemented** (Phases A–D, 2026-10-26; the §10 acceptance check against the real export is still to run). Written 2026-10-25
from a profile of a real Jira Cloud export ("SDLC Workload Export
(Jira).csv", 983 issues, one project); revised the same day with the user's
feedback (keep `-input` for normalized files, §3 D-I12; sort the colleague's
questions by urgency, §11). Nothing in §11 blocks any phase.

Companion to [`CONNECTORS_PLAN.md`](../../CONNECTORS_PLAN.md). This plan is
the near-term path for teams that hand us a tracker's CSV export; that plan's
Jira API connector (its Phase 3) is the long-term path and the only one that
recovers `started_at` for finished work (§8.3).

---

## 0. How to use this document

This plan is written to be executed by an agent that did not take part in
the design conversation. Everything needed is in here or in the files it
names; when something is genuinely ambiguous, **stop and ask the user**
rather than guessing.

### 0.1 Rules

1. **Prerequisite: `CONNECTORS_PLAN.md` Phase 1 (all five steps) is done
   first.** This plan targets the v2 record (`issues.Status`, `status`,
   `status_name`, `milestone_*`, `issue_type`, `parent_identifier`,
   `resolution`, `source`, `url`) and the v2 SQLite schema keyed by
   `(source, identifier)`. Before starting Phase A, confirm with
   `jj log` and by reading `issues/issue.go` that `Issue` has a `Source`
   field and a `Status issues.Status` field; if not, do that plan's Phase 1
   first (or ask the user). Note while doing it: its step 3 says
   "`cmd/forecast` converters"; since the app-layer refactor those live in
   `internal/app/convert.go`.
2. **Work the phases in order; one commit per numbered step** with the given
   subject line. Each commit compiles (`just build`) and passes `just test`
   on its own. A commit holds one change — code, its tests, and the doc lines
   that change only because of it. End every commit message with the
   attribution trailer the session provides.
3. **Version control is `jj`** (colocated with git). Use the `jj` skill.
   Typical step: make the change, run tests, then
   `jj describe -m "<subject>\n\n<body>\n\nCo-Authored-By: …"` and `jj new`.
   Work on top of the `connectors-plan` bookmark (or whatever branch the
   user names); move the bookmark forward with
   `jj bookmark set <name> -r @-` after each commit. Never push unless asked.
4. **Write the tests each step lists.** They are part of the step.
5. **The real export must never be committed**, nor anything derived from it
   (row excerpts, names, summaries). The only Jira-shaped fixture is the
   synthetic one in §6, typed in by hand. Do not open, copy or read rows from
   `SDLC Workload Export (Jira).csv` for tests; it may only be used in the
   final acceptance check (§10), locally, and nothing it prints goes into a
   file in the repo.
6. **`.gitignore` ignores `*.csv`, `*.yaml`, `*.json`, `*.db` globally.**
   New fixtures and templates need `!` negations placed *after* those lines
   (Phase A step 1). After creating any such file, run `jj st` and confirm
   it is listed as added; if it isn't, the negation is wrong.
7. **Fail loudly, never silently guess** (D-I4). Every rule in a mapping
   either matches the data or the import stops with an error that names the
   rule, the column and example values.
8. **Existing analysis output must not change.** The golden files under
   `cmd/forecast/testdata/golden/` change only by *added* files. If an
   existing golden file changes, stop: something outside this plan's scope
   moved. Regenerate goldens only with
   `go test ./cmd/forecast -run TestGolden -update`, then review the diff.

### 0.2 Repo conventions this plan relies on

Read `CLAUDE.md` first. The parts that matter here:

- **Layering.** Pure packages (no `os`, no clock, no logging) hold logic;
  `cmd/forecast` and `internal/web` are adapters that do IO. `internal/app`
  must not import `source`, `sqlite`, `linear`, `logx`. The new
  `internal/importmap` follows the same rule (D-I8).
- **Commands.** A CLI command is a `command{Name, Summary, Run}` entry in
  `cmd/forecast/main.go`'s `topCommands`, a `cmdX(args []string) error`
  function in `cmd/forecast/x.go`, and a flag bundle in `internal/params`
  (`NewX(fs) *X` registers flags; `(*X).Resolve() (Resolved[XRequest], error)`
  validates them). Model the new command on `cmd/forecast/check.go` +
  `internal/params/check.go`; the pattern is: `flag.NewFlagSet(…,
  flag.ExitOnError)`, `NewX`, `fs.Parse(args)`, `util.ApplyConfig(fs,
  *x.Config)`, `Resolve`, `printWarnings(r.Warnings)`, then work.
- **Errors that blame flags** are `*app.FieldError{Fields: []string{"flag"},
  Msg: "…"}`; `Fields` holds flag names without the dash.
- **Text renderers** live in `internal/report` and write to an `io.Writer`
  so the CLI and web produce identical bytes.
- **Web.** A command is exposed by an entry in `internal/web/commands.go`'s
  `registry` (its `Summary` must equal the CLI's — `TestWebSummariesMatchCLI`),
  a `newXFlags`/`prepareX` pair (model: `internal/web/cmd_data.go`'s
  `prepareCheck`, and `cmd_linear.go`'s `prepareLinearSync` for a command
  that writes a file), and a `meta.go` decision for every flag
  (`TestMeta_CoversEveryFlag`). `cmd/forecast/web_parity_test.go` replays
  **every** golden case through the HTTP API, so every golden case's `run`
  function needs an entry in `webIDs()`.
- **Golden tests.** `cmd/forecast/golden_test.go`'s `goldenCases()`; args
  use shorthands (`@S` = `../../testdata/sample-issues.csv`). Add `@J` for
  the Jira fixture (Phase C step 2).

---

## 1. Goal

A colleague whose team lives in Jira can, with no column renaming, row
deleting or spreadsheet editing:

```bash
forecast import -example jira-cloud-csv > bsd-jira.yaml     # once; edit to taste
forecast import -from "SDLC Workload Export (Jira).csv" -map bsd-jira.yaml -out bsd.db
forecast sim items -input bsd.db -whole-team -days 30
forecast serve -input bsd.db
```

…and repeat only the second line (with a fresh export) to refresh. The
mapping file is the durable, reviewable record of how her team's Jira usage
maps onto the forecasting model; when policy changes she edits the file, not
the export and not the code.

### Non-goals

- A `-map` flag on the analysis commands (D-I1).
- Excel (`.xlsx`) input; stdin input to `import`.
- Reconstructing `started_at` for finished work from the export (§8.3).
- An expression language. Every rule is one of the fixed shapes in §4.
- Importing comments, attachments, watchers, links, work logs.
- Editing a mapping in the browser (the web page uploads one; §9).

---

## 2. What the export looks like (profile, 2026-10-22 export)

Aggregate facts only; they drive §4's rules and §6's fixture.

**Shape.**
- UTF-8 **with a BOM** (`EF BB BF`). Today's `issues.ReadCSV` does not strip
  it, so the first header reads as `"﻿Summary"`.
- 670 columns, 983 rows, every row full width. Fields contain commas, quotes
  and newlines (standard RFC 4180 quoting; `encoding/csv` handles it).
- **Repeated headers**: `Sprint` ×16, `Fix versions` ×2, `Labels` ×4,
  `Watchers` ×13, `Comment` ×147, `Attachment` ×37, issue links per type.
  Today's reader silently keeps the *last* occurrence.
- Timestamps look like `22/Oct/26 7:55 AM` — Go layout `02/Jan/06 3:04 PM`:
  two-digit year, no seconds, **no timezone**. The layout and zone follow the
  exporting user's Jira profile settings.

**Columns that matter.**

| Export column | Fill | Use |
|---|---|---|
| `Issue key` | 100% | `identifier` |
| `Summary` | 100% | `title` |
| `Issue Type` | 100% | Story, Task, Bug, Epic (55), Sub-task (17) |
| `Status` | 100% | 17 distinct workflow statuses |
| `Status category` | 100% | `To Do` / `In Progress` / `Done` — the stable signal |
| `Status category changed` | 100% | when the issue entered its *current* category |
| `Resolution` | 55% | only ever `Done` in this export |
| `Resolved` | 55% | resolution timestamp |
| `Created`, `Updated` | 100% | |
| `Assignee` | 94% | current assignee only (§8.2) |
| `Project key`, `Project name` | 100% | one project; `Project type` = `software` |
| `Parent key`, `Parent summary` | 24% | the parent epic |
| `Fix versions` | 55% | mostly one catch-all release value |
| `Sprint` | 73% | sprint history (repeated header) |
| `Custom field (Dev)` / `(QA)` / `(BA)` / `(PM)` | 30–60% | role fields (§8.2) |

**Status semantics observed.**
- The `Done` category holds real completions (`Done`) **and** cancellations
  (`won't do`, `Wont Do` — two spellings). Almost all cancellations have no
  `Resolution`/`Resolved`. "Done category ⇒ completed" would count every
  cancellation as delivered work.
- Some `Done` issues have no `Resolved`; `Status category changed` gives
  their completion time. Where both exist they agree for ~96% of issues.
- To Do category: `New`, `Backlog`, `Analysis`, `Ready for Design`,
  `Waiting for Info`. In Progress category: `Design`, `In Dev`,
  `Code Review`, `Ready for QA`, `In QA`, `Ready for UAT`, `In UAT`,
  `Ready to Deploy`, `Blocked`.

**History window.** Created dates go back to 2024, but every `Resolved` is
2026-04-08 or later: the export's query kept open work plus about six months
of finished work.

**Why `-input` alone can't take it, even after `CONNECTORS_PLAN` Phase 1.**
The v1 reader errors ("no recognized columns"). But the v2 reader knows
`status`, `assignee` and `resolution`, and matches headers case-insensitively,
so it *would* recognize `Status`, `Assignee` and `Resolution` and load 983
issues with **empty identifiers and no timestamps** — silent garbage. Phase A
step 2 closes that hole (D-I12).

---

## 3. Decisions

| # | Decision |
|---|---|
| D-I1 | **Convert once, then use the normal tools.** `forecast import` reads a raw export plus a mapping file and writes a normalized source (`.db`, `.csv`, `.json`/`.jsonl`). The analysis commands, their `-config` files, `check`, and the web UI never learn about mappings. This is `CONNECTORS_PLAN.md` D14 (normalize at ingest): a mapped file import is a "file connector". |
| D-I2 | **Mapping files are YAML** (`gopkg.in/yaml.v3`, already in `go.mod`), decoded strictly: an unknown key anywhere is an error. |
| D-I3 | **Targets are v2 column names** (`CONNECTORS_PLAN.md` §4.3), exactly as `issues` reads them. An unknown target is an error. |
| D-I4 | **Status = named overrides, else a category column.** New workflow statuses get classified through their category with no mapping edit; only statuses that break the category rule (cancellations living in Done) need an override. A row matching nothing is a hard error listing every unmatched value with its row count. |
| D-I5 | **Lifecycle timestamps are conditional on the mapped status.** `completed_at` is only set when the row maps to `completed`, etc. That is what keeps a cancellation's close date out of `completed_at`. |
| D-I6 | **Repeated headers must be resolved explicitly.** Referencing a header that appears more than once without `pick:` is an error. |
| D-I7 | **An import is a snapshot of its source.** Writing to a `.db` deletes every row whose `source` equals the mapping's `source` and inserts the imported set, in one transaction. Issues deleted in Jira vanish on the next import; rows of other sources are untouched. `.csv`/`.json` output is written to a temp file in the same directory and renamed over the target. |
| D-I8 | **`internal/importmap` is pure**: no `os`, no clock, no logging, no `app`/`sqlite`/`source` imports. Adapters do the IO. |
| D-I9 | **Starter templates are embedded in the binary** (`go:embed`) and printed by `forecast import -example <name>`. The colleague installs a release binary, not the repo. |
| D-I10 | **Every import prints a report** (§7) and `-dry-run` prints only the report. |
| D-I11 | **Lookups forgive spelling, never meaning.** Header names and matched values are compared after trimming whitespace and lowercasing (`strings.ToLower(strings.TrimSpace(s))`), and a leading BOM is ignored. Nothing fuzzier. Errors about a missing header suggest the closest existing header (Levenshtein distance ≤ 3, ties → first in header order). |
| D-I12 | **`-input` keeps reading CSV/JSON — normalized files only.** It is the "bring your own data" path (and `import -out x.csv` produces such files). Two changes: a CSV whose header has no `identifier` column is rejected (it can't be a normalized file), and when that header looks like a known tracker export the error says to run `forecast import` (§5.3). No `-map` on `-input`. Possible later: implement `-input`'s CSV reading as `importmap` with a built-in identity mapping, if the two readers drift — not in this plan. |

---

## 4. The mapping file (normative)

### 4.1 Example

```yaml
version: 1                          # required; only 1 exists
source: jira                        # required; stored in `source`, part of the db key
format: csv                         # optional; only "csv"

time_format: "02/Jan/06 3:04 PM"    # required if any timestamp is mapped; string or list, tried in order
timezone: America/Denver            # optional; IANA name, "Local" or "UTC"; default "Local"

columns:
  identifier:  Issue key            # plain string = that header, must be unique in the file
  title:       Summary
  assignee:    Assignee
  team_key:    Project key
  team_name:   Project name
  project_id:  Parent key
  project_name: Parent summary
  milestone_name: {column: Fix versions, pick: first}
  issue_type:  Issue Type
  parent_identifier: Parent key
  status_name: Status
  resolution:  Resolution
  created_at:  Created
  updated_at:  Updated
  # team_key: {value: BSD}          # a constant instead of a column

status:
  column: Status category
  values:
    To Do: backlog
    In Progress: started
    Done: completed
  overrides:                        # keyed on the row's status_name value
    won't do: canceled
    Wont Do: canceled
  resolution_overrides:             # keyed on the row's resolution value
    Duplicate: canceled

timestamps:
  started_at:   {columns: [Status category changed], when: started}
  completed_at: {columns: [Resolved, Status category changed], when: completed}
  canceled_at:  {columns: [Resolved, Status category changed], when: canceled}

exclude:
  Issue Type: [Epic]

detect: [Issue key, Status category, Summary]   # templates only; see §5.3
```

### 4.2 Keys

| Key | Required | Meaning |
|---|---|---|
| `version` | yes | integer `1` |
| `source` | yes | non-empty string, `[a-z0-9-]+` |
| `format` | no | `csv` (default) |
| `time_format` | if any `*_at` target is mapped | Go layout string or list of them |
| `timezone` | no | passed to `time.LoadLocation`; default `Local` |
| `columns` | yes | map target → column rule (§4.3); must include `identifier` |
| `status` | no | §4.4 |
| `timestamps` | no | map lifecycle target → timestamp rule (§4.5) |
| `exclude` | no | map header → list of values (§4.6) |
| `detect` | no | list of headers (§5.3) |

**Valid targets** (D-I3): `source` is *not* a valid target (it comes from
the top-level key). Every other v2 column is: `identifier`, `title`, `url`,
`assignee`, `team_key`, `team_name`, `project_id`, `project_name`,
`milestone_id`, `milestone_name`, `issue_type`, `parent_identifier`,
`status`, `status_name`, `resolution`, `created_at`, `started_at`,
`completed_at`, `canceled_at`, `updated_at`. Mapping `status` under
`columns` means the column already holds the four v2 values
(`issues.ParseStatus`; unknown values are an error, like D-I4) and is
mutually exclusive with the `status:` block.

### 4.3 Column rules (`columns.<target>`)

A rule is one of:

| YAML | Meaning |
|---|---|
| `Issue key` | that header; error if missing or repeated |
| `{column: Fix versions, pick: first}` | `pick` ∈ `first` (first non-empty occurrence in header order), `last` (last non-empty), `join` (all non-empty, joined with `", "`). Allowed on a unique header (no effect). |
| `{columns: [A, B]}` | first non-empty of headers A, B (each must be unique, or carry its own `pick` — not supported; repeated headers here are an error) |
| `{value: BSD}` | that constant on every row |

Exactly one of `column`/`columns`/`value` per mapping-form rule; `pick` only
with `column`. Values are trimmed of surrounding whitespace.

Targets ending in `_at` are parsed with `time_format` in `timezone`
(`time.ParseInLocation`, layouts tried in order); empty stays zero; a
non-empty value no layout accepts is a row error (§4.8). A lifecycle target
(`started_at`, `completed_at`, `canceled_at`) may be in `columns`
(unconditional) or in `timestamps` (conditional), not both.

### 4.4 `status`

```yaml
status:
  column: <header>                  # optional
  values: {<value>: <status>, …}    # required iff column is set
  overrides: {<status_name value>: <status>, …}            # optional
  resolution_overrides: {<resolution value>: <status>, …}  # optional
```

`<status>` ∈ `backlog`, `started`, `completed`, `canceled`. Per row, the
first rule that matches wins:

1. `overrides[status_name]` (requires `status_name` in `columns`)
2. `resolution_overrides[resolution]` (requires `resolution` in `columns`;
   an empty resolution never matches)
3. `values[<column>]`
4. no match → row error (§4.8)

Keys match per D-I11 (so `won't do` matches `Won't Do`). Two keys in the
same map that are equal after normalization are a parse error. Keys that
never occur in the data are fine (templates list common spellings).
Without a `status` block (and no `status` column), `status` is
`issues.StatusUnknown` and the `Is*` helpers rely on timestamps alone.

### 4.5 `timestamps.<target>`

`{columns: [<header>, …], when: <status> | [<status>, …]}` — target is one
of `started_at`, `completed_at`, `canceled_at`; `when` is required. The
first non-empty header's value is parsed (as §4.3); if the row's mapped
status is not in `when`, the target stays zero even if a column has a value.
Requires a `status` block or a `status` column (else parse error: "`when`
needs a status rule").

### 4.6 `exclude`

Map header → list of values. A row is dropped when any listed header's
value (D-I11) is in its list. Dropped rows are not validated further (an
excluded row with an unknown status is not an error) and are counted per
header/value in the report. Headers must exist and be unique.

### 4.7 Parse-time validation (`importmap.Parse`)

All of these are errors from `Parse`, before any data is read. Messages
start with the YAML path:

| Check | Example message |
|---|---|
| YAML syntax / unknown key | `mapping: line 7: field colums not found in type importmap.rawMapping` (yaml.v3's text, prefixed `mapping: `) |
| `version` ≠ 1 | `version: unsupported mapping version 2 (this forecast understands 1)` |
| `source` missing/invalid | `source: required (e.g. "jira")` |
| unknown target | `columns.state_type: unknown target; valid targets: identifier, title, …` |
| `identifier` missing | `columns.identifier: required` |
| bad rule shape | `columns.milestone_name: use exactly one of column, columns, value` |
| bad `pick` | `columns.milestone_name.pick: must be first, last or join` |
| bad status value | `status.overrides["won't do"]: "cancelled" is not a status; use backlog, started, completed or canceled` |
| duplicate normalized key | `status.values: "Done" and "done" are the same value` |
| `values` without `column` (or vice versa) | `status: values and column go together` |
| override needs column | `status.overrides: needs columns.status_name` |
| `when` invalid / missing | `timestamps.completed_at.when: required` |
| target in both | `timestamps.completed_at: also mapped under columns` |
| no `time_format` | `time_format: required because columns.created_at is a timestamp` |
| bad layout | `time_format: "02/Jan/06" does not round-trip a timestamp` — check: `t := time.Date(2026,10,22,19,55,0,0,time.UTC)`; the layout must satisfy `time.Parse(l, t.Format(l))` without error and yield a non-zero time |
| bad timezone | `timezone: unknown time zone "Mountain"` |

Implementation note: decode into a raw struct whose rule fields are
`yaml.Node`, with `decoder.KnownFields(true)`; interpret each node by
`node.Kind` (`ScalarNode` → plain header, `MappingNode` → decode into a
strict struct, `SequenceNode` for lists). Map iteration order must be the
YAML's order (walk `MappingNode.Content` pairs, don't range over a Go map)
so error messages and report rows are deterministic.

### 4.8 Header and row validation (`importmap.Apply`)

**Header checks** (before any row), all collected and reported together:
- every referenced header exists — `columns.title: no column "Sumary" in the file (closest: "Summary")`
- every referenced header without `pick` is unique — `columns.milestone_name: column "Fix versions" appears 2 times; add pick: first, last or join`

**Row checks**, collected over the whole file, then reported together
(cap: the first 5 example rows per problem). "Row N" is the record's
position counting the header as row 1, so the first data row is row 2 —
what a spreadsheet shows. (It is not the text line number: quoted fields
can span lines.)
- `identifier` empty — `identifier: empty on 2 rows (rows 5, 9)`
- `identifier` repeated — `identifier: "DEMO-3" appears on rows 4 and 12`
- unparseable timestamp — `created_at (column "Created"): 3 values match no time_format, e.g. row 7 "2026-10-22"`
- unmatched status — `status: 2 rows match no rule: "Paused" (1, row 8), "On Hold" (1, row 11)`
  (values listed in first-seen order with counts; the value shown is the
  `status.column` value when a column is set, else the `status_name` value)

If any header or row problem exists, `Apply` returns `nil` issues and an
`*importmap.DataError` whose `Error()` is the problems joined with `"\n"`.

---

## 5. Code

### 5.1 `internal/importmap` (new)

```go
// Package importmap turns a tracker's CSV export into issues.Issue records
// using a declarative mapping file. It is pure: callers do the IO.
package importmap

type Mapping struct {
	Version     int
	Source      string
	TimeFormats []string
	Location    *time.Location
	Columns     []ColumnRule      // YAML order
	Status      *StatusRule       // nil when absent
	Timestamps  []TimestampRule   // YAML order
	Exclude     []ExcludeRule     // YAML order
	Detect      []string
}

type ColumnRule struct {
	Target  string   // v2 column name
	Headers []string // one for `column`/plain, several for `columns`; empty for `value`
	Pick    string   // "", "first", "last", "join"
	Value   string   // used when Headers is empty
}

type StatusRule struct {
	Column              string                   // "" when only overrides are used
	Values              []StatusMatch            // YAML order
	Overrides           []StatusMatch
	ResolutionOverrides []StatusMatch
}

type StatusMatch struct {
	Raw    string        // as written in the YAML
	Status issues.Status
}

type TimestampRule struct {
	Target  string
	Headers []string
	When    []issues.Status
}

type ExcludeRule struct {
	Header string
	Values []string
}

// Parse decodes and validates a mapping file (§4.7).
func Parse(data []byte) (*Mapping, error)

// ReadCSV reads a whole CSV export: strips a leading UTF-8 BOM, returns the
// header row and the data rows. Uses encoding/csv with default settings
// (FieldsPerRecord 0 — every row must match the header's width).
func ReadCSV(r io.Reader) (header []string, rows [][]string, err error)

// Apply maps rows onto issues (§4.8). Issues come back in file order with
// Source set from the mapping. On any problem it returns nil issues and a
// *DataError; the Report is still filled as far as the scan got.
func Apply(m *Mapping, header []string, rows [][]string) ([]issues.Issue, Report, error)

type DataError struct{ Problems []string }

func (e *DataError) Error() string // strings.Join(e.Problems, "\n")

// Templates lists the embedded templates' names, sorted.
func Templates() []string
// Template returns an embedded template's YAML text.
func Template(name string) ([]byte, bool)

// Suggest returns the first template (in Templates() order) whose `detect`
// headers are all present in header (D-I11 matching), for the -input hint.
func Suggest(header []string) (name string, ok bool)
```

`Report` (JSON tags so the web UI can serve it as-is):

```go
type Report struct {
	Rows       int            `json:"rows"`        // data rows read
	Columns    int            `json:"columns"`     // header width
	Excluded   []ValueCount   `json:"excluded"`    // Label "Issue Type = Epic", rule then first-seen order
	Imported   int            `json:"imported"`
	Status     []StatusCount  `json:"status"`      // always the four statuses in order backlog, started, completed, canceled (+ "unknown" only if > 0)
	Timestamps []FieldSource  `json:"timestamps"`  // every *_at target mapped, in v2 column order
	Fields     []FieldFill    `json:"fields"`      // every non-timestamp target mapped, in v2 column order
}

type ValueCount struct {
	Label string `json:"label"`
	N     int    `json:"n"`
}

type StatusCount struct {
	Status string       `json:"status"`
	N      int          `json:"n"`
	Via    []ValueCount `json:"via"` // see below
}

type FieldSource struct {
	Target string       `json:"target"`
	Set    int          `json:"set"`   // rows where it ended up non-zero
	From   []ValueCount `json:"from"`  // Label = header the value came from, in the rule's header order
}

type FieldFill struct {
	Target string `json:"target"`
	Filled int    `json:"filled"`  // non-empty among imported rows
}
```

`StatusCount.Via`: one entry per distinct (rule, value) that produced this
status. `Label` is the matched value **as it first appeared in the data**
(not the YAML key's spelling), suffixed ` (override)` for rule 1 and
` (resolution)` for rule 2; category matches (rule 3) have no suffix. Order:
rule 1 entries, then rule 2, then rule 3; within a rule, first-seen order.

Templates live in `internal/importmap/templates/*.yaml`, embedded with
`//go:embed templates/*.yaml`; the name is the file name without `.yaml`.
The first line of every template must be `# name: <display name>`;
`Describe(name string) string` returns that display name (a test asserts
every embedded template has one).

### 5.2 `issues` additions

- `ReadCSV`: strip a leading BOM from the first header cell; a recognized
  header appearing twice is an error (`column "assignee" appears 2 times`);
  a header with no `identifier` column returns `*issues.HeaderError`:
  ```go
  // HeaderError reports a CSV header that can't be a forecast data file.
  type HeaderError struct {
  	Header []string // the header row as read (BOM stripped)
  	Msg    string
  }
  func (e *HeaderError) Error() string { return e.Msg }
  ```
  with `Msg` = `no "identifier" column; expected the forecast column names (see DATA_REQUIREMENTS.md)`. The existing "no recognized columns" error also becomes a `*HeaderError` (same text as today).
- `Columns() []string` — the v2 column names in schema order (export the
  existing `columnNames` list via a function returning a copy).
- `WriteCSV(w io.Writer, items []Issue) error` — header `Columns()`, one
  row per issue; timestamps as `t.Format(time.RFC3339)` (seconds precision
  is all any source has), zero time as `""`; `status` as its string.
- `WriteJSON(w io.Writer, items []Issue) error` — JSON Lines, one object per
  issue with the same keys; zero time as `null`.
- Round-trip guarantee (tested): `ReadCSV(WriteCSV(x)) == x` and likewise
  JSON, for every field, given timestamps without sub-second parts.

### 5.3 The `-input` hint (D-I12)

In `internal/source/source.go`'s `Load` (or, if `CONNECTORS_PLAN.md`
Phase 2 has landed, in `issues.FileStore.Load` — wherever the CSV branch
calls `issues.ReadFile`): when the error is a `*issues.HeaderError`
(`errors.As`) and `importmap.Suggest(he.Header)` finds a template, return:

```
<path>: this looks like a Jira Cloud CSV export (template "jira-cloud-csv"), which needs converting first:
  forecast import -example jira-cloud-csv > mapping.yaml
  forecast import -from <path> -map mapping.yaml -out data.db
```

"Jira Cloud CSV export" is `importmap.Describe("jira-cloud-csv")`. Without
a suggestion, return `<path>: <HeaderError text>`.

### 5.4 `sqlite` addition

```go
// ReplaceSource deletes every issue whose source is src and inserts items
// (whose Source must all equal src), in one transaction. It returns how
// many rows were deleted.
func (s *Store) ReplaceSource(ctx context.Context, src string, items []issues.Issue) (deleted int, err error)
```

Reuse the insert statement `Upsert` uses (factor it into a helper taking a
`*sql.Tx`); an item whose `Source != src` is an error before anything is
written.

### 5.5 `forecast import` (CLI)

```
forecast import -from <export.csv> -map <mapping.yaml> -out <file.db|.csv|.json|.jsonl> [-dry-run] [-config f.yaml]
forecast import -example list
forecast import -example <name>
```

- `-from` (not `-input`, so nobody confuses a raw export with a data
  source; not positional, because no other command takes positionals and
  the web adapter sets flags only). `-` is rejected: `-from: stdin is not supported; pass the export file`.
- `-out` is required unless `-dry-run`; its extension picks the writer:
  `.db`/`.sqlite`/`.sqlite3` → `sqlite.Open` + `ReplaceSource`;
  `.csv` → `issues.WriteCSV`; `.json`/`.jsonl`/`.ndjson` → `issues.WriteJSON`;
  anything else → `-out: unrecognized extension ".txt" (use .db, .csv, .json or .jsonl)`.
- `-example list` prints `Templates()` one per line; `-example <name>`
  prints the template verbatim; both exit without needing other flags, and
  `-example` with any of `-from`/`-map`/`-out`/`-dry-run` is an error.
  Unknown name: `-example: no template "jira"; available: jira-cloud-csv`.
- Flag usage strings:
  - `-from`: `path to the tracker's CSV export to convert`
  - `-map`: `path to the mapping YAML file (start from -example)`
  - `-out`: `where to write the converted data: .db (replaces this mapping's source), .csv or .json`
  - `-dry-run`: `report what would be imported without writing anything`
  - `-example`: `print an embedded mapping template ("list" names them) and exit`
- The work after flag resolution is shared by both adapters, in a new
  `internal/source/import.go` (`source` is the IO-boundary package; it may
  import `app`, `importmap`, `issues`, `sqlite`):
  ```go
  type ImportResult struct {
  	Issues  []issues.Issue
  	Report  importmap.Report
  	Deleted int  // rows replaced in a .db; 0 otherwise
  	Wrote   bool // false for a dry run
  }

  // Import reads and parses the mapping, reads the export, applies the
  // mapping and, unless dryRun, writes out. Paths are used as given.
  func Import(ctx context.Context, from, mapPath, out string, dryRun bool) (ImportResult, error)
  ```
  Steps: read + `importmap.Parse` the mapping (error →
  `&app.FieldError{Fields: []string{"map"}, Msg: "-map " + mapPath + ": " + err.Error()}`)
  → open + `importmap.ReadCSV` the export (field `from`, prefix `-from <path>: `)
  → `Apply` (field `from`, same prefix) → unless dry-run, write `out`
  (field `out`). Nothing is written if any earlier step fails. The returned
  result is non-nil-valued even with an error (Report filled as far as it
  got), matching `internal/app`'s convention.
- `cmdImport` handles `-example` itself (prints and returns before calling
  `source.Import`), then calls `source.Import`, then
  `report.Import(os.Stdout, …)` with
  `readiness = app.CheckReadiness(res.Issues)`. Paths in the report are the
  values as typed (like `check` prints `r.InputPath`), so CLI and web
  outputs are byte-identical.
- `params.Import` (new file `internal/params/import.go`):
  ```go
  type ImportRequest struct {
  	From, Map, Out string
  	DryRun         bool
  	Example        string
  }
  type Import struct {
  	From, Map, Out, Example *string
  	DryRun                  *bool
  	Config                  *string
  }
  func NewImport(fs *flag.FlagSet) *Import
  func (c *Import) Resolve() (Resolved[ImportRequest], error) // InputPath = From
  ```
- `topCommands` entry, inserted after `history`:
  `{Name: "import", Summary: "Convert a tracker export into a forecast data source using a mapping file.", Run: cmdImport}`.

---

## 6. The synthetic fixture (copy exactly)

`testdata/jira-export-sample.csv` — shared by `importmap` tests
(`../../testdata/jira-export-sample.csv`) and golden cases (`@J`). It must
start with a UTF-8 BOM: write the content below, then prepend the three
bytes (`printf '\xef\xbb\xbf' | cat - body.csv > testdata/jira-export-sample.csv`)
and check with `head -c 3 testdata/jira-export-sample.csv | xxd`. Line
endings `\n`. All people and names are invented.

```csv
Summary,Issue key,Issue id,Issue Type,Status,Project key,Project name,Project type,Priority,Resolution,Assignee,Created,Updated,Resolved,Fix versions,Fix versions,Sprint,Sprint,Sprint,Custom field (Dev),Parent key,Parent summary,Status category,Status category changed,Comment,Comment
Checkout revamp,DEMO-1,10001,Epic,In Dev,DEMO,Demo Delivery,software,High,,Alice Example,01/Sep/26 9:00 AM,20/Oct/26 4:10 PM,,Release 1,,Apollo,,,,,,In Progress,15/Sep/26 10:00 AM,,
Cart page layout,DEMO-2,10002,Story,Done,DEMO,Demo Delivery,software,Medium,Done,Alice Example,02/Sep/26 9:30 AM,10/Oct/26 3:00 PM,10/Oct/26 2:45 PM,Release 1,Release 2,Apollo,Brahma,,Alice Example,DEMO-1,Checkout revamp,Done,10/Oct/26 2:45 PM,"Looks good, shipping",
Update tax table,DEMO-3,10003,Task,Done,DEMO,Demo Delivery,software,Low,,Bob Example,03/Sep/26 11:00 AM,08/Oct/26 9:20 AM,,,,Apollo,,,Bob Example,,,Done,08/Oct/26 9:15 AM,,
Old coupon bug,DEMO-4,10004,Bug,won't do,DEMO,Demo Delivery,software,Low,,Bob Example,04/Sep/26 1:00 PM,12/Oct/26 10:00 AM,,,,,,,,,,Done,12/Oct/26 10:00 AM,Not reproducible,
Legacy export,DEMO-5,10005,Task,Wont Do,DEMO,Demo Delivery,software,Medium,Done,Carol Example,05/Sep/26 2:00 PM,14/Oct/26 11:30 AM,14/Oct/26 11:25 AM,,,,,,,,,Done,14/Oct/26 11:25 AM,,
Address autocomplete,DEMO-6,10006,Story,In Dev,DEMO,Demo Delivery,software,High,,Carol Example,06/Sep/26 9:00 AM,21/Oct/26 8:00 AM,,Release 2,,Brahma,,,Carol Example,DEMO-1,Checkout revamp,In Progress,13/Oct/26 9:30 AM,,
Payment retry,DEMO-7,10007,Story,In UAT,DEMO,Demo Delivery,software,Highest,Done,Alice Example,07/Sep/26 10:00 AM,21/Oct/26 1:00 PM,20/Oct/26 5:00 PM,Release 2,,Brahma,,,Alice Example,DEMO-1,Checkout revamp,In Progress,01/Oct/26 9:00 AM,,
Gift cards,DEMO-8,10008,Story,Backlog,DEMO,Demo Delivery,software,No priority,,,08/Sep/26 9:00 AM,08/Sep/26 9:00 AM,,,,,,,,,,To Do,08/Sep/26 9:00 AM,,
Shipping rules review,DEMO-9,10009,Task,Analysis,DEMO,Demo Delivery,software,Medium,,Bob Example,09/Sep/26 3:00 PM,18/Oct/26 2:00 PM,,,,,,,,,,To Do,09/Sep/26 3:00 PM,,
Write cart tests,DEMO-10,10010,Sub-task,Done,DEMO,Demo Delivery,software,Medium,Done,,10/Sep/26 9:00 AM,30/Sep/26 4:00 PM,30/Sep/26 3:55 PM,Release 1,,Apollo,,,,DEMO-2,Cart page layout,Done,30/Sep/26 3:55 PM,,
"Fix ""Save"" button, again",DEMO-11,10011,Bug,Done,DEMO,Demo Delivery,software,Urgent,Done,Carol Example,11/Sep/26 8:00 AM,02/Oct/26 12:10 PM,02/Oct/26 12:00 PM,,,Apollo,Brahma,Comet,Carol Example,,,Done,02/Oct/26 12:00 PM,"First line
second line","Another, comment"
Loyalty points,DEMO-12,10012,Story,Waiting for Info,DEMO,Demo Delivery,software,Lowest,,Alice Example,12/Sep/26 9:00 AM,19/Oct/26 9:00 AM,,,,,,,,,,To Do,12/Sep/26 9:00 AM,,
```

What it exercises, with the shipped template (§6.1) and `timezone: UTC`:

| Row | Expect |
|---|---|
| DEMO-1 | excluded (`Issue Type = Epic`) |
| DEMO-2 | completed; `completed_at` from `Resolved` (2026-10-10 14:45 UTC); `milestone_name` = `Release 1` (pick first); project `Checkout revamp` / `DEMO-1`; parent `DEMO-1` |
| DEMO-3 | completed; no `Resolved` → `completed_at` from `Status category changed` (2026-10-08 09:15) |
| DEMO-4 | canceled via override `won't do`; `canceled_at` from `Status category changed` |
| DEMO-5 | canceled via override `Wont Do`; `canceled_at` from `Resolved` (2026-10-14 11:25) even though resolution is `Done` |
| DEMO-6 | started; `started_at` = 2026-10-13 09:30 |
| DEMO-7 | started (category); has `Resolved` but `completed_at` stays zero (D-I5) |
| DEMO-8 | backlog; empty assignee; `started_at` zero although `Status category changed` is set |
| DEMO-9 | backlog (`Analysis` is To Do) |
| DEMO-10 | completed Sub-task, kept; empty assignee; parent `DEMO-2` |
| DEMO-11 | completed; quoted title with comma and quotes; multi-line comment |
| DEMO-12 | backlog |

Expected report counts: rows 12, columns 26, excluded 1 (`Issue Type = Epic`),
imported 11; backlog 3 (`To Do` 3), started 2 (`In Progress` 2), completed 4
(`Done` 4), canceled 2 (`won't do (override)` 1, `Wont Do (override)` 1);
`started_at` set 2 (`Status category changed` 2); `completed_at` set 4
(`Resolved` 3, `Status category changed` 1); `canceled_at` set 2
(`Resolved` 1, `Status category changed` 1).

Error-path tests build small CSVs inline in Go (`strings.NewReader`) rather
than adding more fixture files.

### 6.1 The shipped template

`internal/importmap/templates/jira-cloud-csv.yaml` (copy exactly; the
comments are the colleague's documentation):

```yaml
# name: Jira Cloud CSV export
#
# Mapping for a Jira Cloud "Export > CSV (all fields)" file. Copy it, adjust
# the marked lines, then:
#   forecast import -from export.csv -map this-file.yaml -out data.db
#
# Every rule below is checked against the export; anything that doesn't fit
# stops the import with a message saying what to change here.
version: 1
source: jira

# Jira writes dates in the exporting user's profile format and timezone.
# CHANGE timezone to that profile's zone (Jira: Profile > Account settings),
# e.g. America/New_York. Change time_format only if your dates don't look
# like 22/Oct/26 7:55 AM (it is a Go time layout).
time_format: "02/Jan/06 3:04 PM"
timezone: Local

columns:
  identifier: Issue key
  title: Summary
  # Assignee is whoever holds the issue at export time. If work is handed
  # between people (BA -> Dev -> QA), a role field such as
  # "Custom field (Dev)" may credit the right person; or forecast with
  # -whole-team and ignore assignees entirely.
  assignee: Assignee
  team_key: Project key
  team_name: Project name
  # "Project" (used by count, history and sim backtest) = the parent epic.
  # Alternatives: {column: Fix versions, pick: first}, or {value: All}.
  project_id: Parent key
  project_name: Parent summary
  milestone_name: {column: Fix versions, pick: first}
  issue_type: Issue Type
  parent_identifier: Parent key
  status_name: Status
  resolution: Resolution
  created_at: Created
  updated_at: Updated

# Status: Jira's status category decides, except for the statuses and
# resolutions listed under overrides. A status your team adds later is
# handled by its category automatically. Matching ignores case.
status:
  column: Status category
  values:
    To Do: backlog
    In Progress: started
    Done: completed
  overrides:
    # Done-category statuses that mean "not delivered". Add your own.
    Won't Do: canceled
    Wont Do: canceled
    Canceled: canceled
    Cancelled: canceled
    Declined: canceled
    Duplicate: canceled
  resolution_overrides:
    Won't Do: canceled
    Won't Fix: canceled
    Duplicate: canceled
    Cannot Reproduce: canceled
    Declined: canceled

# The export only records when an issue entered its *current* category, so
# started_at is only known for work currently in progress. If your Jira admin
# adds a "Work Started" date field (set by automation on the first move to an
# In Progress status), use:
#   started_at: {columns: [Custom field (Work Started), Status category changed], when: [started, completed, canceled]}
timestamps:
  started_at: {columns: [Status category changed], when: started}
  completed_at: {columns: [Resolved, Status category changed], when: completed}
  canceled_at: {columns: [Resolved, Status category changed], when: canceled}

# Rows dropped before anything else. Epics are containers, not deliverables.
# Add Sub-task here if sub-tasks shouldn't count as throughput.
exclude:
  Issue Type: [Epic]

# Used by `forecast` to recognize a Jira export passed to -input.
detect: [Issue key, Status category, Summary]
```

`Template` returns this text verbatim. Two override keys equal after D-I11
normalization would be a parse error — the template has none (`Won't Do` and
`Wont Do` differ).

---

## 7. The import report

Rendered by `report.Import(w io.Writer, from, out string, dryRun bool, deleted int, r importmap.Report, readiness []app.CheckResult)` in
`internal/report/import.go`. Layout (the golden file freezes the exact
bytes; match this shape, two-space indents, `%-12s`-style label columns):

```
read      12 rows, 26 columns from ../../testdata/jira-export-sample.csv
excluded   1 Issue Type = Epic
imported  11 issues (source "jira")

status
  backlog      3  To Do 3
  started      2  In Progress 2
  completed    4  Done 4
  canceled     2  won't do (override) 1, Wont Do (override) 1

timestamps
  created_at   11  Created 11
  started_at    2  Status category changed 2
  completed_at  4  Resolved 3, Status category changed 1
  canceled_at   2  Resolved 1, Status category changed 1
  updated_at   11  Updated 11

fields (non-empty of 11)
  title 11, assignee 9, team_key 11, team_name 11, project_id 4, …

Read 11 issues from the imported set
  history   ok
  cfd       ok
  aging     <CheckReadiness's text for 0 of 4 completed issues having started_at>
  count     ok
  sim       <CheckReadiness's text for 1 completed issue without assignee>

dry run: nothing written
```

- One `excluded` line per `Excluded` entry (omit the line when none).
- Status rows with `N == 0` still print (`canceled  0`) so the shape is
  stable; `unknown` prints only when > 0.
- The block before the last line is `report.Check(w, len(items), "the imported set",
  readiness)` with `readiness = app.CheckReadiness(items)` computed by the
  adapter — import and `check` can never disagree about gaps.
- `fields` lists every non-timestamp target that is mapped, in `Columns()`
  order, as `<target> <filled>` joined by `", "`, wrapped at 76 columns with
  a two-space indent on continuation lines.
- Last line: `dry run: nothing written`, or `wrote 11 issues to bsd.db
  (replaced 9 issues from source "jira")` for a db (`replaced 0 …` on a new
  file), or `wrote 11 issues to bsd.csv`.

---

## 8. What the export cannot provide

### 8.1 Per command

| Command | With this export |
|---|---|
| `sim items` / `days` / `probability` | **Works.** Needs only `completed_at` (+ `assignee` unless `-whole-team`). |
| `count` | **Works**, given a choice of what "project" means (§11). |
| `aging`, current WIP | **Works, with a caveat:** age counts from entering the In Progress *category*; an item that went back to To Do and returned has its clock reset. |
| `aging`, cycle-time distribution | **Empty.** No completed issue has `started_at` (§8.3). |
| `cfd`, `history` | **Partial.** Created / Completed / Canceled lines are right. Finished work leaves the backlog on the day it completes (`history.Normalize`'s fallback), so the in-progress band only shows current WIP and cycle time, average WIP and the Little's Law check are wrong. |
| `sim backtest` | **Partial.** Needs an explicit `-replay-start-date` (inference uses the earliest `started_at`, here only current WIP) and a project choice. |
| Before ≈ 2026-05 | Finished work resolved before the export's window is absent. |

### 8.2 Assignee is the *last* holder

The project has BA / Dev / QA / PM role fields, which suggests issues are
handed between people; `Assignee` is whoever held the issue at export time,
so per-engineer `sim` modes may credit the wrong person. `-whole-team` is
unaffected; mapping `assignee: Custom field (Dev)` is the alternative. The
template's comments say this.

### 8.3 Why `started_at` can't be recovered from this export

- `Status category changed` records only the *latest* category change; for a
  done issue that is the move to Done.
- `[CHART] Time in Status` holds per-status durations, but only on resolved
  issues, keyed by numeric status ids (no names in the export), and not in
  chronological order. Not pursued.

Ways to get it, cheapest first: (1) a Jira automation rule that stamps a
*Work Started* date field on the first move into an In Progress-category
status — forward-only, and the mapping change is already written in the
template's comment; (2) the Jira API connector (`CONNECTORS_PLAN.md`
Phase 3), changelog-derived for all history.

---

## 9. Web UI

The UI builds each command's form generically from the registry and
`meta.go` (`internal/web/static/js/form.js` renders by field kind), so the
Import page is mostly wiring:

- `registry` entry after `check`:
  `{ID: "import", Name: "import", Summary: <same as CLI>, Group: "data", Title: "Import a tracker export", Exports: []string{"text", "json"}, newFlags: newImportFlags, prepare: prepareImport}`.
- `prepareImport` in `internal/web/cmd_data.go`: resolve; reject a
  non-empty `Example` (`-example is CLI-only; download a template with forecast import -example`
  — but `meta.go` hides the flag, so this is defense in depth); every path
  through `rc.s.resolvePath` (`from`, `map`, and `out` when not dry-run);
  then, in the runner, `source.Import(ctx, absFrom, absMap, absOut, dryRun)`
  — but render the report with the *typed* paths, as `prepareCheck` does
  with `r.InputPath`. After a db write, `rc.s.datasets.invalidate(absOut)`.
  Text export = `report.Import(…)` bytes; json export =
  `{"report": …, "readiness": …, "wrote": bool, "deleted": n}`.
- `meta.go`: `metaOverrides["import"]` =
  `from` → `meta("raw-path", "data", "Tracker export (CSV)")`,
  `map` → `meta("map-path", "data", "Mapping file (YAML)")`,
  `out` → `meta("db-path", "data", "Write to")`,
  `dry-run` → `meta("bool", "basic", "Dry run (report only)")`,
  `example` → `hidden("CLI-only: prints a template to stdout")`.
- `files.go` `handleFiles`: new kinds `raw` (`.csv`) and `map`
  (`.yaml`, `.yml`); update the error text listing kinds.
- `uploads.go`: accept `.yaml`/`.yml` uploads (a separate `mapExts` list
  checked alongside `dataExts`); existing upload size limits apply.
- `form.js`: treat `raw-path` and `map-path` exactly like `db-path`
  (datalist from `filesFor(kind)` + Upload button) with kinds `raw`/`map`.
- `cmd/forecast/web_parity_test.go` `webIDs()`: add `cmdImport` → `"import"`.
- Verify by hand: `just build && bin/forecast serve -open`, use the
  built-in browser to upload the fixture and the template, dry-run, then
  import to a `.db` and open `count` on it.

---

## 10. Phases

Prerequisite: `CONNECTORS_PLAN.md` Phase 1 complete (§0.1 rule 1).

### Phase A — groundwork in `issues`

1. **Fixture and ignore rules.** Append to `.gitignore` (after the existing
   lines): `!testdata/jira-export-sample.csv` and
   `!internal/importmap/templates/*.yaml`. Add `testdata/jira-export-sample.csv`
   exactly as §6 (with BOM). `jj st` must show it added. No code.
   *Subject:* `Add a synthetic Jira CSV export fixture`
2. **Reader hardening** (§5.2, first bullet). Tests in `issues/read_test.go`:
   BOM-prefixed header reads the same as without; a repeated recognized
   header errors; a header without `identifier` returns `*HeaderError`
   (`errors.As`) carrying the header; reading the Jira fixture with
   `issues.ReadFile` returns a `*HeaderError` (this is the §2 hole). Existing
   goldens unchanged. Before changing the reader, grep the tests
   (`grep -rn "ReadCSV\|ReadFile\|ReadStream" --include=*_test.go .`) for any
   CSV without an `identifier` column; if one exists for a reason other than
   testing the "no recognized columns" error, stop and ask the user. *Subject:* `issues: strip CSV BOM, reject repeated and identifier-less headers`
3. **Writers** (§5.2, `Columns`/`WriteCSV`/`WriteJSON`). Tests: round-trip
   `testdata/sample-issues.csv` through each writer and back, `reflect.DeepEqual`
   on every issue; zero times write `""`/`null`; header equals `Columns()`.
   *Subject:* `issues: add CSV and JSON writers`

### Phase B — `internal/importmap` (pure)

1. **`Parse`** (§4.1–4.7) + `Mapping` types. Table test with one case per
   §4.7 row asserting the message prefix (YAML path); a happy-path test that
   §4.1's example parses to the expected `Mapping` (spot-check rules, order
   preserved); a YAML `won't do:` unquoted key parses. *Subject:* `importmap: parse and validate mapping files`
2. **`ReadCSV` + `Apply` + `Report`** (§4.8, §5.1). Tests: the fixture with
   the §6.1 template text (inline copy with `timezone: UTC`) yields exactly
   the §6 table, field by field for DEMO-2, -3, -5, -7, -8, -10, -11, and the
   §6 report counts; one inline-CSV test per §4.8 problem asserting message
   text; problems are collected (two different problems in one file → both
   reported); `exclude` skips validation of excluded rows; `pick: last` and
   `join`; `{value: …}`; `resolution_overrides` precedence (override beats
   resolution beats category). *Subject:* `importmap: apply a mapping to CSV rows`
3. **Templates** (§6.1, `Templates`/`Template`/`Suggest`). Tests: the
   embedded template parses; applied to the fixture it reports zero problems;
   `Suggest` finds it from the fixture header (BOM, case) and not from
   `testdata/sample-issues.csv`'s header. *Subject:* `importmap: embed a Jira Cloud CSV template`

### Phase C — the command

1. **`sqlite.ReplaceSource`** (§5.4). Tests: replaces only the given
   source; deleted count; mismatched `Source` errors with nothing written;
   an insert failure rolls back the delete. *Subject:* `sqlite: replace all issues of one source atomically`
2. **`forecast import`** (§5.5, §7). Files: `internal/params/import.go`,
   `internal/source/import.go` (`Import`), `internal/report/import.go`,
   `cmd/forecast/import.go`, `topCommands` entry; `@J` shorthand in
   `gargs`. Golden cases (each also replayed by the parity test, so the web
   side is needed for them to pass — **add the minimal web wiring from §9
   in this commit** (registry entry, `prepareImport`, `meta.go`, `webIDs`)
   and leave the UI widgets for step 4):
   - `import-dry-run`: `-from @J -map <template copy> -dry-run`. The template
     copy lives at `cmd/forecast/testdata/golden/jira-mapping.yaml` (covered
     by the existing `!cmd/forecast/testdata/**` negation) with
     `timezone: UTC` so the golden is zone-independent.
   - `err-import-bad-map`: a mapping with an unknown target (second file
     `cmd/forecast/testdata/golden/bad-jira-mapping.yaml`).
   - `err-import-unmatched-status`: a third mapping,
     `cmd/forecast/testdata/golden/partial-jira-mapping.yaml` = the template
     copy with the `Done: completed` line and the whole `overrides` block
     removed, so DEMO-2/3/4/5/10/11 match no rule (`status: 6 rows match no rule: "Done" (6, rows 3, 4, 5, 6, 11)` —
     the cap of 5 example rows applies).
   - `err-import-no-out`: `-from @J -map … ` without `-out`/`-dry-run`.
   Non-golden tests in `cmd/forecast/import_test.go` (they write files or
   print templates): `-out` to `t.TempDir()/x.db`, then load it with
   `source.Load` and compare to the dry-run's issues; `.csv` and `.jsonl`
   outputs likewise; importing twice into one db keeps the count and
   reports `replaced 11`; `-example list` / `-example jira-cloud-csv`
   output equals `importmap.Template`; `-example` with `-from` errors.
   *Subject:* `Add forecast import`
3. **`-input` hint** (§5.3). Tests in `internal/source/source_test.go`:
   loading the Jira fixture returns the hint naming `jira-cloud-csv` and the
   path; a header-less-identifier CSV that matches no template returns the
   plain `HeaderError` text. *Subject:* `Point -input at forecast import for tracker exports`
4. **Import page UI** (§9: `files.go`, `uploads.go`, `form.js`). Tests:
   `handleFiles` accepts `raw`/`map`; `.yaml` upload accepted, `.exe`
   still rejected; `TestStatic_ImportsResolve` green; then the by-hand
   check in §9. *Subject:* `web: Import page with export and mapping uploads`

### Phase D — docs

`README.md` "Bring your own data" gains "From a Jira export" (§1's four
commands, the §8.1 table in short); a new `docs/IMPORT_MAPPING.md` reference
(§4 rewritten for users: keys, rule shapes, matching rules, errors);
`DATA_REQUIREMENTS.md` links it and mentions the `identifier`-column rule;
`CLAUDE.md` gains `forecast import`, `internal/importmap`, and the `@J`
shorthand; `CHANGELOG.md`. *Subject:* `Document forecast import`

### Acceptance (local only, nothing committed)

With the colleague's real export and the shipped template (only `timezone`
edited): `forecast import -from "<export>" -map <file> -dry-run` succeeds;
the status counts are plausible (the user checks them against Jira);
`-out bsd.db` then `forecast sim items -input bsd.db -whole-team -days 30`
runs; `forecast check -input bsd.db` reports only the §8.1 gaps; `forecast
count -input "<export>"` prints the §5.3 hint. Report the results to the
user in chat, not in a file.

---

## 11. Questions for the colleague

**None of these block any phase.** They change her mapping file or her
command-line choices, never the code. Grouped by when they need an answer:

**Ask now** (each only helps from the day it's in place):

1. **Can a Jira admin add a *Work Started* automation rule?** "When an issue
   moves into an In Progress-category status and *Work Started* is empty,
   set it to now." Unlocks cycle time, aging's distribution and a correct
   CFD for all work finished after that day (§8.3).
2. **Can the export come from a saved filter** with a fixed column set and
   a longer finished-work window, e.g.
   `project = BSD AND (resolved >= -12M OR statusCategory != Done)`?
   More history for `sim` samples, `history` and `cfd`.

**At her first import:**

3. **Her Jira profile timezone** (the `timezone:` line). A wrong guess
   shifts times by hours, occasionally moving an event across midnight.

**While she edits her mapping** (the template's defaults are reasonable):

4. **What counts as started?** Default: In Progress category. If `Analysis`
   is real work it can be overridden to `started`, but its `started_at` would
   then come from `Status category changed`, which for a To Do-category
   status is the wrong moment. Recommendation: keep the default.
5. **Who gets credit for a done issue:** `Assignee` or `Custom field (Dev)`
   — or forecast with `-whole-team` and skip the question (§8.2).
6. **What is a "project"** for `count`/`history`/`sim backtest`: the parent
   epic (template default) or the fix version?
7. **Sub-tasks:** count them as throughput (template default) or exclude?
