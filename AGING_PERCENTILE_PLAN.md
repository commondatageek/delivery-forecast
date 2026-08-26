# Plan: configurable percentile + multiplier column for `forecast aging`

Status: **done**. Implemented top to bottom across three commits
(implementation, tests, docs); `just test` passes.

## Goal

Two user-visible changes to `forecast aging`:

1. **`-percentile N`** (new flag, default `85`) chooses which percentile of the
   historical cycle-time distribution the report is anchored to. Today `85` is
   hardcoded in three places (`p85 := util.PercentileValue(cycleTimes, 85)` in
   `cmd/forecast/aging.go`, and the `85`/`70` thresholds in `ageClass` in
   `aging/aging.go`).
2. **A `MULTIPLIER` column** on every row: `AgeDays / threshold`, where
   `threshold` is the cycle-time (in days) at the chosen percentile. An item
   older than the threshold shows `> 1.00x`; younger shows `< 1.00x`.

Nothing else about the command changes: same issue selection, same sorting
(descending by days — multiplier is a monotonic rescale of `AgeDays`, so the
order is unchanged), same `-show-completed` behavior.

## Definitions (get these exactly right)

- `threshold = util.PercentileValue(sortedCycleTimes, percentile)` — days.
  Same call the existing `p85` line makes, with `85` replaced by the flag.
- `Multiplier = AgeDays / threshold`, computed per item.
- **Degenerate case:** `threshold <= 0` (empty distribution, or a distribution
  whose value at that percentile is 0). Do **not** divide: `RankItems` leaves
  `Multiplier` at 0, and the renderers show an em dash (text/HTML) or `null`
  (JSON) rather than a number, keyed off `Meta.HasThreshold()`.
- `Percentile` (the existing rank column) and `Multiplier` answer different
  questions and are only approximate inverses: `util.ComputePercentile` is a
  cumulative rank with `math.Round`, `util.PercentileValue` is nearest-rank.
  An item whose multiplier is exactly `1.00x` may therefore rank `90th` rather
  than `85th` on a small distribution (the gap closes by roughly n=20 and is
  gone by n=100), and on ~0.5% of rows the two disagree outright about which
  side of the anchor an item falls on — always by a hair (`0.99x` ranking
  `90th`; `1.07x` ranking `89th`). **This is expected. Do not add code to
  reconcile them**, and in particular do not rewrite the percentile column to
  be an exact inverse of the multiplier: that would silently change both the
  meaning and the values of a column users already read. Coloring by the
  multiplier (Phase 1) is what keeps the disagreement from ever appearing as a
  contradiction inside a single cell. Document the rest (Phase 6).

## Decisions already made

- The flag is `-percentile`, an **int**, default `85`, validated to `1..100`.
  `aging.Item.Percentile` is already an `int` and `util.OrdinalSuffix` takes an
  `int`, so an int keeps every existing format string working unchanged.
  (Note for context: `sim items` has `-percentile` registered as a *tombstone*
  flag that always errors, and `sim days` has it as an alias for `-confidence`.
  Neither applies here — `aging` has no confidence-vs-percentile inversion; it
  is a plain percentile of an observed distribution. Do not copy the tombstone
  pattern into `aging`.)
- **Color thresholds are keyed on the multiplier, not on the percentile rank.**
  `high` at `>= 1.00x`, `medium` at `>= 0.85x` (i.e. within 15% of the anchor),
  nothing colored when there is no threshold. This is a deliberate change from
  today's `pct >= 85` / `pct >= 70`: the rank and the multiplier are computed by
  different methods and contradict each other on ~0.5% of rows at realistic
  sample sizes (~7% when the distribution has fewer than 20 issues), which would
  otherwise show up as a red row reading `0.99x`, or an uncolored row reading
  `1.07x`. Coloring by the multiplier makes a cell's color and its number agree
  by construction, and gives the "medium" band a real meaning ("within 15% of
  your anchor") instead of an arbitrary 15 rank points. Expect a small number of
  rows to change color versus today's output even at the default
  `-percentile 85`; that is the point.
- **JSON output stays a bare array** of item objects — adding a top-level
  metadata object would break existing consumers. The `multiplier` field is
  added to each object.
- The five render/rank call sites get a small **`Meta` struct** instead of two
  more positional `float64` parameters. `RenderText` already takes 8 positional
  args including a `p85 float64`; adding a second adjacent float64 next to it
  is a bug waiting to happen.

## Phase 1 — `aging` package: Item, RankItems, ageClass

File: `aging/aging.go`

1. Add to `Item` (after `Percentile int`):

   ```go
   // Multiplier is AgeDays expressed as a multiple of the report's
   // percentile threshold: 1.0 means exactly the threshold, >1.0 older,
   // <1.0 younger. It is 0 when the threshold is 0 (empty or degenerate
   // distribution); renderers check Meta.Threshold before displaying it.
   Multiplier float64
   ```

2. Change `RankItems` to take the threshold and set both fields:

   ```go
   // RankItems sets the Percentile and Multiplier fields on each item based
   // on its AgeDays: Percentile is its cumulative rank in the sorted
   // cycle-time distribution, Multiplier is its age as a multiple of
   // threshold (the distribution's value at the report's chosen percentile).
   // A threshold of 0 leaves Multiplier at 0 rather than dividing.
   func RankItems(items []Item, sortedCycleTimes []float64, threshold float64) {
       for i := range items {
           items[i].Percentile = util.ComputePercentile(sortedCycleTimes, items[i].AgeDays)
           if threshold > 0 {
               items[i].Multiplier = items[i].AgeDays / threshold
           }
       }
   }
   ```

3. Change `ageClass` to key on the multiplier rather than the percentile rank:

   ```go
   // ageClass buckets an item for display by how its age compares to the
   // report's percentile threshold: at or past the threshold is "high",
   // within 15% of it is "medium". Keying this on the multiplier rather than
   // on the percentile rank keeps a cell's color and the number printed in it
   // from ever disagreeing — the two are computed by different methods
   // (cumulative rank vs. nearest rank) and contradict each other on roughly
   // half a percent of rows, more on small distributions. With no usable
   // threshold there is nothing to compare against, so nothing is colored.
   func ageClass(mult float64, hasThreshold bool) string {
       switch {
       case !hasThreshold:
           return "normal"
       case mult >= 1.0:
           return "high"
       case mult >= 0.85:
           return "medium"
       default:
           return "normal"
       }
   }
   ```

4. Add the `Meta` type (put it near `Options`, and document it in the same
   voice as the surrounding comments):

   ```go
   // Meta carries the report-level context the renderers need alongside the
   // items themselves: which percentile the report is anchored to, the
   // cycle-time value at that percentile, the window the distribution was
   // drawn from, and how many completed issues went into it.
   type Meta struct {
       // Percentile is the `-percentile` flag (1–100).
       Percentile int
       // Threshold is the cycle time in days at Percentile. Zero means the
       // distribution was empty or degenerate: renderers must not present a
       // multiplier in that case.
       Threshold float64
       // SampleStart and SampleEnd are the completed-issue window, for display.
       SampleStart time.Time
       SampleEnd   time.Time
       // CompletedCount is len(cycleTimes) — the size of the distribution.
       CompletedCount int
   }

   // Label renders the percentile as it appears in headers, e.g. "P85".
   func (m Meta) Label() string { return fmt.Sprintf("P%d", m.Percentile) }

   // HasThreshold reports whether a multiplier can be shown at all.
   func (m Meta) HasThreshold() bool { return m.Threshold > 0 }
   ```

5. Add `-percentile` to the `Options` struct for parity with the other flags
   (`Options` is a documentation-grade convenience bundle; keep it in sync):

   ```go
   // Percentile is the `-percentile` flag: which percentile of the cycle-time
   // distribution the report is anchored to (default 85).
   Percentile int
   ```

## Phase 2 — `aging` package: text renderer

File: `aging/aging.go`

1. Replace the hand-written `textHeader` / `textDivider` pair with a single
   column list, so the header, the divider, and the row format can't drift:

   ```go
   var textColumns = []string{
       "IDENTIFIER", "TITLE", "DAYS", "PERCENTILE", "MULTIPLIER",
       "STATE", "START DATE", "ASSIGNEE",
   }

   var textHeader = strings.Join(textColumns, "\t")

   // textDivider is tab-separated with the same cell count as textHeader so it
   // stays within the same tabwriter column block as the rows around it —
   // otherwise the two sections' columns would be sized independently and no
   // longer line up.
   var textDivider = func() string {
       dashes := make([]string, len(textColumns))
       for i, c := range textColumns {
           dashes[i] = strings.Repeat("-", len(c))
       }
       return strings.Join(dashes, "\t")
   }()
   ```

   (Keep `textHeader`/`textDivider` as package-level `var`s with those exact
   names; the existing comment above `textDivider` is preserved verbatim.)

2. `writeItemRow` takes `meta` and gains the multiplier cell:

   ```go
   func writeItemRow(tw *tabwriter.Writer, item Item, meta Meta) {
       pct := item.Percentile
       fmt.Fprintf(tw, "%s\t%s\t%.1f\t%d%s\t%s\t%s\t%s\t%s\n",
           item.Identifier,
           truncateTitle(item.Title),
           item.AgeDays,
           pct, util.OrdinalSuffix(pct),
           formatMultiplier(item.Multiplier, meta),
           formatState(item.StateName, item.StateType),
           formatStartDate(item.StartedAt),
           item.Assignee,
       )
   }

   // formatMultiplier renders an item's age as a multiple of the report's
   // percentile threshold, e.g. "1.34x". An em dash stands in when the
   // distribution has no usable threshold to divide by.
   func formatMultiplier(m float64, meta Meta) string {
       if !meta.HasThreshold() {
           return "—"
       }
       return fmt.Sprintf("%.2fx", m)
   }
   ```

3. `RenderText` drops `cycleTimes`, `p85`, `sampleStart`, `sampleEnd` in favor
   of `meta` (`cycleTimes` was only ever used for its length, which is now
   `meta.CompletedCount`):

   ```go
   func RenderText(w io.Writer, items []Item, completed []Item, showCompleted bool, meta Meta) error {
       fmt.Fprintf(w, "Cycle time distribution: %d completed issues (%s to %s)  ·  %s: %.1f days\n\n",
           meta.CompletedCount,
           meta.SampleStart.Format("2006-01-02"),
           meta.SampleEnd.Format("2006-01-02"),
           meta.Label(),
           meta.Threshold,
       )
       ...
   }
   ```

   Pass `meta` through to each `writeItemRow` call. Update the doc comment to
   mention the multiplier column.

## Phase 3 — `aging` package: JSON and HTML renderers

File: `aging/aging.go`

1. **JSON.** Add to `jsonItem`, after `Percentile`:

   ```go
   Multiplier *float64 `json:"multiplier"`
   ```

   A pointer so the degenerate case emits an explicit `null` rather than a
   misleading `0` (a genuinely-zero multiplier — a zero-day item — is a
   different fact from "no threshold to divide by"). `RenderJSON` gains a
   `meta Meta` parameter and sets it:

   ```go
   var mult *float64
   if meta.HasThreshold() {
       v := math.Round(item.Multiplier*100) / 100
       mult = &v   // NOTE: declare v inside the loop; do not take the address
                   // of a loop-scoped variable reused across iterations.
   }
   ```

   Rounded to 2 decimals, matching the `%.2fx` text/HTML formatting (the
   existing `AgeDays` rounds to 1 via the same `math.Round` idiom).

2. **HTML.** In `htmlItemData` add `Multiplier string` (pre-formatted by
   `toHTMLItems` via the same `formatMultiplier`, so text and HTML can never
   disagree). In `htmlData` replace `P85 float64` with `Meta Meta`, keeping
   `Count`, `Items`, `Completed`, `ShowCompleted`; drop `CompletedCount`,
   `SampleStart`, `SampleEnd` (all now on `Meta`).

3. `toHTMLItems` takes `meta Meta` and uses it for both `Multiplier` and
   `AgeClass: ageClass(item.Multiplier, meta.HasThreshold())`.

4. In `htmlTmpl`:
   - `<p class="meta">` line: `P85 cycle time:` → `{{.Meta.Label}} cycle time:`,
     `{{printf "%.1f" .P85}}` → `{{printf "%.1f" .Meta.Threshold}}`,
     `{{.CompletedCount}}` → `{{.Meta.CompletedCount}}`,
     `{{.SampleStart}}`/`{{.SampleEnd}}` → `{{.Meta.SampleStart.Format "2006-01-02"}}`
     etc. — **simpler**: pre-format them into `htmlData` as
     `SampleStart string` / `SampleEnd string` as today and leave those two
     fields alone. Do that; only `P85` and `CompletedCount` need to move.
   - `<colgroup>`: the widths must still total 100%. Eight columns:
     `10%, 23%, 8%, 11%, 9%, 15%, 12%, 12%` (Title drops 32→23, Multiplier
     takes 9).
   - Add `<th>Multiplier</th>` after `<th>Percentile</th>`.
   - Add `<td class="num {{.AgeClass}}">{{.Multiplier}}</td>` after the
     percentile `<td>`.
   - The `table` template is invoked as `{{template "table" .Items}}`, so
     inside it `.` is the item slice — the item struct already carries the
     pre-formatted `Multiplier` string, which is exactly why it's pre-formatted
     rather than computed in the template.

5. `RenderHTML` signature becomes
   `RenderHTML(w io.Writer, items []Item, completed []Item, showCompleted bool, meta Meta) error`.

## Phase 4 — CLI wiring

File: `cmd/forecast/aging.go`

1. Register the flag next to `minCycleTimeStr`:

   ```go
   percentile := cmd.Int("percentile", 85, "percentile of the cycle-time distribution to anchor the report to (1-100)")
   ```

2. After `util.ApplyConfig` (so a config-file value is validated too), validate:

   ```go
   if *percentile < 1 || *percentile > 100 {
       return fmt.Errorf("-percentile must be between 1 and 100, got %d", *percentile)
   }
   ```

3. Add `Percentile: *percentile` to the `aging.Options` literal.

4. Replace the `p85` line and build the meta:

   ```go
   threshold := util.PercentileValue(cycleTimes, float64(opts.Percentile))

   meta := aging.Meta{
       Percentile:     opts.Percentile,
       Threshold:      threshold,
       SampleStart:    opts.SampleStart,
       SampleEnd:      opts.SampleEnd,
       CompletedCount: len(cycleTimes),
   }
   ```

   **Ordering matters:** `threshold` must be computed before the two
   `aging.RankItems` calls, which today sit above the `p85` line. Move the
   `threshold`/`meta` construction up to just after `sort.Float64s(cycleTimes)`,
   then pass `threshold` into both `RankItems` calls.

5. Update the empty-distribution warning to mention the new column:

   ```go
   logx.Warnf("no completed issues found in the sample window; percentiles will be 0 and multipliers blank")
   ```

   Keep it where it is (after ranking, before rendering).

6. Update the three render calls:

   ```go
   case "text":
       return aging.RenderText(os.Stdout, inProgressItems, completedItems, *showCompleted, meta)
   case "json":
       return aging.RenderJSON(os.Stdout, inProgressItems, meta)
   case "html":
       return aging.RenderHTML(os.Stdout, inProgressItems, completedItems, *showCompleted, meta)
   ```

No changes are needed for `-config`: `util.ApplyConfig` is keyed on flag name,
so `percentile: 90` in an aging YAML works the moment the flag exists.

## Phase 5 — Tests

`aging/aging_test.go`:

1. **Update `TestRankItems`** for the new signature. Existing distribution is
   `{1..10}`; `util.PercentileValue(dist, 85)` = index `round(0.85*9)` = 8 →
   `9.0`. Pass `9.0` as the threshold and assert the existing percentile
   expectations plus: `AgeDays 5.0 → Multiplier ≈ 0.5556`,
   `10.0 → ≈ 1.1111`, `0.5 → ≈ 0.0556`. Compare with a tolerance
   (`math.Abs(got-want) > 1e-9`), never `==`.
2. **New `TestRankItemsZeroThreshold`** — threshold `0` leaves every
   `Multiplier` at `0` and does not produce `NaN`/`Inf` (assert with
   `math.IsNaN` / `math.IsInf`).
3. **New `TestAgeClassFromMultiplier`** — table-driven over
   `ageClass(mult float64, hasThreshold bool)`:
   `(1.00, true) → "high"`, `(1.01, true) → "high"`, `(0.99, true) → "medium"`,
   `(0.85, true) → "medium"`, `(0.84, true) → "normal"`, `(0.0, true) →
   "normal"`, and `(1.50, false) → "normal"` (no threshold means no signal to
   color by, whatever the stale multiplier says). Exact-boundary cases matter
   here — `1.00` and `0.85` are the two comparisons most likely to be written
   as `>` instead of `>=`.
4. **New `TestRenderTextMultiplierColumn`** — render one item into a
   `bytes.Buffer` with `Meta{Percentile: 90, Threshold: 4}` and `AgeDays: 6`;
   assert the output contains `MULTIPLIER`, `1.50x`, and `P90: 4.0 days`.
   Then render with `Threshold: 0` and assert it contains `—` and not `x`.

`cmd/forecast/aging_test.go`:

5. **New `TestCmdAging_PercentileFlag`** — reuse `agingFixtureCSV` and
   `runAgingJSON`. The fixture's completed issues are E-1 (3 days) and E-2
   (4 days), and the only in-progress issue is E-3. The distribution is
   therefore `{3, 4}`, and `util.PercentileValue` is nearest-rank over
   `len-1 == 1`, so **most percentile values collide**: 51-100 all map to
   index 1 (`4.0`) and 0-50 all map to index 0 (`3.0`). `-percentile 50` and
   `-percentile 100` would give the *same* threshold — use `-percentile 1` and
   `-percentile 100` instead (thresholds `3.0` and `4.0`). Assert the emitted
   `multiplier` differs between the two runs and is larger for the lower
   percentile (a lower threshold divides into a larger multiple), and that the
   JSON parses into `[]map[string]any` with `multiplier` present. E-3's age is
   measured against the real `time.Now()`, so its absolute multiplier is large
   and grows daily — assert the *relationship* between the two runs, never a
   fixed value.
6. **New `TestCmdAging_PercentileValidation`** — `cmdAging` with
   `-percentile 0` and `-percentile 101` each return a non-nil error. Note:
   the flag set is `flag.ExitOnError`, but these are *post-parse* validations
   returning an error, so the test is safe — do **not** try to test an
   unparseable flag value, which would exit the process.
7. **New `TestCmdAging_DefaultPercentileUnchanged`** — running with no
   `-percentile` produces text output containing `P85:`, pinning the default.

Run `just test` (or `go test ./...`). No other package calls `aging`, so the
blast radius is `aging/` + `cmd/forecast/` only — confirm with
`grep -rn "delivery-forecast/aging" --include=*.go .` before starting, and
again after.

## Phase 6 — Docs

1. **`README.md`**, the `forecast aging` flag table (~line 270): add a row
   after `-min-cycle-time`:

   | `-percentile` | `85` | percentile of the cycle-time distribution to anchor the report to; the `MULTIPLIER` column shows each item's age as a multiple of that threshold |

   Also add a sentence under the section's intro paragraph explaining the
   multiplier column and the boundary caveat from "Definitions" above.
2. **`CLAUDE.md`**, the `forecast aging` bullet: mention `-percentile`
   (default 85) and the multiplier column, and add `percentile: 90` to the
   `aging.yaml` config example in the "Config files" section.
3. **`aging/doc.go`**: the package summary says RankItems "scores both against
   that distribution by percentile" — extend it to say it also scores them as a
   multiple of a chosen percentile threshold, and mention `Meta`.
4. **`CHANGELOG.md`**, under `## Unreleased`. One `### Added` entry for
   `forecast aging -percentile` and the `MULTIPLIER` column. Then two
   `### Changed` entries (the section already exists under Unreleased), because
   both alter output people may already be reading:
   - `forecast aging -format json` gains a `multiplier` key on every object —
     relevant to anyone decoding it strictly.
   - The text/HTML color bands on `forecast aging` now key on the multiplier
     (`>= 1.00x` high, `>= 0.85x` medium) rather than on the percentile rank
     (`>= 85` / `>= 70`). At the default `-percentile 85` the two agree on
     roughly 99.5% of rows; the rest are hairline cases that used to be
     colored against the number printed beside them.
5. **`DATA_REQUIREMENTS.md`**: no change — the new flag needs no new fields.

## Non-goals

- No per-item percentile (the flag is report-wide).
- No change to which issues are selected, or to sort order.
- No multi-value `-percentile` list (unlike `sim`'s `-confidence`); one anchor
  per report keeps the multiplier column single-valued.
- No JSON envelope/metadata object; the output stays a bare array.
