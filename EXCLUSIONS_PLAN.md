# Implementation plan: calendar exclusions for `forecast sim`

Status: **ready to implement.** Every decision below was settled in design
review with the user; the reasoning is recorded so it does not get re-argued.
Work happens on the `exclusions-calendar` branch.

---

## 0. How to use this document

**Rules for whoever implements this:**

1. **Work the phases in order.** Each commit must compile (`just build`) and
   pass `just test` on its own. Never start the next step with the tree red.
2. **One commit per step as listed in each phase**, using the subject line
   given. Commits should have high internal cohesion and low external
   coupling: a commit contains everything about one change (code, its tests,
   and the doc lines that change *only because of it*) and nothing else. Do
   not squash steps; do not split a step across commits unless the plan says
   so. End every commit message with the attribution trailer your session
   provides.
3. **Write the tests described in each step.** They are required. Test names
   given here are the names to use.
4. **Do not relitigate §4.** If you believe a decision is wrong, stop and raise
   it with the user rather than silently doing something else.
5. **Do not do work assigned to a later step**, even when convenient.
6. If an acceptance criterion cannot be met as written, stop and report what
   blocks it rather than improvising a different design.
7. **Verify before you commit.** Every step ends with "Verify:" commands. Run
   them; paste nothing fictional. If one fails, fix the step.
8. Where this plan quotes a signature or a message string, use it exactly.
   Where it says "e.g.", you may choose wording.

**Repo conventions to follow** (they already exist; match them):

- Root-level packages (`simulate`, `issues`, `history`, …) are **pure and
  IO-free**: no file/network/db access, never call `time.Now()`. Time is
  passed in. `simulate` may import `internal/util` (it already does).
- `cmd/forecast` is the only layer that does IO and converts records.
- Every subcommand accepts `-config <file.yaml>` via `util.ApplyConfig`,
  called right after `fs.Parse`. Config values arrive through each flag's
  `Set`, so a custom `flag.Value` gets config values too. YAML sequences are
  joined with commas before `Set` (`engineers: [alice, bob]` → `"alice,bob"`).
- Date flags go through `util.ParseFlexibleStartDate` (start bounds) or
  `util.ParseFlexibleDate` (end bounds). Never `time.Parse` in a command.
- Flag presence is checked with `isFlagSet(cmd, name)` (`cmd/forecast/common.go`).
- cmd-layer tests use the existing `captureStdout(t, func())` helper
  (defined in `cmd/forecast/aging_test.go`, shared by the package) and write
  CSV fixtures to `t.TempDir()`; `simulate` tests use the existing
  `day(y, m, d)` / `at(eng, y, m, d)` / `constantPool` / `assertAll` helpers.
- Any smoke command against `testdata/sample-issues.csv` must pin
  `-sample-start 2025-01-01 -sample-end 2025-04-01`: the fixture's completions
  are all in early 2025 and the default "last 3 months" window is empty.
- `.gitignore` ignores `*.json` and `*.yaml`. Test fixtures therefore live as
  Go string constants in `_test.go` files, except the one committed example
  file in Phase 6, which gets an explicit negation.
- Comments: match the surrounding density. Public identifiers in `simulate`
  get doc comments; explain *why*, not *what*.

---

## 1. Goal

`forecast sim` lets the user say "nobody works on these dates" and "this
engineer doesn't work on these dates", and that statement applies wherever the
date lands:

- **In the sample window** → the day is dropped from that engineer's (or
  everyone's) throughput sample, so a holiday's zero is not mistaken for a
  normal zero-throughput day. (Exists today for single dates.)
- **In the forecast horizon** → the simulation contributes 0 completions for
  that engineer (or everyone) on that day. The day still counts toward the
  calendar: `-days 30` is still 30 calendar days and `sim days` still reports
  calendar days; the day is simply unproductive. (New.)

So that per-engineer future exclusions have someone to attach to, the user
can **name the engineers doing the work** independently of **whose history
forms the sample pool**.

## 2. What exists today (read before starting)

| Piece | Where | Behavior |
|---|---|---|
| `Exclusions{Global []string; Engineers map[string][]string}` | `simulate/pool.go` | Flat `YYYY-MM-DD` lists. Malformed dates silently skipped in `BuildPool`. |
| `BuildPool(records, exc, start, end, wholeTeam)` | `simulate/pool.go` | Drops excluded day-slots per engineer. `-whole-team` honors `global` only. |
| `-exclusions` flag | `cmd/forecast/common.go` (`addSimFlags`) | Default `exclusions.json`; silently loaded if present, silently empty if absent (`loadExclusions`). |
| Engine | `simulate/engine.go` | `SimulateItemsInDays(samples, numDailyDraws, days, …)` and three siblings. Pure draws; no calendar. |
| Dispatch | `simulate/dispatch.go` | `Params{Mode, Team, Engineers, Days, Items, Simulations, Workers, Seed, Progress}`; `ItemsInDays` / `DaysToComplete` switch on `Mode`. |
| Modes | `simulate/mode.go` | `ModeAnonymous` (`-engineers N`, draws from `pool.Combined`), `ModeFullTeam` (`-whole-team`), `ModeNamedTeam` (`-team a,b`, each draws own history). `ResolveMode`, `ModeLabel`, `ValidatePool`. |
| `-typical-engineers` | `cmd/forecast/common.go` (`loadPool`/`completedForPool`) | Filters whose completions enter the pool. Orthogonal to mode. |
| `-target-start-date` | `sim days` (default `today`), `sim probability` (default `tomorrow`); **absent from `sim items`** | Used only after simulation: `start.AddDate(0,0,days)`. |
| `-percentile` | `sim items` (`removedFlag` tombstone that always errors), `sim days` (working alias of `-confidence`) | Leftovers from the `-confidence` rename. |
| Manifest | `cmd/forecast/manifest.go` | `SchemaVersion: 1`; `Resolved.Team`; `Data.ExclusionsPath` + raw `Data.ExclusionsApplied`. |
| Backtest | `simulate/backtest.go` `RunBacktest` | Per replayed day calls `ItemsInDays` with `Days: daysToTarget`. |
| Trajectory | `cmd/forecast/sim.go` `printTrajectoryReport` | `sim days -items a,b,c`; all thresholds share one seed (invariant in `simulate.ComputeTrajectoryTable`). |

## 3. Definitions

- **Typical engineers** — whose completion history forms the sample pool.
  `-typical-engineers` (default: every assignee with a completion in the window).
- **Slot** — one daily draw in the simulation. Named or anonymous. Only a
  named slot can have per-engineer exclusions applied.
- **Exclusion** — a calendar date on which a scope (everyone, or one named
  engineer) does no work. One exclusion applies on both sides; where the
  date falls decides which side it affects.
- **Horizon** — the calendar range a forecast covers, day 0 = target start.
  `[target-start, target-start + days)` for `items`/`probability`; open-ended
  from target-start for `days`; `[replayDay, target-end]` per replayed day
  for `backtest`.
- **Anchor** — the local-midnight date a `Calendar` counts day indices from.

## 4. Decisions (all settled — do not reopen)

**D1. One unified calendar, not separate past/future sections.** Past and
future are disjoint, so one list of non-working dates per scope is
unambiguous. `exclusions.json` is a long-lived "company calendar + PTO" file,
not a per-run artifact.

**D2. Extend the schema backward-compatibly.** Keep `global` / `engineers`.
Each list entry is one of:

```jsonc
{
  "global": [
    "2025-12-25",                                                   // one day
    "2025-12-22/2026-01-02",                                        // inclusive range
    {"from": "2026-07-03", "to": "2026-07-06", "reason": "July 4th weekend"},
    {"date": "2026-11-26", "reason": "Thanksgiving"}
  ],
  "engineers": {
    "alice": ["2026-03-02/2026-03-13", {"date": "2026-04-10", "reason": "PTO"}],
    "bob":   ["2026-02-16"]
  }
}
```

Mixed string/object entries by design (hand-edit friendly; existing files stay
valid). `reason` is optional and recorded in the manifest. Every entry must
parse; malformed date, inverted range, unknown object key, missing `to`, a
range longer than 366 days, or an empty string is a **hard error** at load.

**D3. `-exclusions` has no default; when given, the file must exist.**
Default `""`. A missing file is an error, not an empty set.

**D4. `-engineers` accepts a count or a list of names.** `-engineers 3` → 3
anonymous slots. `-engineers alice,bob,carol` → 3 *named* slots, each still
drawing from the pooled `Combined` samples (they remain statistically
"equivalent engineers"), with per-engineer exclusions applied by name. A value
that parses as an integer is a count (must be > 0); anything else is a name
list (no empties, no duplicates, no all-digit names). A named slot need not
appear in the data — a new hire with no history is a fine equivalent engineer.

| Flag | Slots | Each slot draws from | Per-engineer exclusions |
|---|---|---|---|
| `-engineers 3` | 3 anonymous | `Combined` (typical engineers) | global only |
| `-engineers alice,bob,carol` | 3 named | `Combined` (typical engineers) | yes |
| `-whole-team` | 1 | summed whole-team series | global only |

**D4a. Remove `-team` outright (no tombstone).** Rationale, stronger reason
first: (1) *Statistical* — over a 3-month window each engineer's series is
~90 mostly-zero samples; drawing from one person's history is lumpy and makes
the forecast hypersensitive to which names were typed. (2) *Organizational* —
that false precision invites "what if we put Alice on it instead of Bob?",
comparisons the data can't bear. Heterogeneous teams remain expressible via
`-typical-engineers` and `-whole-team`. The tool has one user, so the flag is
deleted, not tombstoned.

**D4b. Remove `-percentile` from `sim` entirely (no tombstone).** Both the
`sim items` `removedFlag` and the `sim days` alias go, along with the
`removedFlag` type. `forecast aging -percentile` is a different, real flag and
**stays**.

**D5. Engine = slots + calendar.** One implementation each for items-in-days
and days-to-complete over a `[]Slot` and a `*Calendar`. An excluded day does
**not** consume an RNG draw. `Calendar` is rule-based and anchored to a real
date: explicit-date rules from `exclusions.json` plus a weekday mask reserved
for a future weekends flag (§6). `BuildPool` consumes the same `Calendar`
type (anchored at `-sample-start`) — one definition of "is X working on this
day" on both sides. Draw order changes, so **pinned seeds from earlier builds
will not reproduce**; same-build determinism (same seed, inputs,
`-goroutines`) is unchanged.

**D6. Every forecast is anchored to a real start date.** `sim items` gains
`-target-start-date` and `-target-end-date` with `sim probability`'s rule:
exactly one of `-days` / `-target-end-date`; end inclusive;
`effectiveDays = DayIndex(end, start) + 1`. `-target-start-date` defaults to
**`tomorrow`** on `items`, `days`, `probability` (today's completions are
already in the pool via `-sample-end now`). `sim days` moves from `today` to
`tomorrow`: every reported date shifts +1 — CHANGELOG. `sim items -days`
loses its default of 30 (mirrors `probability`) — CHANGELOG.

**D7. `-whole-team` ignores per-engineer exclusions on both sides**; warn once
if the file has any.

**D8. Warn on unmatched exclusion names.** A name under `engineers:` that is
neither an assignee in the pool nor a named slot → `logx.Warnf`. A name may
legitimately match only one side.

**D9. Backtest honors the calendar per replayed day.** At replay day *r* the
horizon is `[r, target-end]`; exclusions in that range apply as they would
have on that day.

**D10. `forecast check -exclusions <path>` validates the file.** Same flag
name as `sim`. Reports parse validity, names matching no assignee, and a
past/future summary. Does not judge window membership.

---

## 5. Phases

Branch first:

```bash
git checkout -b exclusions-calendar
```

(The plan document itself is the first commit on this branch.)

### Phase 1 — Removals

Pure deletions, done first so the refactor in Phase 3 touches less.

#### Step 1a — commit `Remove sim's -team mode`

`simulate/mode.go`
- Delete `ModeNamedTeam`. `Mode` keeps `ModeAnonymous`, `ModeFullTeam`.
- `func ResolveMode(engineersSet, wholeTeam bool) (Mode, error)`. Both set →
  `"-whole-team and -engineers are mutually exclusive"`; neither →
  `"one of -engineers or -whole-team must be specified"`.
- `func ModeLabel(mode Mode, engineers int) string` — drop the named case.
- `func ValidatePool(pool *SamplePool, mode Mode, requireProgress bool) error`
  — drop the named case and `team` param; update the doc comment.

`simulate/engine.go` — delete `SimulateItemsInDaysPerEngineer`,
`SimulateDaysToCompletePerEngineer`.
`simulate/pool.go` — delete `DrawFromEngineer` (keep `PerEngineer`; `Combined`
and the manifest still use it).
`simulate/dispatch.go` — delete `Team` from `Params` and the `ModeNamedTeam`
cases; fix the `Mode` field comment (`-engineers`/`-whole-team`).

`cmd/forecast/common.go` — delete `simFlags.Team` and its `fs.Var`; `-engineers`
usage → `"number of (equivalent) engineers; one of -engineers or -whole-team is required"`.
`cmd/forecast/sim.go`, `backtest.go` — remove every `sf.Team` argument
(`ResolveMode`, `ValidatePool`, `ModeLabel`, `Params`, `manifestInputs`,
`printTrajectoryReport`).
`cmd/forecast/manifest.go` — delete `Resolved.Team`, `manifestInputs.Team`,
and the `ModeNamedTeam` case in `modeName`.

Tests
- `simulate/mode_test.go`: drop named cases from `TestResolveMode`,
  `TestModeLabel`, `TestValidatePool`; add `{"engineers + whole-team conflict", true, true, …, wantErr}` if not present.
- `simulate/engine_test.go`: delete the two `…PerEngineer_ConstantPool` tests.
- `simulate/dispatch_test.go`: delete the "named team" cases.
- `internal/util/config_test.go` registers flags literally named `team` and
  `percentile` on its *own* FlagSet to test generic config behavior. **Leave
  it alone.**

Docs (only lines that exist because of `-team`)
- `README.md`: in "Choosing how to model the team" delete the `-team` bullet
  and the parenthetical about `-team` vs `-teams`; change the `sim items`
  example to `-engineers 2`; in "Config files" drop `-team` from the list-flags
  sentence.
- `CLAUDE.md`: in the `forecast sim` paragraph, change "Three sampling modes"
  to two and delete the `named -team a,b,c` clause; drop `-team` from the
  list-flags bullet under "Config files"; drop `team: [alice, bob]` from the
  example YAML.
- `CHANGELOG.md` → `## Unreleased` → `### Removed`: add
  `**`forecast sim -team`**` with a two-sentence version of D4a and the
  migration: use `-engineers <names>` (Phase 4) — write it as "replaced in a
  following change by named `-engineers`" for now; Phase 4b will reword.

Verify:
```bash
just build && just test
grep -rn "ModeNamedTeam\|sf\.Team\|DrawFromEngineer\|PerEngineer(" --include='*.go' . ; echo "expect no output above"
```

#### Step 1b — commit `Remove -percentile from sim items and sim days`

- `cmd/forecast/sim.go`: delete the `removedFlag` type and its doc comment,
  the `cmd.Var(removedFlag{…}, "percentile", …)` line in `cmdSimItems`, and
  the `cmd.Var(&confidences, "percentile", …)` alias line in `cmdSimDays`.
  Remove the now-unused `errors` import.
- `README.md`: delete the "`sim items` formerly took `-percentile` …" paragraph
  and the "(`-percentile` is an alias here)" parenthetical in the `sim days`
  flag table.
- `CLAUDE.md`: in the `forecast sim` paragraph, delete the sentences about the
  `-percentile` tombstone and the `sim days` alias.
- `CHANGELOG.md` → Unreleased → Removed: `**`sim items -percentile` (the
  always-erroring tombstone) and `sim days -percentile` (alias of
  `-confidence`).** Use `-confidence`. Note: `aging -percentile` is unrelated
  and unchanged.` Leave the historical v0.131.0 entry untouched.

Verify:
```bash
just build && just test
grep -n "percentile" cmd/forecast/sim.go cmd/forecast/common.go; echo "expect no output above"
go run ./cmd/forecast sim items -percentile 5 -input testdata/sample-issues.csv -whole-team -days 5 2>&1 | grep -q "flag provided but not defined" && echo OK
```

### Phase 2 — Exclusions schema and the Calendar (`simulate`, pure)

#### Step 2a — commit `Accept ranges and reasons in exclusions entries`

New file `simulate/exclusions.go`; move `Exclusions` and `ParseExclusions` out
of `pool.go` into it.

```go
// Entry is one exclusion: an inclusive [From, To] span of local calendar
// days with an optional free-text Reason. A single day has From == To.
type Entry struct {
    From   time.Time
    To     time.Time
    Reason string
}

type Exclusions struct {
    Global    []Entry            `json:"global"`
    Engineers map[string][]Entry `json:"engineers"`
}

func ParseExclusions(data []byte) (Exclusions, error)
// Days returns the sorted, de-duplicated local-midnight days for scope
// ("" = global, else an engineer name). nil when the scope has none.
func (x Exclusions) Days(scope string) []time.Time
// Scopes returns the engineer names present, sorted.
func (x Exclusions) Scopes() []string
// IsEmpty reports whether no entries exist in any scope.
func (x Exclusions) IsEmpty() bool
```

`Entry` implements `json.Unmarshaler` and `json.Marshaler`:
- Unmarshal accepts a JSON string `"YYYY-MM-DD"` or `"YYYY-MM-DD/YYYY-MM-DD"`,
  or an object with either `date` or both `from`+`to`, plus optional `reason`.
  Dates parse with `util.ParseDate`. Decode the object through a small struct
  using `json.Decoder.DisallowUnknownFields`.
- Errors (wrap with the entry's raw text): empty string; unparseable date;
  `to` before `from`; object with `date` *and* `from`/`to`; object with only
  one of `from`/`to`; object with none; unknown key; span > 366 days
  (`"range spans %d days; refusing more than 366"` — guards a typo'd year).
- Marshal always emits the object form `{"from","to"}` plus `"reason"` when
  non-empty (this is what the manifest records; it must round-trip through
  Unmarshal).
- `ParseExclusions` wraps element errors as
  `exclusions: global[2]: …` / `exclusions: engineers["alice"][0]: …`.
  Use `json.Unmarshal` into `Exclusions` and rely on the `Entry` unmarshaler;
  to get the index/scope into the message, decode in two passes
  (`map[string]json.RawMessage` → per-scope `[]json.RawMessage` → `Entry`), or
  any approach that yields those messages. Also reject a top-level key other
  than `global`/`engineers`.

`BuildPool` (still in `pool.go`) keeps its signature this step but switches
from re-parsing strings to `exc.Days("")` / `exc.Days(name)`; the silent
`continue` on bad dates disappears (parsing already failed upstream).

Tests — new `simulate/exclusions_test.go`:
- `TestParseExclusions_StringForms` — single day, range, both scopes;
  `Days("")` sorted and de-duplicated across overlapping entries.
- `TestParseExclusions_ObjectForms` — `date`+`reason`; `from`/`to`+`reason`;
  `Reason` preserved.
- `TestParseExclusions_Errors` — table: each error case above; assert the
  message contains the scope/index prefix and the offending text.
- `TestEntry_MarshalRoundTrip` — marshal then unmarshal equals original.
- `TestParseExclusions_LegacyFileUnchanged` — the exact current format
  (plain date strings in both scopes) parses to the same days as before.
- Existing `TestBuildPool_GlobalExclusionRemovesSlot`,
  `TestBuildPool_PerEngineerExclusion`,
  `TestBuildPool_WholeTeamSumsAndIgnoresPerEngineerExclusions` are rewritten
  to build `Exclusions` via `ParseExclusions([]byte(…))` (string literals), not
  struct literals of strings.

Verify: `just build && just test`.

#### Step 2b — commit `Add a Calendar of working days shared by pool and forecast`

New file `simulate/calendar.go`:

```go
// Calendar answers "is this scope working on day d?", where d counts whole
// days from anchor (d = 0 is the anchor date). Scope "" is everyone; a name
// is that engineer and also inherits the global rules. A nil *Calendar means
// every day is a working day.
//
// It is rule-based and anchored to a real date on purpose: the explicit-date
// rules come from an Exclusions file, and weekdaysOff is the hook for a
// future "weekends off" flag (unused this round). Both the sample pool
// (anchored at the sample start) and the forecast engine (anchored at the
// target start) use the same type, so the two sides cannot define "working
// day" differently.
type Calendar struct {
    anchor      time.Time              // local midnight
    weekdaysOff [7]bool                // indexed by time.Weekday; all false this round
    global      map[int]bool           // day index → off
    perName     map[string]map[int]bool
}

// NewCalendar builds a Calendar from exc anchored at anchor (normalized to
// local midnight via util.LocalDay).
func NewCalendar(exc Exclusions, anchor time.Time) *Calendar
func (c *Calendar) Anchor() time.Time
// Working reports whether scope name works on day index d (d may be any
// int; indices before the anchor are answered honestly). nil receiver → true.
func (c *Calendar) Working(name string, d int) bool
// Rebase returns an equivalent Calendar whose day 0 is newAnchor.
// nil receiver → nil.
func (c *Calendar) Rebase(newAnchor time.Time) *Calendar
// OffDays returns, sorted, the dates in [from, to) on which scope name does
// not work (global rules included). to < 0 means "no upper bound" and
// returns every explicitly excluded date at or after from. nil → nil.
func (c *Calendar) OffDays(name string, from, to int) []time.Time
```

- `Working`: weekday check uses `((int(c.anchor.Weekday())+d)%7+7)%7` so
  negative `d` is safe. Then `global[d]`, then `perName[name][d]` when
  `name != ""`.
- Day indices via `util.DayIndex(day, c.anchor)`.
- `OffDays` with `to < 0`: iterate the explicit maps (weekday mask contributes
  nothing unbounded); with `to >= 0`: iterate `d` in `[from, to)` and call
  `Working`.

`BuildPool` → `func BuildPool(records []Completion, cal *Calendar, startDate, endDate time.Time, wholeTeam bool) *SamplePool`.
- Precondition: `cal == nil || cal.Anchor().Equal(util.LocalDay(startDate))`;
  otherwise `panic(fmt.Sprintf("simulate.BuildPool: calendar anchored at %s but startDate is %s", …))`.
  It is a programming error, not a user error.
- Slot `i` is kept iff `cal.Working(name, i)` (`cal.Working("", i)` in
  whole-team mode). Update the doc comment.

`cmd/forecast/common.go` `loadPool` (minimal change so the tree compiles):
`simulate.BuildPool(records, simulate.NewCalendar(exc, startDate), startDate, endDate, wholeTeam)`.
Also store the calendar in `poolData` as `SampleCalendar *simulate.Calendar`
(the manifest uses it in Phase 4d).

Tests — new `simulate/calendar_test.go`:
- `TestCalendar_Working_GlobalAndPerName` — global day off for everyone and
  for "alice"; alice's day off not off for "bob" nor for `""`.
- `TestCalendar_NilIsAlwaysWorking`.
- `TestCalendar_BeforeAnchor` — an excluded date before the anchor yields a
  negative index and `Working` is false there; an unexcluded negative index
  is true.
- `TestCalendar_Rebase` — `Rebase(anchor+3d).Working(x, d) == Working(x, d+3)`
  for a spread of d.
- `TestCalendar_OffDays_BoundedAndUnbounded`.
- `TestCalendar_Anchor_NormalizesToLocalMidnight` — passing 14:30 anchors at
  00:00 of that day.
- `TestCalendar_WeekdayMaskReservedButInert` — set the unexported field
  directly in-package and confirm `Working` honors it for the right weekdays
  including a negative `d`; this pins the hook for §6.
- `pool_test.go`: adapt every `BuildPool` call to pass
  `NewCalendar(exc, start)` (or `nil`); add
  `TestBuildPool_PanicsOnMismatchedAnchor`.

Verify: `just build && just test`.

### Phase 3 — Slot engine (`simulate`, pure)

#### Step 3 — commit `Simulate over named slots with a working-day calendar`

`simulate/engine.go`

```go
// Slot is one daily draw in a simulation: a (possibly empty) engineer name
// and the sample slice it draws from. Name "" is an anonymous slot, which a
// Calendar can only exclude via its global rules.
type Slot struct {
    Name    string
    Samples []int
}

func simulateItems(slots []Slot, cal *Calendar, days, numSimulations, numWorkers int, seed int64, progress func(done, total int)) []int
func simulateDays(slots []Slot, cal *Calendar, items, numSimulations, numWorkers int, seed int64, progress func(done, total int)) []int
```

- `simulateItems` trial: `for d := 0; d < days; d++ { for _, s := range slots { if cal.Working(s.Name, d) { total += s.Samples[rng.Intn(len(s.Samples))] } } }`.
- `simulateDays` trial: `for completed < items { for s … if cal.Working(s.Name, days) {…}; days++ }`;
  return `days`. (Day index is the pre-increment value; the returned count
  is unchanged in meaning.)
- Day-outer, slot-inner. This is the draw-order change D5 warns about.
- `SimulateItemsInDays(samples, numDailyDraws, …)` and
  `SimulateDaysToComplete(samples, numEngineers, …)` stay exported, as thin
  wrappers: build `numDailyDraws` anonymous slots over `samples`, `cal = nil`.
  Update their doc comments to say so.

`simulate/pool.go`

```go
// Slots derives the simulation's slots from p: whole-team mode is one
// anonymous slot over the summed series; anonymous mode is p.Engineers slots
// over Combined, named after p.EngineerNames when given.
func (p *SamplePool) Slots(prm Params) []Slot
```

`simulate/dispatch.go`
- `Params` gains `EngineerNames []string` (doc: the `-engineers` names, if
  the flag was given names rather than a count; `len == Engineers` when set)
  and `Calendar *Calendar` (doc: horizon calendar anchored at target start;
  nil = every day working).
- `ItemsInDays(pool, p)` → `simulateItems(pool.Slots(p), p.Calendar, p.Days, …)`.
  `DaysToComplete` likewise. Delete the per-mode switch.

`simulate/mode.go`
- `func ModeLabel(mode Mode, engineers int, names []string) string` →
  `"whole-team throughput"`; `"3 equivalent engineers"`;
  `"3 equivalent engineers [alice, bob, carol]"` when `len(names) > 0`.
- `ValidatePool` unchanged in behavior.

`simulate/backtest.go` `RunBacktest`: per row, pass
`Calendar: p.Calendar.Rebase(r.Date)` into the `ItemsInDays` params (nil-safe).
Update the doc comment with one sentence on D9.

`cmd/forecast` — minimal edits so it compiles: `ModeLabel(mode, n, nil)` at
every call site. Nothing else yet.

Tests
- `engine_test.go`:
  - keep `TestSimulateItemsInDays_ConstantPool`, `TestSimulateDaysToComplete_ConstantPool`.
  - `TestSimulateItems_CalendarZeroesGlobalDays` — 3 anonymous slots over
    `{2}`, 10 days, calendar with 3 global off days inside the horizon →
    every trial `(10-3)*3*2 = 42`.
  - `TestSimulateItems_CalendarZeroesOnlyNamedSlot` — slots alice, bob over
    `{2}`; alice off 4 of 10 days → `10*2 + 6*2 = 32`.
  - `TestSimulateItems_AnonymousSlotIgnoresPerNameRules` — anonymous slots,
    calendar has only `alice` rules → unchanged total.
  - `TestSimulateItems_OffDaysBeyondHorizonAreIrrelevant`.
  - `TestSimulateDays_LeadingOffDaysAddCalendarDays` — 2 slots over `{2}`,
    items 20, first 3 days globally off → every trial `3 + 5 = 8`.
  - `TestSimulateDays_OffDayDoesNotConsumeRNG` — use a *non*-constant sample
    slice and a pinned seed: run (a) calendar with the first k days off, and
    (b) no calendar; assert `result(a) == result(b) + k` for every trial
    index. (Because off days skip the draw entirely, the RNG sequence lines
    up exactly.)
- `dispatch_test.go`: replace named-team cases with "anonymous named slots"
  cases (`EngineerNames: []string{"a","b"}`), including one with a calendar;
  whole-team case with a global off day.
- `mode_test.go` `TestModeLabel`: three cases per the new signature.
- `backtest_test.go` (exists): add `TestRunBacktest_CalendarAppliesPerReplayDay`
  — constant pool big enough that an unexcluded horizon gives 100% on a
  remaining>0 row; with a calendar excluding every day of the horizon the
  same row reports 0%. Build the calendar anchored at the replay start.
- `pool_test.go`: `TestSlots_Anonymous`, `TestSlots_Named`, `TestSlots_WholeTeam`.

`CHANGELOG.md` → Unreleased → Changed: `**Pinned `-random-seed` results from
earlier builds no longer reproduce.** The engine now draws day-by-day across
slots (to apply calendar exclusions) rather than engineer-by-engineer, so the
RNG stream is consumed in a different order. Same-build determinism is
unchanged: the same seed, inputs, and `-goroutines` still give identical
output every run.`

Verify: `just build && just test`.

### Phase 4 — CLI wiring (`cmd/forecast`)

#### Step 4a — commit `Make -exclusions opt-in and fail on a missing file`

- `addSimFlags`: `fs.String("exclusions", "", "path to an exclusions JSON file (holidays, PTO); applies to both the sample window and the forecast horizon; default: none")`.
- `loadExclusions(path)`: `path == ""` → `Exclusions{}, nil`; otherwise read
  the file and return any error (including not-exist) wrapped as
  `reading exclusions file %q: %w`. Delete the `ErrNotExist` branch.
- `manifestInputs.ExclusionsPath` stays for now.
- Tests (`cmd/forecast/common_test.go`, new or existing):
  `TestLoadExclusions_EmptyPathIsNone`, `TestLoadExclusions_MissingFileErrors`,
  `TestLoadExclusions_ParsesFile` (write JSON to `t.TempDir()`).
- `README.md` sampling-flags table: `-exclusions` default cell → *(none)*.
  Rewrite the "`exclusions.json` — holidays and time off" section heading
  and text: no default, must exist when given, the D2 entry grammar with the
  example from D2, "applies to both sides" in two sentences, and the
  per-engineer caveat for `-whole-team`. (Phase 4b/4d add nothing more to
  this section; write it complete now.)
- `CLAUDE.md`: the `exclusions.json` block under "Data formats" → the D2
  example and one sentence each on no-default and both-sides semantics.
- `CHANGELOG.md` → Changed: `**`-exclusions` no longer defaults to
  `./exclusions.json`.** Pass the path explicitly (or put `exclusions:` in a
  `-config` file). A path that does not exist is now an error instead of an
  empty set.` → Added: `**Exclusion ranges and reasons.** …` (D2 summary).

Verify:
```bash
just build && just test
# An unparseable ./exclusions.json in cwd must be ignored now (it used to be auto-loaded):
repo=$(pwd); d=$(mktemp -d); echo 'not json' > "$d/exclusions.json"
(cd "$d" && "$repo/bin/forecast" sim items -input "$repo/testdata/sample-issues.csv" -whole-team -days 5 \
   -sample-start 2025-01-01 -sample-end 2025-04-01 >/dev/null) && echo "default gone (OK)"
# An explicit path that does not exist must error:
./bin/forecast sim items -input testdata/sample-issues.csv -whole-team -days 5 -exclusions nope.json; echo "exit=$? (expect non-zero)"
```

#### Step 4b — commit `Accept engineer names in -engineers`

New `cmd/forecast/engineers.go`:

```go
// engineerSpec is the flag.Value behind -engineers: either a positive count
// of anonymous equivalent engineers ("3") or a comma-separated list of names
// ("alice,bob,carol") for the same number of named equivalent engineers.
// Names let per-engineer exclusions attach to a slot; they do not have to
// appear in the sample data.
type engineerSpec struct {
    count int
    names []string
}
func (e *engineerSpec) String() string   // "" when unset; count as decimal; else names joined by ","
func (e *engineerSpec) Set(v string) error
func (e *engineerSpec) Count() int
func (e *engineerSpec) Names() []string  // nil for a count
```

`Set` rules (trim whitespace first): empty → `"-engineers: empty value"`;
parses as int → must be `> 0` (`"-engineers: count must be positive, got %d"`),
sets count and clears names; otherwise split on `,`, trim, drop empties
(none left → error), reject any part that is all digits
(`"-engineers: %q looks like a count inside a name list"`), reject duplicates
(`"-engineers: duplicate name %q"`); `count = len(names)`.

- `simFlags.Engineers` becomes `engineerSpec` (value, not pointer);
  `fs.Var(&sf.Engineers, "engineers", "number of equivalent engineers (e.g. 3) or their names (e.g. alice,bob,carol) so per-engineer exclusions apply; one of -engineers or -whole-team is required")`.
- Every `*sf.Engineers` → `sf.Engineers.Count()`; `Params.EngineerNames: sf.Engineers.Names()`;
  `ModeLabel(mode, sf.Engineers.Count(), sf.Engineers.Names())`;
  `manifestInputs.Engineers`/`EngineerNames`; `Resolved.EngineerNames []string json:"engineer_names,omitempty"`.
- `printTrajectoryReport`: pass names through (signature grows; or pass a
  `simulate.Params` template — your call, keep it readable).
- D8: pure helper in `common.go`:
  `func unmatchedExclusionNames(exc simulate.Exclusions, assignees map[string]bool, slotNames []string) []string`
  (sorted). `loadPool` already computes `engineerSeen`; expose it on
  `poolData` as `Assignees map[string]bool`. Each sim subcommand (and
  backtest) calls it after `loadPool` and logs
  `logx.Warnf("exclusions: engineer %q matches no assignee in the sample data and no -engineers name", n)`
  per name.
- D7: if `*sf.WholeTeam && len(exc.Engineers) > 0` →
  `logx.Warnf("exclusions: per-engineer entries are ignored in -whole-team mode")`.
  Put both warnings in one helper `warnExclusionMismatches(exc, pd, sf)` so
  the four subcommands share it.

Tests — `cmd/forecast/engineers_test.go`:
`TestEngineerSpec_Count`, `TestEngineerSpec_Names`, `TestEngineerSpec_Errors`
(table: empty, `0`, `-2`, `"alice,,"` → ok with one name, `",,"` → error,
`"alice,3"` → error, `"alice,alice"` → error), `TestEngineerSpec_String`,
`TestEngineerSpec_ConfigListForm` (apply `engineers: [alice, bob]` through
`util.ApplyConfig` on a FlagSet and read back names).
`common_test.go`: `TestUnmatchedExclusionNames`.
`sim_test.go`: `TestCmdSimItems_NamedEngineersLabel` — run with
`-engineers alice,bob` and assert the header contains
`2 equivalent engineers [alice, bob]`.

Docs
- `README.md` "Choosing how to model the team": the three-row matrix from D4
  and one paragraph on why there is no per-person mode (D4a, briefly).
- `CLAUDE.md` `forecast sim` paragraph: describe `-engineers` count-or-names.
- `CHANGELOG.md`: Added → `**`-engineers` accepts names.** …`; reword the
  Removed `-team` entry's migration to point here.

Verify: `just build && just test`.

#### Step 4c — commit `Anchor every forecast to -target-start-date, default tomorrow`

- New helper in `common.go`:

```go
// resolveTargetWindow resolves a forecast horizon from -target-start-date and
// exactly one of -days / -target-end-date. end is inclusive; days is the
// calendar-day count of [start, end].
func resolveTargetWindow(cmd *flag.FlagSet, days int, startStr, endStr string, now time.Time) (start, end time.Time, effectiveDays int, err error)
```
  Errors (same text as today's `probability`): both given → `"-days and -target-end-date are mutually exclusive"`;
  neither → `"one of -days or -target-end-date must be provided"`; `-days <= 0` →
  `"-days must be positive"`; end not after start → `"-target-end-date must be after -target-start-date"`.
  With `-days`, `end = start.AddDate(0,0,days-1)`.
- `cmdSimItems`: `-days` default → `0` with usage `"number of days; mutually exclusive with -target-end-date, one must be given"`;
  add `-target-start-date` (default `"tomorrow"`) and `-target-end-date`
  (default `""`), same usage strings as `probability`. Call
  `resolveTargetWindow`. Header becomes
  `"%s, %s -> how many items?\n\n"` with `windowDescription` =
  `"2026-10-08 to 2026-11-06 (30 days)"` (factor `probability`'s existing
  `windowDescription` into a helper `describeWindow(start, end, days)` and use
  it in both; drop `probability`'s `-days`-only branch — the window is always
  known now). Resolve the window *before* `loadIssues`, alongside the other
  flag validation, so a bad window fails fast.
- `cmdSimProbability`: use `resolveTargetWindow`; `targetStart` is now always
  resolved.
- `cmdSimDays`: `-target-start-date` default `"today"` → `"tomorrow"`; usage
  `"forecast start date used to compute calendar dates (YYYY-MM-DD; or: yesterday, today, tomorrow, "-3 months"); default: tomorrow"`.
- Manifests: `items` and `probability` put `target_start_date`,
  `target_end_date`, `effective_days` in `Extra` unconditionally; `days` puts
  `target_start_date`.
- Tests: `common_test.go` `TestResolveTargetWindow` (table: days only; end
  only; both → err; neither → err; inclusive-end arithmetic
  `2025-03-02..2025-03-11` = 10 days; `-days 1` → end == start).
  `sim_test.go`: `TestCmdSimItems_RequiresDaysOrTargetEnd`,
  `TestCmdSimItems_TargetEndEquivalentToDays` (same seed, `-days 10` vs
  `-target-start-date 2025-03-02 -target-end-date 2025-03-11` → identical
  stdout apart from the header line). Existing `runSimItems` passes `-days 30`
  already and keeps working.
- `README.md`: `sim items` flag table gains the two flags and `-days` default
  cell becomes "give this **or** `-target-end-date`"; `sim days` table
  `-target-start-date` default → `tomorrow`.
- `CLAUDE.md`: conventions bullet listing date flags already includes
  `-target-start-date`/`-target-end-date`; add `sim items` to the sentence in
  the `forecast sim` paragraph; note the `tomorrow` default.
- `CHANGELOG.md` → Changed: `**`sim days` dates shift one day later**: the
  default `-target-start-date` is now `tomorrow` (was `today`), matching
  `probability`; today's completions are already in the sample via
  `-sample-end now`. Pass `-target-start-date today` for the old dates.`
  `**`sim items -days` has no default** (was 30): give `-days` or
  `-target-end-date`, like `probability`.` → Added: `sim items -target-start-date`/`-target-end-date`.

Verify:
```bash
just build && just test
go run ./cmd/forecast sim items -input testdata/sample-issues.csv -whole-team 2>&1 | grep -q "one of -days or -target-end-date" && echo OK
```

#### Step 4d — commit `Apply exclusions to the forecast horizon`

- In `cmdSimItems`, `cmdSimDays`, `cmdSimProbability`: after `loadPool`,
  `horizon := simulate.NewCalendar(loaded.Exclusions, targetStart)`;
  `Params.Calendar = horizon`; `printTrajectoryReport` receives and forwards
  it. (Note `loadPool` returns `Exclusions` in `poolData` already.)
- `cmdSimBacktest`: `Params.Calendar = simulate.NewCalendar(pd.Exclusions, startDate)`
  (the replay start; `RunBacktest` rebases per row — Phase 3).
- Manifest (`manifest.go`):

```go
type DataSection struct {
    DB         DataFile          `json:"db"`
    Exclusions ExclusionsSection `json:"exclusions"`
}
type ExclusionsSection struct {
    Path              string              `json:"path"`              // "" when none
    Source            simulate.Exclusions `json:"source"`            // entries marshal as {"from","to","reason"}
    SampleDaysDropped DaysByScope         `json:"sample_days_dropped"` // within [sample-start, sample-end)
    HorizonDaysOff    DaysByScope         `json:"horizon_days_off"`    // every excluded date at/after target start
}
type DaysByScope struct {
    Global    []string            `json:"global"`
    Engineers map[string][]string `json:"engineers,omitempty"`
}
```
  `manifestInputs` gains `SampleCalendar, HorizonCalendar *simulate.Calendar`
  (horizon nil for `backtest`, which writes no manifest anyway) and keeps its
  `ExclusionsPath` field, now written to `Data.Exclusions.Path`. Compute
  `SampleDaysDropped` with `SampleCalendar.OffDays(scope, 0, TotalDays)` and
  `HorizonDaysOff` with `HorizonCalendar.OffDays(scope, 0, -1)`, for `""` and
  each `Source.Scopes()` name; format `2006-01-02`. `SchemaVersion = 2`.
- Tests
  - `manifest_test.go` `TestNewManifest_Assembly`: `SchemaVersion == 2`; add
    calendars built from a small `Exclusions` and assert both `DaysByScope`.
  - `sim_test.go`, new fixture and tests. Fixture: alice completes exactly
    one issue per day 2025-01-01 … 2025-01-10:

    ```go
    const constantFixtureCSV = `identifier,team_key,assignee,state_type,created_at,started_at,completed_at
    C-1,ENG,alice,completed,2024-12-30,2024-12-31,2025-01-01
    … one row per day through …
    C-10,ENG,alice,completed,2025-01-08,2025-01-09,2025-01-10
    `
    ```
    With `-sample-start 2025-01-01 -sample-end 2025-01-11` every sample is 1,
    so results are exact. Horizon anchored at `-target-start-date 2025-03-02`
    (a Sunday; weekdays are irrelevant this round). Exclusions file written to
    `t.TempDir()`:
    `{"global":["2025-03-03/2025-03-05"],"engineers":{"alice":["2025-03-06","2025-03-07"]}}`.
    - `TestCmdSimItems_HorizonGlobalExclusions` — `-engineers 2 -days 10` →
      no file: every confidence row says `at least 20`; with file: `at least 14`.
    - `TestCmdSimItems_HorizonNamedExclusions` — `-engineers alice,bob -days 10`
      with file → `at least 12`.
    - `TestCmdSimItems_AnonymousIgnoresNamedExclusions` — `-engineers 2` with
      file → `at least 14`.
    - `TestCmdSimDays_HorizonExclusionsAddCalendarDays` — `-engineers 2 -items 20`
      → no file: `Days` 10, `Date 2025-03-12`; with file: 13, `2025-03-15`
      (match with a regexp like `(?m)^50%\s+13\s+2025-03-15`).
    - `TestCmdSimProbability_HorizonExclusions` — `-engineers 2 -days 10 -items 14`
      with file → `100.0%`; `-items 15` → `0.0%`.
    - `TestCmdSimItems_SampleExclusionsInManifest` — file
      `{"global":["2025-01-05"]}`, `-manifest -`; decode the JSON from stdout
      (the manifest is the only JSON object; the table follows it — split on
      the first `}\n` at column 0 or decode with `json.NewDecoder` and stop);
      assert `pool.per_engineer_sample_days.alice == 9`,
      `data.exclusions.sample_days_dropped.global == ["2025-01-05"]`,
      `schema_version == 2`.
    - `backtest_test.go`: `TestCmdSimBacktest_AcceptsExclusions` — runs with a
      file and a horizon-covering global range; the last projected rows
      report `0.00` probability where `remaining > 0`.
- Docs: `CLAUDE.md` `forecast sim` paragraph: one sentence that exclusions
  zero horizon days and that `backtest` applies them per replayed day;
  manifest schema 2 mention near `-manifest`. `CHANGELOG.md` → Added:
  `**Exclusions now apply to the forecast horizon** …` (D1 summary + D7);
  Changed: `**Manifest `schema_version` is 2.** `data.exclusions` replaces
  `data.exclusions_path`/`data.exclusions_applied` and records both the
  source entries and the resolved dates dropped from the sample and zeroed in
  the horizon.`

Verify:
```bash
just build && just test
./bin/forecast sim items -input testdata/sample-issues.csv -sample-start 2025-01-01 -sample-end 2025-04-01 \
  -engineers a,b -days 10 -exclusions <(echo '{"global":["2030-01-01"]}') 2>&1 | head -3
```
(The `<(…)` path exists during the run; expect the header to show the window
and the `[a, b]` label.)

### Phase 5 — `forecast check -exclusions`

#### Step 5 — commit `Validate an exclusions file with forecast check`

`cmd/forecast/check.go`
- Flag: `exclusions := cmd.String("exclusions", "", "path to an exclusions JSON file to validate against the input's assignees")`.
- Pure function:

```go
// checkExclusions renders the exclusions report lines for check's output.
// parseErr non-nil means the file did not parse; the one line returned then
// is the error. assignees is the set of non-empty Assignee values in the
// loaded issues; today is the caller's clock (local midnight).
func checkExclusions(exc simulate.Exclusions, parseErr error, assignees map[string]bool, today time.Time) []string
```
  Lines (each printed with the existing two-space indent):
  - `invalid   <err>` when `parseErr != nil`;
  - otherwise `entries   N global, M engineers (alice: 2, bob: 1)` (names
    sorted; omit the parenthetical when M == 0);
  - `span      2025-11-26 .. 2026-04-10 (12 dates past, 8 future, as of 2026-10-07)`
    counting distinct dates across all scopes; `none` when empty;
  - `engineers ok` when every name is an assignee; else
    `engineers bob: no assignee named "bob" in the input (fine if bob is given to sim -engineers)`
    one line per unmatched name.
- `cmdCheck`: after the per-command rows, when `*exclusions != ""`, read the
  file (`os.ReadFile`; a missing file is reported as an `invalid` line, not a
  returned error), `simulate.ParseExclusions`, build `assignees` from `raw`,
  print `Exclusions: <path>` then the lines. Return `nil`.
- Tests (`check_test.go`): `TestCheckExclusions_Invalid`,
  `TestCheckExclusions_Summary` (fixed `today`), `TestCheckExclusions_UnmatchedName`,
  `TestCmdCheck_ExclusionsFile` (end to end with a temp file; assert the
  `Exclusions:` header and an `entries` line).
- Docs: `README.md` `check` section gains the flag and a two-line example of
  the output; `CLAUDE.md` `forecast check` paragraph: one sentence;
  `CHANGELOG.md` → Added.

Verify: `just build && just test` and
`go run ./cmd/forecast check -input testdata/sample-issues.csv -exclusions <(echo '{"global":["2025-13-01"]}')`
prints an `invalid` line and exits 0.

### Phase 6 — Docs sweep and example file

#### Step 6 — commit `Document calendar exclusions and named engineers`

- `.gitignore`: add `!testdata/*.json` after the `*.json` line (same idiom as
  the existing `!internal/**/*.html`).
- Add `testdata/sample-exclusions.json`: the D2 example verbatim. Reference
  it from the README exclusions section and use it in one README example
  command.
- Read `README.md` top to bottom once and fix anything the earlier steps
  left inconsistent (the sim intro's "four subcommands … same sampling
  setup" paragraph; the "Config files" example comments; any `-team`,
  `-percentile`, or "exclusions.json from the working directory" remnants).
  `grep -n "\-team\b\|percentile\|working directory" README.md` must show
  only `aging -percentile` and the `-teams` filter.
- `CLAUDE.md`: same pass; also the architecture diagram's `forecast sim`
  caption is fine as is. Add `EXCLUSIONS_PLAN.md` to the "On-call modeling"
  section's neighborhood with one sentence: the `Calendar` type is where an
  on-call or weekends rule would plug in.
- `ONCALL_MODELING.md`: in "The Problem", replace "Currently, `exclusions.json`
  only covers major holidays" with a sentence that exclusions now cover
  past and future days for everyone or named engineers via
  `simulate.Calendar`, and on-call would be a third rule there.
- `CHANGELOG.md`: read the Unreleased section as a whole; merge duplicate
  bullets; keep Added / Changed / Removed ordering.
- Update this file's status line to **done**, with a short "what diverged"
  note if anything did (mirror `HISTORY_PLAN.md`'s header).

Verify: `just build && just test`, then the smoke run in §8.

---

## 6. Deferred but designed for: weekends off

Not this round (settled with the user), but the design accommodates it:

- **What it will be:** one flag (name TBD) that marks Saturday and Sunday as
  non-working on **both** sides at once — sample window (those day-slots drop
  from every series, so the distribution becomes workday throughput) and
  horizon (those days contribute 0). It must never apply to one side only;
  a workday-only sample paired with a weekend-counting horizon would bias
  every forecast upward.
- **Why it already fits:** `Calendar.weekdaysOff` exists; the flag sets it on
  both calendars. Output dates are unaffected because weekend days remain
  (zero-work) calendar days — `sim days` still reports calendar days and
  `start.AddDate(0, 0, days)` still lands correctly. Anchoring is guaranteed
  by D6 for every subcommand.
- **Later:** the flag, its config key, a header note saying "workdays",
  manifest recording, docs. Optionally a `"weekdays_off": ["sat","sun"]` key
  in `exclusions.json` so a company calendar file is self-contained.

## 7. Non-goals (this round)

- Fractional availability (half days, on-call at 50%). `Slot` could grow a
  weight later.
- Per-engineer exclusions under `-whole-team` (D7).
- Exclusions for `aging` / `cfd` / `history` — they count real events on
  real dates.
- Removing the deprecated `-db` alias. Out of scope; leave it.

## 8. Final verification (run after Phase 6)

The fixture's completions span 2025-01-02 … 2025-03-10, so every command
pins the sample window (`W` below) — the default "last 3 months" would be
empty.

```bash
just build && just test
W="-sample-start 2025-01-01 -sample-end 2025-04-01"
./bin/forecast check -input testdata/sample-issues.csv -exclusions testdata/sample-exclusions.json
./bin/forecast sim items -input testdata/sample-issues.csv $W -engineers alice,bob -days 20 -exclusions testdata/sample-exclusions.json -random-seed 1
./bin/forecast sim items -input testdata/sample-issues.csv $W -engineers alice,bob -days 20 -exclusions testdata/sample-exclusions.json -random-seed 1   # byte-identical to the line above
./bin/forecast sim days  -input testdata/sample-issues.csv $W -engineers 3 -items 10
./bin/forecast sim probability -input testdata/sample-issues.csv $W -whole-team -target-end-date "+30 days" -items 5
./bin/forecast sim items -input testdata/sample-issues.csv $W -whole-team -days 5 -team alice 2>&1 | grep -q "flag provided but not defined" && echo "-team gone"
./bin/forecast sim items -input testdata/sample-issues.csv $W -whole-team -days 5 -exclusions missing.json; echo "exit=$? (expect non-zero)"
```

Expected: every command runs; the two identical-seed runs match
byte-for-byte; the header lines show the window (`YYYY-MM-DD to YYYY-MM-DD (N days)`)
and, for named engineers, the `[alice, bob]` label; `-team` is an unknown
flag; a missing exclusions file is an error.

## 9. Record of review decisions

- Overload `-engineers` with names; no separate flag. (D4)
- Remove `-team` and `sim`'s `-percentile` outright; no tombstones — single
  user, active development. (D4a, D4b)
- Mixed string/object entry grammar. (D2)
- `sim items` gets both target-date flags; `tomorrow` default everywhere. (D6)
- Cross-build seed reproducibility may break; same-build determinism must
  not. (D5)
- Hard errors for malformed entries and missing files. (D2, D3)
- Manifest schema 2. (Step 4d)
- `check -exclusions`. (D10)
- Weekends deferred; `Calendar` designed to absorb it. (§6)
