package importmap

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"
)

// ReadCSV reads a whole CSV export: strips a leading UTF-8 BOM, returns the
// header row and the data rows. Uses encoding/csv with default settings
// (FieldsPerRecord 0 — every row must match the header's width).
func ReadCSV(r io.Reader) (header []string, rows [][]string, err error) {
	cr := csv.NewReader(r)
	header, err = cr.Read()
	if err == io.EOF {
		return nil, nil, fmt.Errorf("empty file: no header row")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading header: %w", err)
	}
	header[0] = strings.TrimPrefix(header[0], bom)
	rows, err = cr.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	return header, rows, nil
}

// DataError lists every way an export failed to fit its mapping.
type DataError struct{ Problems []string }

func (e *DataError) Error() string { return strings.Join(e.Problems, "\n") }

// maxExamples caps the example rows (or values) quoted per problem.
const maxExamples = 5

// Apply maps rows onto issues. Issues come back in file order with Source
// set from the mapping. Every referenced header must exist (and be unique
// unless picked); then every row is checked and every problem collected. On
// any problem it returns nil issues and a *DataError; the Report is still
// filled as far as the scan got.
//
// Per row: excluded rows are dropped first and checked no further. Status is
// the first of: overrides[status_name], resolution_overrides[resolution]
// (a non-empty resolution only), values[status.column]. A conditional
// timestamp is kept only when the row's status is in its rule's when list.
func Apply(m *Mapping, header []string, rows [][]string) ([]issues.Issue, Report, error) {
	rep := Report{Rows: len(rows), Columns: len(header)}
	a := &applier{m: m, header: header, pos: make(map[string][]int)}
	for i, h := range header {
		a.pos[norm(h)] = append(a.pos[norm(h)], i)
	}
	if problems := a.checkHeaders(); len(problems) > 0 {
		return nil, rep, &DataError{Problems: problems}
	}
	out, rep := a.scan(rows, rep)
	if problems := a.rowProblems(); len(problems) > 0 {
		return nil, rep, &DataError{Problems: problems}
	}
	return out, rep, nil
}

type applier struct {
	m      *Mapping
	header []string
	pos    map[string][]int // normalized header -> positions, in header order

	// row problems, collected over the whole file
	emptyID   []int
	idRows    map[string][]int // identifier -> rows, for repeated ones
	idOrder   []string
	badTimes  []*badTime
	unmatched *valueRows
}

// badTime collects unparseable values for one (target, header) pair.
type badTime struct {
	target, from string
	n            int
	exampleRow   int
	example      string
}

// valueRows counts rows per distinct (normalized) value, remembering the
// first spelling seen and the first few rows.
type valueRows struct {
	order []string
	first map[string]string
	n     map[string]int
	rows  map[string][]int
}

func newValueRows() *valueRows {
	return &valueRows{first: map[string]string{}, n: map[string]int{}, rows: map[string][]int{}}
}

func (v *valueRows) add(raw string, row int) {
	k := norm(raw)
	if _, ok := v.first[k]; !ok {
		v.order = append(v.order, k)
		v.first[k] = strings.TrimSpace(raw)
	}
	v.n[k]++
	if len(v.rows[k]) < maxExamples {
		v.rows[k] = append(v.rows[k], row)
	}
}

func (v *valueRows) total() int {
	t := 0
	for _, n := range v.n {
		t += n
	}
	return t
}

// checkHeaders verifies every header a rule names exists and, unless a pick
// resolves it, appears once.
func (a *applier) checkHeaders() []string {
	var problems []string
	need := func(path, h string, pickable bool, hasPick bool) {
		p := a.pos[norm(h)]
		switch {
		case len(p) == 0:
			msg := fmt.Sprintf("%s: no column %q in the file", path, h)
			if c, ok := closest(h, a.header); ok {
				msg += fmt.Sprintf(" (closest: %q)", c)
			}
			problems = append(problems, msg)
		case len(p) > 1 && !hasPick:
			if pickable {
				problems = append(problems, fmt.Sprintf("%s: column %q appears %d times; add pick: first, last or join", path, h, len(p)))
			} else {
				problems = append(problems, fmt.Sprintf("%s: column %q appears %d times; only a single {column: …, pick: …} can choose between repeated columns", path, h, len(p)))
			}
		}
	}
	for _, c := range a.m.Columns {
		path := "columns." + c.Target
		for _, h := range c.Headers {
			need(path, h, len(c.Headers) == 1, c.Pick != "")
		}
	}
	if s := a.m.Status; s != nil && s.Column != "" {
		need("status.column", s.Column, false, false)
	}
	for _, t := range a.m.Timestamps {
		for _, h := range t.Headers {
			need("timestamps."+t.Target, h, false, false)
		}
	}
	for _, e := range a.m.Exclude {
		need(fmt.Sprintf("exclude[%q]", e.Header), e.Header, false, false)
	}
	return problems
}

// cell returns a header's trimmed value in row (the header is unique).
func (a *applier) cell(row []string, h string) string {
	return strings.TrimSpace(row[a.pos[norm(h)][0]])
}

// columnValue evaluates a column rule on row and reports which header the
// value came from ("" for a constant or an empty result).
func (a *applier) columnValue(c ColumnRule, row []string) (string, string) {
	if len(c.Headers) == 0 {
		return strings.TrimSpace(c.Value), ""
	}
	if c.Pick != "" {
		var vals []string
		for _, i := range a.pos[norm(c.Headers[0])] {
			if v := strings.TrimSpace(row[i]); v != "" {
				vals = append(vals, v)
			}
		}
		if len(vals) == 0 {
			return "", ""
		}
		switch c.Pick {
		case "first":
			return vals[0], c.Headers[0]
		case "last":
			return vals[len(vals)-1], c.Headers[0]
		default: // join
			return strings.Join(vals, ", "), c.Headers[0]
		}
	}
	return a.firstNonEmpty(c.Headers, row)
}

// firstNonEmpty returns the first non-empty value among headers, and which
// header it came from.
func (a *applier) firstNonEmpty(headers []string, row []string) (string, string) {
	for _, h := range headers {
		if v := a.cell(row, h); v != "" {
			return v, h
		}
	}
	return "", ""
}

// parseTime parses v with the mapping's layouts, in order, in its location.
func (a *applier) parseTime(v string) (time.Time, bool) {
	for _, l := range a.m.TimeFormats {
		if t, err := time.ParseInLocation(l, v, a.m.Location); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// noteBadTime records an unparseable timestamp value.
func (a *applier) noteBadTime(target, from string, row int, v string) {
	for _, b := range a.badTimes {
		if b.target == target && b.from == from {
			b.n++
			return
		}
	}
	a.badTimes = append(a.badTimes, &badTime{target: target, from: from, n: 1, exampleRow: row, example: v})
}

// statusHit records how one row's status was decided, for Report.Status.
type statusHit struct {
	rule  int    // 1 override, 2 resolution override, 3 category value
	value string // the matched data value, as first seen
}

// matchStatus applies the status rule to a row. ok is false when no rule
// matches; shown is then the value an error should quote.
func (a *applier) matchStatus(it *issues.Issue, row []string) (st issues.Status, hit statusHit, shown string, ok bool) {
	if a.m.mapped("status") {
		raw := string(it.Status) // set from the column by setField
		if st, ok := issues.ParseStatus(raw); ok {
			return st, statusHit{3, raw}, raw, true
		}
		return issues.StatusUnknown, statusHit{}, raw, false
	}
	s := a.m.Status
	if s == nil {
		return issues.StatusUnknown, statusHit{}, "", true
	}
	if st, ok := lookup(s.Overrides, it.StatusName); ok {
		return st, statusHit{1, it.StatusName}, "", true
	}
	if it.Resolution != "" {
		if st, ok := lookup(s.ResolutionOverrides, it.Resolution); ok {
			return st, statusHit{2, it.Resolution}, "", true
		}
	}
	if s.Column != "" {
		v := a.cell(row, s.Column)
		if st, ok := lookup(s.Values, v); ok {
			return st, statusHit{3, v}, "", true
		}
		return issues.StatusUnknown, statusHit{}, v, false
	}
	return issues.StatusUnknown, statusHit{}, it.StatusName, false
}

func lookup(matches []StatusMatch, v string) (issues.Status, bool) {
	for _, m := range matches {
		if norm(m.Raw) == norm(v) {
			return m.Status, true
		}
	}
	return issues.StatusUnknown, false
}

// scan maps every row, collecting row problems on a and filling rep.
func (a *applier) scan(rows [][]string, rep Report) ([]issues.Issue, Report) {
	a.idRows = make(map[string][]int)
	a.unmatched = newValueRows()

	excluded := make([]*valueRows, len(a.m.Exclude))
	for i := range excluded {
		excluded[i] = newValueRows()
	}
	type statusKey struct {
		rule  int
		value string
	}
	via := map[issues.Status]*struct {
		order []statusKey
		first map[statusKey]string
		n     map[statusKey]int
	}{}
	statusN := map[issues.Status]int{}
	tsFrom := map[string]map[string]int{} // target -> header -> rows

	var out []issues.Issue
rowLoop:
	for i, row := range rows {
		rowNum := i + 2 // the header is row 1, as a spreadsheet counts
		for j, e := range a.m.Exclude {
			v := a.cell(row, e.Header)
			for _, x := range e.Values {
				if norm(x) == norm(v) {
					excluded[j].add(v, rowNum)
					continue rowLoop
				}
			}
		}

		it := issues.Issue{Source: a.m.Source}
		for _, c := range a.m.Columns {
			v, from := a.columnValue(c, row)
			if !isTimestamp(c.Target) {
				setField(&it, c.Target, v)
				continue
			}
			if v == "" {
				continue
			}
			t, ok := a.parseTime(v)
			if !ok {
				where := "value"
				if len(c.Headers) > 0 {
					where = fmt.Sprintf("column %q", from)
				}
				a.noteBadTime(c.Target, where, rowNum, v)
				continue
			}
			setTime(&it, c.Target, t)
			addFrom(tsFrom, c.Target, columnLabel(c, from))
		}

		if it.Identifier == "" {
			a.emptyID = append(a.emptyID, rowNum)
		} else {
			if _, seen := a.idRows[it.Identifier]; !seen {
				a.idOrder = append(a.idOrder, it.Identifier)
			}
			a.idRows[it.Identifier] = append(a.idRows[it.Identifier], rowNum)
		}

		st, hit, shown, ok := a.matchStatus(&it, row)
		if !ok {
			a.unmatched.add(shown, rowNum)
		}
		it.Status = st
		statusN[st]++
		if hit.rule != 0 {
			v := via[st]
			if v == nil {
				v = &struct {
					order []statusKey
					first map[statusKey]string
					n     map[statusKey]int
				}{first: map[statusKey]string{}, n: map[statusKey]int{}}
				via[st] = v
			}
			k := statusKey{hit.rule, norm(hit.value)}
			if _, seen := v.first[k]; !seen {
				v.order = append(v.order, k)
				v.first[k] = hit.value
			}
			v.n[k]++
		}

		for _, tr := range a.m.Timestamps {
			if !containsStatus(tr.When, st) {
				continue
			}
			v, from := a.firstNonEmpty(tr.Headers, row)
			if v == "" {
				continue
			}
			t, ok := a.parseTime(v)
			if !ok {
				a.noteBadTime(tr.Target, fmt.Sprintf("column %q", from), rowNum, v)
				continue
			}
			setTime(&it, tr.Target, t)
			addFrom(tsFrom, tr.Target, from)
		}
		out = append(out, it)
	}

	for j, e := range a.m.Exclude {
		for _, k := range excluded[j].order {
			rep.Excluded = append(rep.Excluded, ValueCount{Label: e.Header + " = " + excluded[j].first[k], N: excluded[j].n[k]})
		}
	}
	rep.Imported = len(out)

	suffix := map[int]string{1: " (override)", 2: " (resolution)", 3: ""}
	for _, st := range []issues.Status{issues.StatusBacklog, issues.StatusStarted, issues.StatusCompleted, issues.StatusCanceled, issues.StatusUnknown} {
		if st == issues.StatusUnknown && statusN[st] == 0 {
			continue
		}
		sc := StatusCount{Status: statusLabel(st), N: statusN[st], Via: []ValueCount{}}
		if v := via[st]; v != nil {
			for rule := 1; rule <= 3; rule++ {
				for _, k := range v.order {
					if k.rule == rule {
						sc.Via = append(sc.Via, ValueCount{Label: v.first[k] + suffix[rule], N: v.n[k]})
					}
				}
			}
		}
		rep.Status = append(rep.Status, sc)
	}

	for _, target := range issues.Columns() {
		if target == "source" {
			continue
		}
		headers, ok := a.mappedHeaders(target)
		if !ok {
			continue
		}
		if isTimestamp(target) {
			fs := FieldSource{Target: target, From: []ValueCount{}}
			for _, it := range out {
				if !getTime(it, target).IsZero() {
					fs.Set++
				}
			}
			for _, h := range headers {
				if n := tsFrom[target][h]; n > 0 {
					fs.From = append(fs.From, ValueCount{Label: h, N: n})
				}
			}
			rep.Timestamps = append(rep.Timestamps, fs)
			continue
		}
		ff := FieldFill{Target: target}
		for _, it := range out {
			if getField(it, target) != "" {
				ff.Filled++
			}
		}
		rep.Fields = append(rep.Fields, ff)
	}
	return out, rep
}

// mappedHeaders reports whether target is mapped (under columns or
// timestamps) and the labels its values can come from, in rule order.
func (a *applier) mappedHeaders(target string) ([]string, bool) {
	for _, c := range a.m.Columns {
		if c.Target == target {
			if len(c.Headers) == 0 {
				return []string{"value"}, true
			}
			return c.Headers, true
		}
	}
	for _, t := range a.m.Timestamps {
		if t.Target == target {
			return t.Headers, true
		}
	}
	return nil, false
}

// columnLabel names where a column rule's value came from: the header, or
// "value" for a constant.
func columnLabel(c ColumnRule, from string) string {
	if len(c.Headers) == 0 {
		return "value"
	}
	return from
}

func addFrom(m map[string]map[string]int, target, from string) {
	if m[target] == nil {
		m[target] = map[string]int{}
	}
	m[target][from]++
}

func containsStatus(list []issues.Status, st issues.Status) bool {
	for _, s := range list {
		if s == st {
			return true
		}
	}
	return false
}

func statusLabel(st issues.Status) string {
	if st == issues.StatusUnknown {
		return "unknown"
	}
	return string(st)
}

// rowProblems turns what scan collected into messages, in a fixed order:
// identifiers, timestamps, status.
func (a *applier) rowProblems() []string {
	var problems []string
	if n := len(a.emptyID); n > 0 {
		problems = append(problems, fmt.Sprintf("identifier: empty on %d %s (%s %s)", n, plural(n, "row", "rows"), plural(n, "row", "rows"), rowList(a.emptyID)))
	}
	repeated := 0
	for _, id := range a.idOrder {
		rs := a.idRows[id]
		if len(rs) < 2 {
			continue
		}
		repeated++
		if repeated <= maxExamples {
			problems = append(problems, fmt.Sprintf("identifier: %q appears on rows %s", id, andList(rs)))
		}
	}
	if repeated > maxExamples {
		problems = append(problems, fmt.Sprintf("identifier: %d more repeated values", repeated-maxExamples))
	}
	for _, b := range a.badTimes {
		problems = append(problems, fmt.Sprintf("%s (%s): %d %s no time_format, e.g. row %d %q",
			b.target, b.from, b.n, plural(b.n, "value matches", "values match"), b.exampleRow, b.example))
	}
	if n := a.unmatched.total(); n > 0 {
		var parts []string
		for _, k := range a.unmatched.order {
			c := a.unmatched.n[k]
			parts = append(parts, fmt.Sprintf("%q (%d, %s %s)", a.unmatched.first[k], c,
				plural(len(a.unmatched.rows[k]), "row", "rows"), rowList(a.unmatched.rows[k])))
		}
		problems = append(problems, fmt.Sprintf("status: %d %s no rule: %s", n, plural(n, "row matches", "rows match"), strings.Join(parts, ", ")))
	}
	return problems
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// rowList renders up to maxExamples row numbers as "3, 4, 5".
func rowList(rows []int) string {
	if len(rows) > maxExamples {
		rows = rows[:maxExamples]
	}
	parts := make([]string, len(rows))
	for i, r := range rows {
		parts[i] = fmt.Sprint(r)
	}
	return strings.Join(parts, ", ")
}

// andList renders row numbers as "4 and 12" or "4, 7 and 12", capped at
// maxExamples.
func andList(rows []int) string {
	more := 0
	if len(rows) > maxExamples {
		more = len(rows) - maxExamples
		rows = rows[:maxExamples]
	}
	parts := make([]string, len(rows))
	for i, r := range rows {
		parts[i] = fmt.Sprint(r)
	}
	s := strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	if more > 0 {
		s = strings.Join(parts, ", ") + fmt.Sprintf(" and %d more", more)
	}
	return s
}

// setField assigns a non-timestamp target. status is parsed later by
// matchStatus, so it is kept raw here.
func setField(it *issues.Issue, target, v string) {
	switch target {
	case "identifier":
		it.Identifier = v
	case "title":
		it.Title = v
	case "url":
		it.URL = v
	case "assignee":
		it.Assignee = v
	case "team_key":
		it.TeamKey = v
	case "team_name":
		it.TeamName = v
	case "project_id":
		it.ProjectID = v
	case "project_name":
		it.ProjectName = v
	case "milestone_id":
		it.MilestoneID = v
	case "milestone_name":
		it.MilestoneName = v
	case "issue_type":
		it.IssueType = v
	case "parent_identifier":
		it.ParentIdentifier = v
	case "status":
		it.Status = issues.Status(v)
	case "status_name":
		it.StatusName = v
	case "resolution":
		it.Resolution = v
	}
}

func getField(it issues.Issue, target string) string {
	switch target {
	case "identifier":
		return it.Identifier
	case "title":
		return it.Title
	case "url":
		return it.URL
	case "assignee":
		return it.Assignee
	case "team_key":
		return it.TeamKey
	case "team_name":
		return it.TeamName
	case "project_id":
		return it.ProjectID
	case "project_name":
		return it.ProjectName
	case "milestone_id":
		return it.MilestoneID
	case "milestone_name":
		return it.MilestoneName
	case "issue_type":
		return it.IssueType
	case "parent_identifier":
		return it.ParentIdentifier
	case "status":
		return string(it.Status)
	case "status_name":
		return it.StatusName
	case "resolution":
		return it.Resolution
	}
	return ""
}

func timeField(it *issues.Issue, target string) *time.Time {
	switch target {
	case "created_at":
		return &it.CreatedAt
	case "started_at":
		return &it.StartedAt
	case "completed_at":
		return &it.CompletedAt
	case "canceled_at":
		return &it.CanceledAt
	case "updated_at":
		return &it.UpdatedAt
	}
	return nil
}

func setTime(it *issues.Issue, target string, t time.Time) { *timeField(it, target) = t }

func getTime(it issues.Issue, target string) time.Time { return *timeField(&it, target) }

// closest returns the header nearest to name by edit distance (after
// normalization), if any is within 3 edits; ties go to the first in header
// order.
func closest(name string, header []string) (string, bool) {
	best, bestD := "", 4
	for _, h := range header {
		if d := levenshtein(norm(name), norm(h)); d < bestD {
			best, bestD = h, d
		}
	}
	return strings.TrimSpace(best), bestD <= 3
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
