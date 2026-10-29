# Import mapping files

`forecast import` converts a tracker's raw CSV export into a `forecast` data
source (`.db`, `.csv` or `.json`). A **mapping file** says how: which export
column fills which field, how the export's workflow maps onto `forecast`'s
four statuses, which lifecycle timestamps to keep, and which rows to drop.

```bash
forecast import -example list                       # embedded templates
forecast import -example jira-cloud-csv > team.yaml # start from one
forecast import -from export.csv -map team.yaml -dry-run   # report only
forecast import -from export.csv -map team.yaml -out team.db
```

The mapping is the reviewable record of how your team's tracker usage maps
onto the forecasting model. When your process changes, edit the mapping, not
the export and not the code.

Nothing is guessed. Every rule is checked against the export, and anything
that doesn't fit stops the import with a message naming the rule, the
column and example rows. Nothing is written when an import fails.

## Example

```yaml
version: 1                          # required; only 1 exists
source: jira                        # required; stored with every issue
format: csv                         # optional; only "csv"

time_format: "02/Jan/06 3:04 PM"    # required if any timestamp is mapped
timezone: America/Denver            # optional; default "Local"

columns:
  identifier:  Issue key            # a header name
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
  overrides:                        # keyed on the row's status_name
    won't do: canceled
    Wont Do: canceled
  resolution_overrides:             # keyed on the row's resolution
    Duplicate: canceled

timestamps:
  started_at:   {columns: [Status category changed], when: started}
  completed_at: {columns: [Resolved, Status category changed], when: completed}
  canceled_at:  {columns: [Resolved, Status category changed], when: canceled}

exclude:
  Issue Type: [Epic]

detect: [Issue key, Status category, Summary]
```

## Top-level keys

Unknown keys are an error at every level, so a typo can't silently do
nothing.

| Key | Required | Meaning |
|---|---|---|
| `version` | yes | `1` |
| `source` | yes | Lowercase letters, digits and dashes, e.g. `jira`. Stored with every imported issue. Importing into a `.db` replaces all of that source's issues. |
| `format` | no | `csv` (the only format) |
| `time_format` | when any `*_at` field is mapped | A [Go time layout](https://pkg.go.dev/time#pkg-constants), or a list of them tried in order. `2006-01-02 15:04` reads `2026-10-22 19:55`; `02/Jan/06 3:04 PM` reads `22/Oct/26 7:55 PM`. A layout must at least round-trip a date. |
| `timezone` | no | An IANA zone (`America/New_York`), `UTC` or `Local` (default). The zone the export's timestamps are written in. Jira uses the exporting user's profile zone. |
| `columns` | yes | Field → column rule. Must include `identifier`. |
| `status` | no | How each row's status is decided. |
| `timestamps` | no | Lifecycle timestamps kept only for some statuses. |
| `exclude` | no | Rows to drop. |
| `detect` | no | Headers that identify this kind of export (used by templates; see below). |

## Fields

The fields a mapping can fill are `forecast`'s own column names (see
[DATA_REQUIREMENTS.md](../DATA_REQUIREMENTS.md)): `identifier`, `title`,
`url`, `assignee`, `team_key`, `team_name`, `project_id`, `project_name`,
`milestone_id`, `milestone_name`, `issue_type`, `parent_identifier`,
`status`, `status_name`, `resolution`, `created_at`, `started_at`,
`completed_at`, `canceled_at`, `updated_at`. `source` is not a field; it
comes from the top-level key.

Mapping `status` under `columns` means the export already holds
`forecast`'s four status words (`backlog`, `started`, `completed`,
`canceled`); any other value is an error. It can't be combined with a
`status:` block.

## Column rules

| Rule | Meaning |
|---|---|
| `Issue key` | That column. It must exist and appear once in the header. |
| `{column: Fix versions, pick: first}` | For a column whose header repeats (Jira repeats `Sprint`, `Fix versions`, `Labels`, ...): `first` is the first non-empty one, `last` the last non-empty one, `join` all non-empty ones joined with `", "`. |
| `{columns: [Custom field (Dev), Assignee]}` | The first non-empty of these columns. Each must appear once. |
| `{value: BSD}` | That text on every row. |

Values are trimmed of surrounding whitespace. Fields ending in `_at` are
parsed with `time_format` in `timezone`; an empty cell stays unset, and a
non-empty value no layout reads is an error.

## Status

```yaml
status:
  column: Status category          # with values
  values: {To Do: backlog, In Progress: started, Done: completed}
  overrides: {Won't Do: canceled}               # needs columns.status_name
  resolution_overrides: {Duplicate: canceled}   # needs columns.resolution
```

Each status is one of `backlog`, `started`, `completed` or `canceled`. For
each row, the first rule that matches wins:

1. `overrides`, matched against the row's `status_name`;
2. `resolution_overrides`, matched against the row's `resolution` (an empty
   resolution never matches);
3. `values`, matched against `column`.

A row that matches nothing is an error, listing every unmatched value with
its row count. This is deliberate: a status your team adds later is handled
by its category (rule 3) with no mapping change, and only statuses that
break the category rule need an override. In Jira, cancellations such as
"Won't Do" sit in the Done category, which is why the template lists them.
Without a status rule, issues have an unknown status and commands rely on
timestamps alone.

## Timestamps that depend on status

```yaml
timestamps:
  completed_at: {columns: [Resolved, Status category changed], when: completed}
  canceled_at:  {columns: [Resolved, Status category changed], when: canceled}
```

`started_at`, `completed_at` and `canceled_at` may be mapped here instead of
under `columns` (not both). The first non-empty column is used, but only on
rows whose status is in `when` (one status or a list). This keeps a
cancellation's close date out of `completed_at`. It needs a status rule.

## Exclude

```yaml
exclude:
  Issue Type: [Epic, Sub-task]
```

A row is dropped when any listed column's value is in its list. Dropped rows
are counted in the report and not checked further.

## Matching

Header names and matched values are compared ignoring case and surrounding
whitespace (`won't do` matches `Won't Do`), and a byte order mark at the
start of the file is ignored. Nothing fuzzier. Two keys in one `values`,
`overrides` or `resolution_overrides` map that are equal by this rule are an
error. Keys that never occur in the data are fine; templates list common
spellings.

## Errors

The mapping is checked before the export is read; every message starts with
the YAML path at fault:

```
columns.state_type: unknown target; valid targets: identifier, title, …
columns.milestone_name.pick: must be first, last or join
status.overrides["won't do"]: "cancelled" is not a status; use backlog, started, completed or canceled
status.values: "Done" and "done" are the same value
timestamps.completed_at: also mapped under columns
time_format: required because columns.created_at is a timestamp
timezone: unknown time zone "Mountain"
```

Then the export's header is checked, and then every row. All problems are
reported together. Rows are numbered as a spreadsheet numbers them (the
header is row 1); a quoted cell can span several lines of the file.

```
columns.title: no column "Sumary" in the file (closest: "Summary")
columns.milestone_name: column "Fix versions" appears 2 times; add pick: first, last or join
identifier: empty on 2 rows (rows 5, 9)
identifier: "DEMO-3" appears on rows 4 and 12
created_at (column "Created"): 3 values match no time_format, e.g. row 7 "2026-10-22"
status: 2 rows match no rule: "Paused" (1, row 8), "On Hold" (1, row 11)
```

## The report

Every import prints what it read, dropped and imported; how each status
was decided (` (override)` and ` (resolution)` mark the override rules);
which column each timestamp came from; how many issues have each other
field; and the same per-command readiness check `forecast check` prints.

## Templates and detection

Templates are embedded in the binary: `forecast import -example list` names
them and `forecast import -example <name>` prints one. Each template's
`detect` headers identify its kind of export. If you pass a raw export to
`-input` by mistake and its header has every `detect` header of a template,
the error names the template and gives the `forecast import` commands to
run.

| Template | For |
|---|---|
| `jira-cloud-csv` | Jira Cloud "Export > CSV (all fields)" |
