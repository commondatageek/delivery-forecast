// Package importmap turns a tracker's CSV export into issues.Issue records
// using a declarative mapping file. It is pure: callers do the IO.
//
// A mapping (Parse) names, for each v2 issue column, which export column(s)
// fill it, how the export's workflow maps onto issues.Status, which
// lifecycle timestamps are kept for which statuses, and which rows to drop.
// Apply checks every rule against the export's header and rows and refuses
// to guess: anything that doesn't fit stops the import with an error naming
// the rule, the column and example values. See docs/IMPORT_MAPPING.md.
package importmap

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/commondatageek/delivery-forecast/issues"

	"gopkg.in/yaml.v3"
)

// Mapping is a parsed, validated mapping file.
type Mapping struct {
	Version     int
	Source      string
	TimeFormats []string
	Location    *time.Location
	Columns     []ColumnRule    // YAML order
	Status      *StatusRule     // nil when absent
	Timestamps  []TimestampRule // YAML order
	Exclude     []ExcludeRule   // YAML order
	Detect      []string
}

// ColumnRule says where one v2 column's value comes from.
type ColumnRule struct {
	Target  string   // v2 column name
	Headers []string // one for `column`/plain, several for `columns`; empty for `value`
	Pick    string   // "", "first", "last", "join"
	Value   string   // used when Headers is empty
}

// StatusRule maps each row onto an issues.Status (see Apply for the order
// the rules are tried in).
type StatusRule struct {
	Column              string        // "" when only overrides are used
	Values              []StatusMatch // YAML order
	Overrides           []StatusMatch
	ResolutionOverrides []StatusMatch
}

// StatusMatch is one "<value>: <status>" entry of a StatusRule map.
type StatusMatch struct {
	Raw    string // as written in the YAML
	Status issues.Status
}

// TimestampRule fills a lifecycle timestamp from the first non-empty of
// Headers, but only on rows whose mapped status is in When.
type TimestampRule struct {
	Target  string
	Headers []string
	When    []issues.Status
}

// ExcludeRule drops rows whose Header value is one of Values.
type ExcludeRule struct {
	Header string
	Values []string
}

// targets lists every v2 column a mapping may fill, in issues.Columns()
// order. source is absent: it comes from the mapping's top-level key.
var targets = func() []string {
	var out []string
	for _, c := range issues.Columns() {
		if c != "source" {
			out = append(out, c)
		}
	}
	return out
}()

// lifecycleTargets may be mapped under timestamps (conditionally) as well as
// under columns (unconditionally), but not both.
var lifecycleTargets = []string{"started_at", "completed_at", "canceled_at"}

var sourcePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

const bom = "\uFEFF"

// norm is the one comparison rule for header names and matched values:
// surrounding whitespace and a leading byte order mark are ignored, and
// case doesn't matter. Nothing fuzzier.
func norm(s string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(s, bom)))
}

func isTimestamp(target string) bool { return strings.HasSuffix(target, "_at") }

// statusWords is the list every "not a status" error offers.
const statusWords = "backlog, started, completed or canceled"

// rawMapping is the top level of a mapping file. Rule fields stay yaml.Node
// so they can be interpreted by shape, in YAML order.
type rawMapping struct {
	Version    yaml.Node `yaml:"version"`
	Source     yaml.Node `yaml:"source"`
	Format     yaml.Node `yaml:"format"`
	TimeFormat yaml.Node `yaml:"time_format"`
	Timezone   yaml.Node `yaml:"timezone"`
	Columns    yaml.Node `yaml:"columns"`
	Status     yaml.Node `yaml:"status"`
	Timestamps yaml.Node `yaml:"timestamps"`
	Exclude    yaml.Node `yaml:"exclude"`
	Detect     yaml.Node `yaml:"detect"`
}

// present reports whether a key was given at all.
func present(n *yaml.Node) bool { return n.Kind != 0 }

// Parse decodes and validates a mapping file. Every problem found is
// reported, one per line, each starting with its YAML path.
func Parse(data []byte) (*Mapping, error) {
	var raw rawMapping
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		var te *yaml.TypeError
		if errors.As(err, &te) {
			return nil, fmt.Errorf("mapping: %s", strings.Join(te.Errors, "; "))
		}
		return nil, fmt.Errorf("mapping: %s", strings.TrimPrefix(err.Error(), "yaml: "))
	}

	p := &parser{m: &Mapping{Location: time.Local}}
	p.version(&raw.Version)
	p.source(&raw.Source)
	p.format(&raw.Format)
	p.timezone(&raw.Timezone)
	p.columns(&raw.Columns)
	p.status(&raw.Status)
	p.timestamps(&raw.Timestamps)
	p.exclude(&raw.Exclude)
	p.m.Detect = p.stringList(&raw.Detect, "detect")
	p.timeFormat(&raw.TimeFormat)

	if len(p.problems) > 0 {
		return nil, errors.New(strings.Join(p.problems, "\n"))
	}
	return p.m, nil
}

// parser accumulates the Mapping and every problem found while building it.
type parser struct {
	m        *Mapping
	problems []string
}

func (p *parser) errf(path, format string, args ...any) {
	p.problems = append(p.problems, path+": "+fmt.Sprintf(format, args...))
}

// scalar returns n's text, or reports that a single value was expected.
func (p *parser) scalar(n *yaml.Node, path string) (string, bool) {
	if n.Kind != yaml.ScalarNode {
		p.errf(path, "expected a single value")
		return "", false
	}
	return n.Value, true
}

// stringList accepts a sequence of scalars (or a single scalar, read as a
// one-element list). An absent node yields nil.
func (p *parser) stringList(n *yaml.Node, path string) []string {
	switch n.Kind {
	case 0:
		return nil
	case yaml.ScalarNode:
		return []string{n.Value}
	case yaml.SequenceNode:
		out := make([]string, 0, len(n.Content))
		for i, c := range n.Content {
			if v, ok := p.scalar(c, fmt.Sprintf("%s[%d]", path, i)); ok {
				out = append(out, v)
			}
		}
		return out
	}
	p.errf(path, "expected a list")
	return nil
}

// pairs walks a mapping node's key/value pairs in YAML order.
func (p *parser) pairs(n *yaml.Node, path string) [][2]*yaml.Node {
	if n.Kind != yaml.MappingNode {
		p.errf(path, "expected a mapping")
		return nil
	}
	var out [][2]*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
	}
	return out
}

// fields checks a mapping node's keys against allowed and returns them by
// name. yaml.v3's Node.Decode can't reject unknown keys, so nested blocks are
// checked here.
func (p *parser) fields(n *yaml.Node, path string, allowed ...string) map[string]*yaml.Node {
	out := make(map[string]*yaml.Node)
	for _, kv := range p.pairs(n, path) {
		k := kv[0].Value
		if !slices.Contains(allowed, k) {
			p.errf(path, "unknown key %q (expected %s)", k, strings.Join(allowed, ", "))
			continue
		}
		out[k] = kv[1]
	}
	return out
}

func (p *parser) version(n *yaml.Node) {
	if !present(n) {
		p.errf("version", "required (use 1)")
		return
	}
	var v int
	if err := n.Decode(&v); err != nil {
		p.errf("version", "must be a number (use 1)")
		return
	}
	if v != 1 {
		p.errf("version", "unsupported mapping version %d (this forecast understands 1)", v)
		return
	}
	p.m.Version = v
}

func (p *parser) source(n *yaml.Node) {
	if !present(n) || (n.Kind == yaml.ScalarNode && n.Value == "") {
		p.errf("source", `required (e.g. "jira")`)
		return
	}
	v, ok := p.scalar(n, "source")
	if !ok {
		return
	}
	if !sourcePattern.MatchString(v) {
		p.errf("source", "%q must be lowercase letters, digits and dashes", v)
		return
	}
	p.m.Source = v
}

func (p *parser) format(n *yaml.Node) {
	if !present(n) {
		return
	}
	if v, ok := p.scalar(n, "format"); ok && v != "csv" {
		p.errf("format", `unsupported format %q (only "csv")`, v)
	}
}

func (p *parser) timezone(n *yaml.Node) {
	if !present(n) {
		return
	}
	v, ok := p.scalar(n, "timezone")
	if !ok {
		return
	}
	loc, err := time.LoadLocation(v)
	if err != nil || v == "" {
		p.errf("timezone", "unknown time zone %q", v)
		return
	}
	p.m.Location = loc
}

// roundTripProbe is the instant every time_format layout must be able to
// format and parse back to the same date.
var roundTripProbe = time.Date(2026, 10, 22, 19, 55, 0, 0, time.UTC)

// timeFormat runs last: whether it is required depends on the targets.
func (p *parser) timeFormat(n *yaml.Node) {
	p.m.TimeFormats = p.stringList(n, "time_format")
	for _, l := range p.m.TimeFormats {
		// The date must survive formatting and parsing back; a layout with
		// no date fields parses to year 0, not the zero time, so compare.
		t, err := time.Parse(l, roundTripProbe.Format(l))
		py, pm, pd := roundTripProbe.Date()
		if y, m, d := t.Date(); err != nil || y != py || m != pm || d != pd {
			p.errf("time_format", "%q does not round-trip a timestamp", l)
		}
	}
	if len(p.m.TimeFormats) > 0 {
		return
	}
	for _, c := range p.m.Columns {
		if isTimestamp(c.Target) {
			p.errf("time_format", "required because columns.%s is a timestamp", c.Target)
			return
		}
	}
	if len(p.m.Timestamps) > 0 {
		p.errf("time_format", "required because timestamps.%s is a timestamp", p.m.Timestamps[0].Target)
	}
}

func (p *parser) columns(n *yaml.Node) {
	if !present(n) {
		p.errf("columns", "required; columns.identifier at least")
		return
	}
	seen := make(map[string]bool)
	for _, kv := range p.pairs(n, "columns") {
		target := kv[0].Value
		path := "columns." + target
		if !slices.Contains(targets, target) {
			p.errf(path, "unknown target; valid targets: %s", strings.Join(targets, ", "))
			continue
		}
		if seen[target] {
			p.errf(path, "mapped twice")
			continue
		}
		seen[target] = true
		if r, ok := p.columnRule(kv[1], path); ok {
			r.Target = target
			p.m.Columns = append(p.m.Columns, r)
		}
	}
	if !seen["identifier"] {
		p.errf("columns.identifier", "required")
	}
}

func (p *parser) columnRule(n *yaml.Node, path string) (ColumnRule, bool) {
	switch n.Kind {
	case yaml.ScalarNode:
		if strings.TrimSpace(n.Value) == "" {
			p.errf(path, "empty header name")
			return ColumnRule{}, false
		}
		return ColumnRule{Headers: []string{n.Value}}, true
	case yaml.MappingNode:
	default:
		p.errf(path, "expected a header name, or one of {column: …}, {columns: […]}, {value: …}")
		return ColumnRule{}, false
	}
	f := p.fields(n, path, "column", "columns", "value", "pick")
	shapes := 0
	for _, k := range []string{"column", "columns", "value"} {
		if f[k] != nil {
			shapes++
		}
	}
	if shapes != 1 {
		p.errf(path, "use exactly one of column, columns, value")
		return ColumnRule{}, false
	}
	var r ColumnRule
	if pick := f["pick"]; pick != nil {
		if f["column"] == nil {
			p.errf(path+".pick", "only goes with column")
			return ColumnRule{}, false
		}
		v, ok := p.scalar(pick, path+".pick")
		if !ok {
			return ColumnRule{}, false
		}
		if v != "first" && v != "last" && v != "join" {
			p.errf(path+".pick", "must be first, last or join")
			return ColumnRule{}, false
		}
		r.Pick = v
	}
	switch {
	case f["column"] != nil:
		v, ok := p.scalar(f["column"], path+".column")
		if !ok {
			return ColumnRule{}, false
		}
		r.Headers = []string{v}
	case f["columns"] != nil:
		r.Headers = p.stringList(f["columns"], path+".columns")
		if len(r.Headers) == 0 {
			p.errf(path+".columns", "needs at least one header")
			return ColumnRule{}, false
		}
	default:
		v, ok := p.scalar(f["value"], path+".value")
		if !ok {
			return ColumnRule{}, false
		}
		r.Value = v
	}
	return r, true
}

// mapped reports whether target is filled under columns.
func (m *Mapping) mapped(target string) bool {
	return slices.ContainsFunc(m.Columns, func(c ColumnRule) bool { return c.Target == target })
}

func (p *parser) status(n *yaml.Node) {
	if !present(n) {
		return
	}
	f := p.fields(n, "status", "column", "values", "overrides", "resolution_overrides")
	if p.m.mapped("status") {
		p.errf("status", "can't be used together with columns.status")
		return
	}
	r := &StatusRule{}
	if c := f["column"]; c != nil {
		r.Column, _ = p.scalar(c, "status.column")
	}
	if (f["column"] != nil) != (f["values"] != nil) {
		p.errf("status", "values and column go together")
	}
	r.Values = p.statusMap(f["values"], "status.values")
	r.Overrides = p.statusMap(f["overrides"], "status.overrides")
	r.ResolutionOverrides = p.statusMap(f["resolution_overrides"], "status.resolution_overrides")
	if f["overrides"] != nil && !p.m.mapped("status_name") {
		p.errf("status.overrides", "needs columns.status_name")
	}
	if f["resolution_overrides"] != nil && !p.m.mapped("resolution") {
		p.errf("status.resolution_overrides", "needs columns.resolution")
	}
	if f["column"] == nil && f["values"] == nil && f["overrides"] == nil && f["resolution_overrides"] == nil {
		p.errf("status", "needs column and values, or overrides")
	}
	p.m.Status = r
}

// statusMap reads a "<value>: <status>" map in YAML order, rejecting keys
// that are the same value once normalized.
func (p *parser) statusMap(n *yaml.Node, path string) []StatusMatch {
	if n == nil {
		return nil
	}
	var out []StatusMatch
	firstSpelling := make(map[string]string)
	for _, kv := range p.pairs(n, path) {
		key := kv[0].Value
		if prev, dup := firstSpelling[norm(key)]; dup {
			p.errf(path, "%q and %q are the same value", prev, key)
			continue
		}
		firstSpelling[norm(key)] = key
		v, ok := p.scalar(kv[1], fmt.Sprintf("%s[%q]", path, key))
		if !ok {
			continue
		}
		st, ok := issues.ParseStatus(v)
		if !ok {
			p.errf(fmt.Sprintf("%s[%q]", path, key), "%q is not a status; use %s", v, statusWords)
			continue
		}
		out = append(out, StatusMatch{Raw: key, Status: st})
	}
	return out
}

func (p *parser) timestamps(n *yaml.Node) {
	if !present(n) {
		return
	}
	seen := make(map[string]bool)
	for _, kv := range p.pairs(n, "timestamps") {
		target := kv[0].Value
		path := "timestamps." + target
		if !slices.Contains(lifecycleTargets, target) {
			p.errf(path, "must be one of %s", strings.Join(lifecycleTargets, ", "))
			continue
		}
		if seen[target] {
			p.errf(path, "mapped twice")
			continue
		}
		seen[target] = true
		if p.m.mapped(target) {
			p.errf(path, "also mapped under columns")
			continue
		}
		f := p.fields(kv[1], path, "columns", "when")
		r := TimestampRule{Target: target}
		if f["columns"] == nil {
			p.errf(path+".columns", "required")
		} else if r.Headers = p.stringList(f["columns"], path+".columns"); len(r.Headers) == 0 {
			p.errf(path+".columns", "needs at least one header")
		}
		if f["when"] == nil {
			p.errf(path+".when", "required")
			continue
		}
		for _, w := range p.stringList(f["when"], path+".when") {
			st, ok := issues.ParseStatus(w)
			if !ok {
				p.errf(path+".when", "%q is not a status; use %s", w, statusWords)
				continue
			}
			r.When = append(r.When, st)
		}
		if p.m.Status == nil && !p.m.mapped("status") {
			p.errf(path+".when", "needs a status rule (a status block or columns.status)")
			continue
		}
		p.m.Timestamps = append(p.m.Timestamps, r)
	}
}

func (p *parser) exclude(n *yaml.Node) {
	if !present(n) {
		return
	}
	for _, kv := range p.pairs(n, "exclude") {
		header := kv[0].Value
		vals := p.stringList(kv[1], fmt.Sprintf("exclude[%q]", header))
		if len(vals) == 0 {
			p.errf(fmt.Sprintf("exclude[%q]", header), "needs at least one value")
			continue
		}
		p.m.Exclude = append(p.m.Exclude, ExcludeRule{Header: header, Values: vals})
	}
}
