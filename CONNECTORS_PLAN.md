# Implementation plan: tracker connectors (Linear, Jira, …)

Status: **agreed design, implementation not started.** The decisions in §3
were settled with the user on 2026-10-08 except where a row says *open*;
§11 lists what is still open and which phase each item blocks. Nothing in
§11 blocks Phase 1 or Phase 2.

---

## 0. How to use this document

**Rules for whoever implements this** (same as `HISTORY_PLAN.md` and
`EXCLUSIONS_PLAN.md`):

1. **Work the phases in order.** Each commit must compile (`just build`) and
   pass `just test` on its own. Never start the next phase with the tree red.
2. **One commit per step as listed in each phase**, using the subject line
   given. A commit contains everything about one change (code, its tests, and
   the doc lines that change *only because of it*) and nothing else. End every
   commit message with the attribution trailer the session provides. This repo
   is a colocated `jj`/git checkout — use `jj` for commits.
3. **Write the tests described in each step.** They are required.
4. **Analysis numbers must not move.** `simulate`, `aging`, `cfd`, `counts`,
   `history` are already tracker-neutral; this plan touches only their *input
   struct field names*, never their arithmetic. Capture each analysis
   command's output on `testdata/sample-issues.csv` before Phase 1 and diff
   it after every step that touches the record.
5. **No backward-compatibility shims** (D5). The tool is pre-1.0 and under
   heavy development; there are no third-party CSVs or databases to protect.
   Rename hard, regenerate fixtures, and refuse outdated databases.
6. **Never put a credential in a file, a flag, a log line, or a test
   fixture.** Connectors read secrets from environment variables only.
7. **Do not re-open a §3 decision without checking with the user.**

---

## 1. Goal

Make `forecast` tracker-agnostic end to end, so that adding a connector for a
new project-management system (Jira is the first; GitHub Issues, Azure
DevOps, Shortcut are the obvious next ones) touches *one new package and one
new subcommand pair* and nothing else:

- **A neutral issue model whose vocabulary is ours**, not Linear's. Today
  `state_type`, `trashed`, `project_milestone_*`, `archived_at`, and the
  literal state strings `completed`/`canceled`/`duplicate`/`started` are
  Linear's words and Linear's semantics, tested for directly in `issues`,
  `counts`, and `cmd/forecast`.
- **A connector interface** on the ingest side (`list teams; fetch issues
  changed since a watermark for one team; report removals`) that one generic
  `syncer` drives, with concrete connectors instantiated from their own
  auth and mapping configuration.
- **An `issues.Store` interface on the read side**, so every analysis command
  loads issues through one seam whether they come from SQLite, a file, or
  stdin.
- **A Jira Cloud connector** as the second implementation — the one that
  proves the abstraction, because Jira disagrees with Linear on almost every
  point that matters here (§6). The first real target is a **Jira Service
  Management** project on the user's company instance (§6.1).
- **A store that can hold more than one tracker's issues** without identifier
  collisions, keyed on `(source, identifier)`.

**Non-goals.** Writing back to any tracker. Loading connectors as external
plugins. Replacing SQLite. Changing any analysis semantics. Jira Data
Center/Server in the first cut (designed-for; D10). Preserving existing
databases or CSV headers (D5).

---

## 2. Where Linear leaks today (survey)

The clean part first: **`simulate`, `aging`, `cfd`, `counts`, `history`, and
`issues.Filter` are already tracker-neutral.** They take plain structs and
`cmd/forecast` is the only converter. The HISTORY_PLAN work did the heavy
lifting; what's left is vocabulary, the ingest side, and the store.

| # | Where | What leaks | Fixed in |
|---|---|---|---|
| L1 | `issues/issue.go` `IsCompleted`/`IsCanceled`/`IsInProgress` | Test `StateType` against Linear's literal `"completed"`, `"canceled"`, `"duplicate"`, `"started"`. | P1 |
| L2 | `counts/aggregate.go` `isTerminalState`; `cmd/forecast/count.go` `toCountsIssues` | Hard-code the same three terminal strings; `counts.Issue` carries a raw `StateType`. | P1 |
| L3 | `aging.Issue`/`aging.Item` `StateType`+`StateName`; `cfd.Issue.StateType` | Raw Linear strings carried for display (`formatState` prints `Done (completed)`); `cfd.Issue.StateType` is never read at all. | P1 |
| L4 | `issues.Issue` fields `ArchivedAt`, `AutoArchivedAt`, `AddedToProjectAt` | Linear-only; written by sync, read by nothing (confirmed by grep; `DATA_REQUIREMENTS.md` says so too). | P1 |
| L5 | `issues.Issue` fields `ProjectMilestoneID/Name`, `StateType`/`StateName` | Linear's compound nouns as the neutral column names (`project_milestone_name`, `state_type`). | P1 |
| L6 | `internal/sqlite/store.go` | Single-tracker assumption: PK `identifier`; watermark keyed on `team_key` alone; no schema version; comment "replicates Linear's own data". Two trackers can both have `ENG-1`. | P1 |
| L7 | `internal/linear/client.go` `Fetch` returns `trashed []string`; `internal/syncer` deletes them, logs `trashed-skipped` | "Trashed" is Linear's soft-delete. Jira hard-deletes (an issue simply vanishes from search); GitHub deletes or *transfers*. The neutral concept is a *removal*, and some trackers can't report it incrementally at all. | P2 |
| L8 | `internal/syncer/syncer.go` | Imports `internal/linear` directly; its `client` test seam names `linear.Team`; `Options.Teams` is `linear.TeamKeyList`. | P2 |
| L9 | `internal/linear/keylist.go` `TeamKeyList` | Is the `flag.Value` behind **every** command's `-teams` flag and `blendingTeamsWarning` — so `cmd/forecast/aging.go`, `cfd.go`, `count.go`, `history.go` all import a Linear package to parse a team list. | P2 |
| L10 | `linear.GetAPIKey` / `LINEAR_API_KEY` | Auth is a package-level function reading one env var; nothing to instantiate a connector from. | P2 |
| L11 | `cmd/forecast/main.go` `linearCommands`; `cmd/forecast/linear.go` | The only ingest command group; `-db`/`-teams`/`-all-teams`/`-full-reload` wiring is inline, not shareable with a second connector. | P2 |
| L12 | `cmd/forecast/source.go` `loadIssues` | Dispatches on file extension to two concrete loaders; no interface to add a third or to test commands against a fake. | P2 |
| L13 | `cmd/forecast/manifest.go` | `data.db` key and `IssueRecord` mirror the old Linear-shaped selection. | P1 |
| L14 | `scripts/check-engineer-data.sh` | Raw SQL against `linear.db` with no `source` column. | P1 |
| L15 | `README.md` (22 mentions), `CLAUDE.md`, `DATA_REQUIREMENTS.md` ("field names mirroring Linear's own GraphQL field names") | Docs describe a Linear tool with a file escape hatch. | P4 |

`history.ExitType` uses the strings `"completed"`/`"canceled"` internally;
those are *our* words and stay (they'll match the new `issues.Status`
constants, which is tidy but not required).

---

## 3. Decisions

| # | Decision | Reasoning |
|---|---|---|
| D1 | **Three words, kept distinct: a *connector* writes, a *store* is read, a *tracker* is the external system.** A connector fetches a tracker's issues into SQLite; an `issues.Store` is whatever `-input` loads (`.db`/`.csv`/`.json`/stdin). | "Source" is already overloaded in the code (`source.go`, "source-agnostic") and becomes the `source` *column* (the connector kind). Giving each side its own noun keeps every doc sentence unambiguous. |
| D2 | **Keep `team` / `project` / `milestone` as the tool's own vocabulary.** Connectors map onto them with explicit, configurable mappings (§4.1). | These are delivery-forecasting words: *team* = the group whose throughput is sampled and the unit of sync; *project* = the deliverable whose completion is forecast; *milestone* = a sub-scope. Renaming them would churn every flag and doc for a cosmetic win and be wrong for the next tracker anyway. The one collision — Jira's "project" is our "team" — is handled by always saying "Jira project key" in Jira-facing help text. |
| D3 | **`state_type` becomes `status`, a four-value enum we own** (`backlog`, `started`, `completed`, `canceled`, plus `""` unknown); `state_name` becomes `status_name` (the tracker's own label); `project_milestone_*` becomes `milestone_*`. Connectors do the mapping; nothing downstream ever sees a tracker's raw state vocabulary. | L1–L3, L5. Four values are exactly what the analyses distinguish. Linear's `triage`/`backlog`/`unstarted` collapse to `backlog`; `duplicate` folds into `canceled` and survives in `resolution` (D14). |
| D4 | **Timestamps stay primary** (D7 of `HISTORY_PLAN.md` holds): `IsCompleted` is `CompletedAt` non-zero *or* `Status == completed`, etc. Connectors must produce status and timestamps that agree; a conformance test enforces it. | File input without `status` keeps working. A connector that can only derive one of the two still produces a consistent record. |
| D5 | **No backward compatibility.** Column renames are hard renames with no aliases; the three Linear-only timestamps are deleted; `testdata/sample-issues.csv` is regenerated with the new headers; an outdated SQLite database is **refused** with an error telling the user to delete it and re-sync. `PRAGMA user_version` exists only to make that refusal possible. | Pre-1.0, heavy development, no external data to protect (user, 2026-10-08). Alias tables and auto-migration would be code that exists to serve nobody. |
| D6 | **Add `source`, `url`, `issue_type`, `parent_identifier`, `resolution`.** Primary key becomes `(source, identifier)`; one database **may** hold several trackers' issues. | `source` makes a multi-tracker store safe and lets `check` give tracker-specific hints; `url` is display value (aging HTML); `issue_type`, `parent_identifier`, `resolution` are the raw facts behind mapping policy (D14). |
| D7 | **Removal is a tombstone list plus reconcile-by-diff, not a tracker flag.** `FetchResult{Issues, Removed []string, Complete bool}`. `syncer` deletes `Removed`; when `Complete` is true (a full fetch) it also deletes everything else it holds for that `(source, team)`; a connector may optionally implement a key-only `ListIdentifiers` so `-reconcile` can prune on an incremental sync too. | L7. Linear can tombstone incrementally (`trashed`). Jira cannot — a deleted issue is just absent — so the honest options are "full fetch prunes" and "an explicit cheap reconcile pass". Both fall out of one `Complete` bit and one optional interface. |
| D8 | **Verb-first CLI with the tracker as a subcommand**, mirroring `forecast sim <sub>`: `forecast sync linear|jira …`, `forecast teams linear|jira`. The `forecast linear` group is removed. Shared sync flags come from one helper; each tracker subcommand adds its own typed flags. | Consistency with the rest of the command tree (user preference). Per-tracker subcommands keep Go's `flag` package doing the work; a generic `-source jira -opt k=v` bag would be worse in every way. |
| D9 | **Outdated database ⇒ refuse.** Both `Open` (sync) and `OpenExisting` (read) check `user_version`; a mismatch is an error: "database schema is v%d, this build needs v%d; delete the file and run `forecast sync <tracker>` again". | User preference; simplest possible policy; the store is a replica so nothing is lost. |
| D10 | **Jira Cloud first, via `/rest/api/3/search/jql` and Basic auth (email + API token).** Data Center (Bearer PAT, legacy `/rest/api/2/search` with `startAt`) is a documented follow-up behind one small transport interface. | The target instance is `<company>.atlassian.net`, i.e. Cloud (confirmed). Jira Service Management projects live on the same platform and API (§6.1). |
| D11 | **Jira's `started_at` is derived from the changelog**: the earliest transition into a status that counts as started (default: category *In Progress*). Completed/canceled timestamps come from `resolutiondate`, falling back to the last transition into a *Done*-category status; completed vs canceled is decided by resolution name, falling back to status name — both lists configurable. | Jira has no `startedAt`. Everything `aging`, `cfd`, and `history` do depends on it. The changelog is the only truthful source and costs one `expand=changelog` on the search call (plus a per-issue follow-up only for issues with >100 history entries). |
| D12 | **Jira mapping is configuration, not code.** One `Mapping` struct (§6.3) holds every rule; every field is a flag on `forecast sync jira` and therefore a key in a `-config jira.yaml`. Defaults derive from Jira's own `statusCategory` and never from one team's usage style. A policy change is "edit the YAML, run `sync jira -full-reload`". | The user does not want the connector to change every time the team's Jira habits change. Rules-as-data with Jira's own normalization as the default is the cheapest way to get that; the mapping is unit-tested against fixture JSON with no HTTP. |
| D13 | **`issues.Store` is the read-side interface**: `Load(ctx) ([]issues.Issue, error)`, implemented by `sqlite.Store` and a new `issues.FileStore{Path, Format}` (files and stdin). `cmd/forecast` gets `openStore(path, stdinFormat) (issues.Store, error)` in place of `loadIssues`'s inline dispatch. **Not** one implementation per tracker — normalization happens at ingest (D14), so every store yields the same records. | User decision (2026-10-08) after weighing normalize-at-ingest against per-tracker schemas. One seam, two implementations, and a fake for command tests. |
| D14 | **Normalize at ingest, but keep the raw facts each policy depends on next to the normalized fields.** `status`+`status_name`, `canceled`+`resolution`, `issue_type`, `parent_identifier`. Include/exclude decisions can then move to read-side filters later without a re-sync; only changelog-derived `started_at` needs a re-sync when its rule changes. A neutral `status_transitions` table that would remove even that limit is a Phase 5 follow-up, not a Phase 1 item. | The common in-memory type exists in every design; per-tracker schemas would only move the mapping to load time and multiply the places it lives (every store *and* the file reader). The expensive derivations need the API anyway. A normalized store stays inspectable with one set of column names, and `check` can report data quality without mapping first. |

---

## 4. The neutral model (normative)

### 4.1 Vocabulary and per-tracker mapping

| Term | Meaning in `forecast` | Linear | Jira / JSM (default; configurable) | GitHub Issues (sanity check only) |
|---|---|---|---|---|
| tracker | the external system a connector talks to | Linear | Jira Cloud (Software or Service Management) | GitHub |
| team | unit whose issues share an identifier prefix; the unit of sync and of throughput sampling (`-teams`) | team (`ENG`) | project (`ENG`) | repository |
| project | the deliverable whose completion is forecast (`-project`) | project | parent Epic (or Fix Version / Component / `label:<prefix>` / none) | milestone |
| milestone | a sub-scope within a project (`-milestone`) | project milestone | Fix Version (or Sprint / none) | — |
| assignee | the person credited with a completion (`sim`'s pool key) | `assignee.name` | `assignee.displayName` | `assignee.login` |
| status | normalized lifecycle phase (§4.2) | `state.type` | `status.statusCategory.key` + `resolution.name` | `state` + `state_reason` |
| status_name | the tracker's own status label, display + raw fact | `state.name` | `status.name` | — |
| resolution | the tracker's close reason, `""` when none | `"duplicate"` when `state.type` was duplicate, else `""` | `resolution.name` | `state_reason` |
| issue_type | the tracker's type label | `""` | `issuetype.name` (`Story`, `Bug`, `Sub-task`, `Epic`; JSM: `Incident`, `Service Request`, …) | `""` / `pull_request` |
| parent_identifier | the containing issue, if any | `parent.identifier` | `parent.key` | — |
| removed | an issue the tracker no longer has | `trashed == true` | absent from search (hard delete) | deleted / transferred |

### 4.2 `issues.Status`

```go
// Status is the normalized lifecycle phase every connector maps its tracker's
// workflow onto. Timestamps remain the primary signal (IsCompleted prefers
// CompletedAt); Status is the fallback and the display grouping.
type Status string

const (
	StatusUnknown   Status = ""          // not provided (file input may omit it)
	StatusBacklog   Status = "backlog"   // not started.   Linear: triage/backlog/unstarted. Jira: category "new"
	StatusStarted   Status = "started"   // in progress.   Linear: started.                  Jira: category "indeterminate"
	StatusCompleted Status = "completed" // finished, delivered. Linear: completed.          Jira: category "done", non-canceled resolution
	StatusCanceled  Status = "canceled"  // finished, not delivered. Linear: canceled/duplicate. Jira: category "done", canceled resolution/status
)

// ParseStatus accepts exactly the four values above (case-insensitive).
// Anything else returns StatusUnknown and false, never an error — a file's
// status column is advisory (D4).
func ParseStatus(s string) (Status, bool)
```

`IsCompleted`/`IsCanceled`/`IsTerminal`/`IsInProgress` keep their exact
semantics, with `Status` constants replacing the string literals.

### 4.3 `issues.Issue` (v2)

| Field | JSON/CSV/column | Notes |
|---|---|---|
| `Source` | `source` | connector kind (`linear`, `jira`); `""` for file input. Part of the primary key. **new** |
| `Identifier` | `identifier` | tracker's human key (`ENG-123`) |
| `Title` | `title` | |
| `URL` | `url` | link to the issue; display only. **new** |
| `Assignee` | `assignee` | |
| `TeamKey`, `TeamName` | `team_key`, `team_name` | |
| `ProjectID`, `ProjectName` | `project_id`, `project_name` | ids kept: they disambiguate same-named projects |
| `MilestoneID`, `MilestoneName` | `milestone_id`, `milestone_name` | **renamed** from `project_milestone_*` |
| `IssueType` | `issue_type` | **new**; `""` when the tracker has no notion |
| `ParentIdentifier` | `parent_identifier` | **new**; `""` when top-level |
| `Status` | `status` | **renamed** from `state_type`; type `Status` |
| `StatusName` | `status_name` | **renamed** from `state_name` |
| `Resolution` | `resolution` | **new**; tracker's close reason, `""` when none |
| `CreatedAt`, `StartedAt`, `CompletedAt`, `CanceledAt`, `UpdatedAt` | unchanged | |
| ~~`ArchivedAt`, `AutoArchivedAt`, `AddedToProjectAt`~~ | | **deleted** (L4); the readers no longer know these columns |

`issues.Filter` is unchanged in behavior (`Milestone` now matches
`MilestoneName`). The analysis input structs change only in field *names*:
`counts.Issue.StateType string` → `counts.Issue.Terminal bool` (set by
`cmd/forecast` via `IsTerminal()`, removing the last hard-coded state list
in a public package); `aging.Issue`/`aging.Item` `StateType`/`StateName` →
`Status`/`StatusName` (both `string`, display only; `formatState` prints
`Done (completed)` exactly as before); `cfd.Issue.StateType` is deleted.

---

## 5. Interfaces

### 5.1 Read side — `issues.Store` (D13)

```go
package issues

// Store is anything an analysis command can load issues from. Load returns
// every issue unfiltered; callers apply Filter afterwards, so every store
// gets identical scoping semantics.
type Store interface {
	Load(ctx context.Context) ([]Issue, error)
}

// FileStore loads a CSV or JSON file (Path), or stdin when Path is "-", in
// which case Format ("csv"/"json") is required. It wraps ReadFile/ReadStream.
type FileStore struct {
	Path   string
	Format string
}
```

`*sqlite.Store` gains `Load` (an alias of today's `AllIssues`). In
`cmd/forecast`, `openStore(path, stdinFormat string) (issues.Store, func() error, error)`
replaces `loadIssues`'s inline dispatch (the second return closes the SQLite
handle; `FileStore` returns a no-op). Command tests that today write a temp
CSV can instead pass a `fakeStore{issues}`.

### 5.2 Ingest side — `internal/connector`

```go
package connector

// Team is a syncable scope: the unit whose issues share an identifier prefix.
type Team struct{ Key, Name string }

type FetchRequest struct {
	TeamKey      string
	UpdatedSince time.Time // zero means a full fetch
}

type FetchResult struct {
	Issues  []issues.Issue
	// Removed lists identifiers the tracker reports as deleted/trashed since
	// UpdatedSince. Trackers without tombstones leave it empty.
	Removed []string
	// Complete is true when Issues is the team's entire live set (always true
	// for a full fetch), so the caller may delete anything else it holds for
	// this team.
	Complete bool
}

// Connector is what every tracker integration implements. Constructors are
// per-package and take that tracker's own Config (auth, URL, Mapping).
type Connector interface {
	Kind() string // "linear", "jira" — stored in issues.Issue.Source
	ListTeams(ctx context.Context) ([]Team, error)
	Fetch(ctx context.Context, req FetchRequest) (FetchResult, error)
}

// Lister is an optional capability: a cheap key-only listing used to prune
// removed issues during an incremental sync (-reconcile).
type Lister interface {
	ListIdentifiers(ctx context.Context, teamKey string) ([]string, error)
}
```

Every `issues.Issue` a connector returns must have `Source == Kind()`,
`TeamKey == req.TeamKey`, a non-empty `Identifier`, and status/timestamp
agreement (`Status == completed` ⇒ `CompletedAt` set when the tracker records
one; `CompletedAt` and `CanceledAt` never both set; `Status == canceled` ⇒
`CanceledAt` set when known). `internal/connector/connectortest.Conformance(t, c, fixtures)`
asserts these against any connector so the third one doesn't rediscover them.

### 5.3 `internal/syncer` (generic)

`syncer.Run(ctx, c connector.Connector, store *sqlite.Store, opts Options)`;
`Options{Teams []string, AllTeams, FullReload, Reconcile bool}`. Per team:
watermark from `store.LatestUpdatedAt(ctx, c.Kind(), team)` (zero if new or
`FullReload`); `Fetch`; `Upsert`; `Delete(Removed)`; if `Complete`, delete the
store's `(source, team)` identifiers not in the result; else if `Reconcile`
and `c` is a `Lister`, do the same against `ListIdentifiers`. Logs say
`removed`, never `trashed`. The existing `stubClient` tests port 1:1; add
tests for `Complete` pruning and `Reconcile` with and without a `Lister`.

### 5.4 `internal/linear` becomes a connector

`linear.New(Config{APIKey string}) *Client`; `Kind() = "linear"`;
`ListTeams` unchanged; `Fetch` wraps today's loop, sets `Source`, `URL` (the
issue node's `url`), `ParentIdentifier` (`parent { identifier }`), maps
`state.type` → `Status` with an explicit switch (duplicate → `canceled` with
`Resolution = "duplicate"`), returns `trashed` identifiers as `Removed`, and
sets `Complete = since.IsZero()`. `GetAPIKey` moves to `cmd/forecast` (reading
`LINEAR_API_KEY` is CLI policy). `TeamKeyList` moves to `cmd/forecast` as
`teamKeyList` (L9).

---

## 6. The Jira Cloud connector (`internal/jira`)

### 6.1 Jira Service Management — what's different

JSM runs on the same Jira Cloud platform: its projects are ordinary Jira
projects with `projectTypeKey = "service_desk"`, and their issues come back
from the same `/rest/api/3/search/jql` with the same `status`,
`statusCategory`, `resolution`, `changelog`, and `assignee` fields. The
separate `/rest/servicedeskapi/` (queues, SLAs, customer requests, portal
users) is not needed. Differences that matter here:

- **Issue types are request types** (`Incident`, `Service Request`,
  `Problem`, `Change`, …). Epics and sprints are usually absent, so
  `project_name` is often `""` and `(No Project)` is the normal bucket.
  `sim`, `aging`, `cfd`, and `history` without `-project` are unaffected;
  `count -milestones` and `sim backtest -project` are less useful until a
  `-project-from` rule fits how the team groups work.
- **Waiting statuses** (`Waiting for customer`, `Pending`, …) usually sit in
  the *In Progress* category, so they count as started. Right for
  throughput; inflates cycle time. A `-paused-statuses` rule that subtracts
  that time is a Phase 5 follow-up (§9), not in the first cut.
- **Permissions.** The API account needs an agent license or *Browse
  Projects* on the JSM project to see its issues. `forecast teams jira`
  listing the project proves the first; a non-empty first page proves the
  second.

### 6.2 Transport, auth, teams, fetch

**Auth.** `Config{BaseURL, Email, APIToken string, Mapping Mapping}`; Basic
auth. The CLI reads `JIRA_URL` (or `-url`), `JIRA_EMAIL`, `JIRA_API_TOKEN`
from the environment and never echoes them. Transport is one tiny interface
(`do(ctx, method, path, body)`) so Data Center's Bearer-PAT + legacy
pagination is a second implementation later (D10).

**Teams.** `GET /rest/api/3/project/search?startAt=…&maxResults=50` →
`{key, name, projectTypeKey}`; the type is logged so a JSM project is
recognizable in `teams` output.

**Fetch.** `POST /rest/api/3/search/jql` with
`jql = project = "KEY" [AND updated >= "<since − 1 minute, yyyy-MM-dd HH:mm>"] [AND (<Mapping.ExtraJQL>)] ORDER BY updated ASC`,
`maxResults = 100`, `nextPageToken` pagination, `expand = ["changelog"]`,
`fields = [summary, status, resolution, resolutiondate, created, updated,
assignee, project, issuetype, parent, fixVersions, components, labels]` plus
the sprint custom field when `MilestoneFrom = sprint`. JQL's `updated` has
minute precision, hence the one-minute back-off; upsert is idempotent so the
overlap is harmless. If `changelog.total > len(changelog.histories)` (the
inline changelog caps at 100 entries), page
`GET /rest/api/3/issue/{key}/changelog` for that issue. Honor `429
Retry-After`; retry 5xx with backoff.

### 6.3 `Mapping` — the rules, as data (D12)

```go
// Mapping holds every rule that turns a Jira issue into an issues.Issue.
// Each field is a flag on `forecast sync jira` and therefore a key in a
// -config YAML file. Zero values mean "use the Jira-category default".
type Mapping struct {
	// StartedStatuses: status names that count as started. Empty = any
	// status whose category is "indeterminate".
	StartedStatuses []string
	// CanceledResolutions: resolution names that mean canceled rather than
	// completed. Default: Won't Do, Won't Fix, Duplicate, Cannot Reproduce,
	// Declined, Obsolete.
	CanceledResolutions []string
	// CanceledStatuses: done-category status names that mean canceled
	// regardless of resolution. Default: none.
	CanceledStatuses []string
	// ExcludeTypes: issue type names dropped at ingest. Default: Epic.
	ExcludeTypes []string
	// ProjectFrom: epic | fixversion | component | label:<prefix> | none.
	// Default: epic (walk `parent` up to two levels to an issue of type Epic).
	ProjectFrom string
	// MilestoneFrom: fixversion | sprint | none. Default: fixversion
	// (several → the earliest by release date, with a warning).
	MilestoneFrom string
	// ExtraJQL is ANDed into every search, e.g. `issuetype != Sub-task`.
	ExtraJQL string
}
```

Flags: `-started-statuses`, `-canceled-resolutions`, `-canceled-statuses`,
`-exclude-types`, `-project-from`, `-milestone-from`, `-jql`. A worked
`testdata/jira-sync.example.yaml` ships with the README section. `Mapping`
is applied by a pure function `mapIssue(raw jiraIssue, m Mapping) (issues.Issue, bool)`
unit-tested against fixture JSON with no HTTP involved. Changing a rule is
"edit the YAML, run `forecast sync jira -full-reload`".

### 6.4 Status and timestamps (D11)

| Jira facts | `status` | `started_at` | `completed_at` / `canceled_at` |
|---|---|---|---|
| category `new` | `backlog` | earliest transition into a started status, if any (a re-opened issue keeps it) | — |
| category `indeterminate` | `started` | same | — |
| category `done`, not canceled per `Mapping` | `completed` | same | `completed_at` = `resolutiondate`, else last transition into a done-category status |
| category `done`, canceled per `Mapping` | `canceled` | same | `canceled_at` = same rule |

Always stored: `status_name = status.name`, `resolution = resolution.name`
(`""` when unresolved), `issue_type = issuetype.name`,
`parent_identifier = parent.key`, `url = <BaseURL>/browse/<key>`.

An issue that is done but never passed through a started status keeps a zero
`started_at` — the case `forecast check` already reports for `aging`. When
`source == "jira"`, `check` appends the hint "they never passed through an
In Progress status; check the workflow or set -started-statuses".

### 6.5 Removal (D7)

No tombstones. A full fetch returns `Complete = true`; `ListIdentifiers` is
a `fields: ["key"]` JQL pass for `-reconcile`.

### 6.6 Tests

`httptest.Server` fixtures committed as JSON under `internal/jira/testdata/`
(scrubbed of anything identifying; no real instance data in the repo
without the user's say-so): two-page `nextPageToken` pagination;
changelog-derived `started_at` including a re-opened issue; resolution →
canceled vs completed; done-with-no-resolution fallback; truncated-changelog
follow-up; epic resolution through a sub-task; each `Mapping` field; `429`
retry; `connectortest.Conformance`.

---

## 7. Store (`internal/sqlite`), schema v2

```sql
PRAGMA user_version = 2;
CREATE TABLE IF NOT EXISTS issues (
    source            TEXT NOT NULL DEFAULT '',
    identifier        TEXT NOT NULL,
    title             TEXT NOT NULL DEFAULT '',
    url               TEXT,
    assignee          TEXT,
    team_key          TEXT NOT NULL DEFAULT '',
    team_name         TEXT NOT NULL DEFAULT '',
    project_id        TEXT,
    project_name      TEXT,
    milestone_id      TEXT,
    milestone_name    TEXT,
    issue_type        TEXT,
    parent_identifier TEXT,
    status            TEXT NOT NULL DEFAULT '',
    status_name       TEXT NOT NULL DEFAULT '',
    resolution        TEXT,
    created_at        DATETIME,
    started_at        DATETIME,
    completed_at      DATETIME,
    canceled_at       DATETIME,
    updated_at        DATETIME,
    PRIMARY KEY (source, identifier)
);
CREATE INDEX IF NOT EXISTS idx_issues_source_team_updated ON issues (source, team_key, updated_at);
CREATE INDEX IF NOT EXISTS idx_issues_completed_at        ON issues (completed_at);
```

Methods: `Upsert`, `Delete(source, ids…)`, `Load`/`AllIssues`,
`LatestUpdatedAt(source, team)`, `DistinctTeams(source)`,
`Identifiers(source, team)`. Version policy per D9: `Open` on a brand-new
file sets `user_version = 2`; `Open` and `OpenExisting` on a file whose
`user_version != 2` (legacy databases read as `0`) return an error naming
both versions and telling the user to delete the file and re-run
`forecast sync <tracker>`.

---

## 8. CLI (D8)

```
forecast sync linear  -db <path> [-teams k1,k2 | -all-teams] [-full-reload] [-reconcile] [-config f.yaml]
forecast sync jira    -db <path> [-url …] [same shared flags] [-started-statuses …] [-canceled-resolutions …]
                      [-canceled-statuses …] [-exclude-types …] [-project-from …] [-milestone-from …] [-jql …]
forecast teams linear
forecast teams jira   [-url …]
```

`sync` and `teams` are command groups exactly like `sim`: `runGroup("sync",
syncCommands, …)`. `addSyncFlags(fs)` registers `-db`, `-teams`,
`-all-teams`, `-full-reload`, `-reconcile`, `-config`; each tracker
subcommand adds its own. The usage screen gains:

```
  sync <tracker>         Sync issues from a tracker into a SQLite database.
    linear               …
    jira                 …
  teams <tracker>        List the teams (Jira: projects) a tracker account can see.
```

Jira-facing `-teams` help text says "Jira project keys". Secrets:
`LINEAR_API_KEY`; `JIRA_URL`, `JIRA_EMAIL`, `JIRA_API_TOKEN`. The deprecated
`-db` alias on *read* commands is untouched by this plan.

---

## 9. Phases

### Phase 1 — the neutral model (no connector work yet)

0. Capture `aging`/`count`/`cfd`/`history` output on the sample CSV into the
   scratchpad as the before-image for rule 4. (Not a commit.)
1. `issues`: add `Status` + constants + `ParseStatus`; rename
   `StateType`→`Status`, `StateName`→`StatusName`; `Is*` helpers use the
   constants; readers know `status`/`status_name` only. Tests: each `Is*`
   helper per status value, `ParseStatus` table, reader rejects nothing but
   maps unknown status strings to `StatusUnknown`.
   *Subject:* `Replace state_type with a neutral issues.Status enum`
2. `issues`: add `Source`, `URL`, `IssueType`, `ParentIdentifier`,
   `Resolution`; rename `ProjectMilestone*`→`Milestone*`; delete the three
   Linear-only timestamps and their reader entries; regenerate
   `testdata/sample-issues.csv` with the new headers (same 30 issues, same
   values). Tests follow. *Subject:* `Add source, url, type, parent, resolution to issues.Issue`
3. `counts.Issue.StateType`→`Terminal bool`; `aging` field renames; delete
   `cfd.Issue.StateType`; `cmd/forecast` converters follow. Diff the
   before-image: byte-identical except `aging -format json`'s key rename.
   *Subject:* `Take tracker vocabulary out of the analysis input structs`
4. `sqlite`: schema v2, `user_version` refusal, per-source methods;
   `linear.toIssue` sets `Source`/`Status`/`Resolution`/`URL`/`ParentIdentifier`;
   `syncer` passes the source through. Tests: legacy db refused with the
   expected message, two sources with the same identifier, per-source
   watermark. *Subject:* `Version the SQLite schema and key issues by (source, identifier)`
5. `scripts/check-engineer-data.sh` gains a `source` filter; manifest
   `IssueRecord` follows the renames and `data.db`→`data.input`
   (`schema_version` 3); `DATA_REQUIREMENTS.md` field table and `CHANGELOG.md`
   updated for everything above. *Subject:* `Document the v2 issue record`

**Acceptance:** `just test` green; `forecast linear sync` on an old
`linear.db` is refused with instructions and succeeds on a fresh file; every
analysis command matches the before-image.

### Phase 2 — the two interfaces (Linear only, no behavior change)

1. `issues.Store` + `issues.FileStore`; `sqlite.Store.Load`; `openStore` in
   `cmd/forecast` replaces `loadIssues`; one command test switched to a
   `fakeStore` to prove the seam. *Subject:* `Load issues through an issues.Store interface`
2. `internal/connector`: interface, `FetchResult`, `connectortest`.
   *Subject:* `Add the connector interface and conformance tests`
3. `syncer` generic per §5.3; `linear.Client` implements
   `connector.Connector`; `GetAPIKey` and `TeamKeyList` move to the CLI.
   *Subject:* `Drive sync through connector.Connector`
4. `forecast sync linear` / `forecast teams linear` via `addSyncFlags`;
   `forecast linear` removed; README/CLAUDE.md command lists updated in the
   same commit. *Subject:* `Move ingest to forecast sync <tracker> and forecast teams <tracker>`

**Acceptance:** sync behaves as before under the new names; `internal/syncer`
and the analysis commands no longer import `internal/linear`;
`connectortest.Conformance` passes for Linear against a recorded fixture.

### Phase 3 — the Jira connector

0. **Discovery against the real instance** (needs `JIRA_URL`/`JIRA_EMAIL`/
   `JIRA_API_TOKEN` in the environment of the session; the token is never
   read by the agent, only by the binary). Run `forecast teams jira`; pull
   the first page of the JSM project with the changelog; record *to a
   scratchpad note, not the repo*: project type, issue types in use, status
   names with their categories, resolution names, whether `parent`/epics,
   fix versions, or sprints appear at all. This answers §11 Q4 empirically
   and sets the shipped `Mapping` defaults. (Not a commit.)
1. HTTP transport, auth, `ListTeams`, pagination. *Subject:* `Add a Jira Cloud client: auth, projects, paginated search`
2. `Mapping` + `mapIssue`: status/resolution → `Status`; changelog →
   `started_at`; resolution/transition → completed/canceled; `parent` → epic
   → project; fix version → milestone; `ExcludeTypes`. *Subject:* `Map Jira issues onto the neutral record`
3. `ListIdentifiers` + `Complete`; `forecast sync jira` / `forecast teams jira`
   with the §8 flags; `check` hint; `testdata/jira-sync.example.yaml`.
   *Subject:* `Add forecast sync jira and forecast teams jira`
4. End-to-end on the real JSM project: sync, run every analysis command,
   fix what the data breaks, write down in `CHANGELOG.md` any default that
   discovery changed. *Subject:* `Harden the Jira connector against real data`

### Phase 4 — docs

README: "Getting your data in" gets *From Linear* / *From Jira* / *Writing a
connector* (pointing at `internal/connector` and `connectortest`); the intro
stops saying "Linear" first; a short JSM note. `CLAUDE.md` architecture
section rewritten around tracker / connector / store and the new command
tree. `DATA_REQUIREMENTS.md` loses "mirroring Linear's field names" and
gains the new columns. `CHANGELOG.md` calls out the renames, the deleted
columns, the refused-database rule, and the command moves.

### Phase 5 — follow-ups (not in scope unless asked)

- `-paused-statuses` (JSM waiting states) subtracted from cycle time.
- A neutral `status_transitions` table so `started_at` policy can change
  without a re-sync (D14).
- A `sources` metadata table (`kind`, `base_url`, non-secret `Mapping`) so
  `forecast sync -db x.db` can re-sync without naming the tracker.
- Read-side `-exclude-types` on analysis commands (the column is already
  there per D14).
- Jira Data Center/Server transport.
- A GitHub Issues connector — the third implementation is where an
  abstraction stops being two special cases.

---

## 10. Compatibility summary

| Who | What happens |
|---|---|
| Existing `linear.db` files | Refused by every command with "delete the file and run `forecast sync linear`". |
| Existing CSV/JSON files | Old headers (`state_type`, `project_milestone_name`, …) are simply unknown columns and ignored; the file still loads but loses those fields. There are no such files in the wild (D5). |
| `forecast linear sync|teams` invocations | Become `forecast sync linear` / `forecast teams linear`. |
| Analysis output | Unchanged, except `aging -format json`'s `state_type`/`state_name` keys become `status`/`status_name`. |
| `-manifest` consumers | `schema_version` 3: `data.db`→`data.input`, `IssueRecord` field renames. |
| Library users (`aging`/`counts`/`cfd`) | Field renames in `aging.Issue`, `counts.Issue`, `cfd.Issue` only. |

---

## 11. Open questions

Nothing here blocks Phase 1 or Phase 2.

1. **(Blocks Phase 3, step 0.)** Credentials handoff: set `JIRA_URL`,
   `JIRA_EMAIL`, and `JIRA_API_TOKEN` in the environment the session's
   shell runs in. The agent never needs to read the token; only the
   `forecast` binary does. Do not paste it into chat or a file.
2. **(Checked in Phase 3, step 0.)** Does the API account have agent /
   Browse Projects access to the JSM project? `forecast teams jira` and a
   non-empty first page will show it; if not, the colleague grants it.
3. **(Decides Phase 3's shipped defaults; now informational, not blocking,
   because every rule is configurable per D12.)** How the team actually
   uses Jira: epics or not, fix versions or not, sprints or not, which
   statuses mean "being worked", which resolutions mean "didn't deliver".
   Phase 3 step 0 reads most of this off the instance; the colleague
   confirms the rest.
4. **(Phase 3, step 4.)** May scrubbed excerpts of the real instance's data
   be committed as test fixtures, or must fixtures be fully synthetic?
   Default if unanswered: synthetic.
5. **(Phase 5, not urgent.)** Is a `status_transitions` table worth
   building, or is "re-sync when the started rule changes" acceptable for
   the foreseeable future?
