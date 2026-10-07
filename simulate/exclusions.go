package simulate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
)

// maxEntryDays caps how long one Entry may span. It exists to catch a typo'd
// year ("2025-01-01/2206-01-02") before it silently zeroes centuries of days.
const maxEntryDays = 366

// Entry is one exclusion: an inclusive [From, To] span of local calendar
// days with an optional free-text Reason. A single day has From == To.
type Entry struct {
	From   time.Time
	To     time.Time
	Reason string
}

// Exclusions lists the calendar dates on which everyone (Global) or a named
// engineer does no work. The same entries apply to the sample window (the day
// is dropped from the throughput sample) and to the forecast horizon (the day
// contributes no completions); which one a date affects depends only on where
// it falls.
type Exclusions struct {
	Global    []Entry            `json:"global"`
	Engineers map[string][]Entry `json:"engineers"`
}

// entryObject is the wire shape of an object-form entry. Pointers distinguish
// "absent" from "empty string", so `{"date": ""}` is reported as an unparseable
// date rather than as a missing key.
type entryObject struct {
	Date   *string `json:"date"`
	From   *string `json:"from"`
	To     *string `json:"to"`
	Reason string  `json:"reason"`
}

// UnmarshalJSON accepts a "YYYY-MM-DD" or "YYYY-MM-DD/YYYY-MM-DD" string, or an
// object with either "date" or both "from" and "to", plus an optional "reason".
func (e *Entry) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	got, err := parseEntry(data)
	if err != nil {
		return fmt.Errorf("%s: %w", raw, err)
	}
	*e = got
	return nil
}

func parseEntry(data []byte) (Entry, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return Entry{}, fmt.Errorf("empty entry")
	}
	switch data[0] {
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return Entry{}, err
		}
		if strings.TrimSpace(s) == "" {
			return Entry{}, fmt.Errorf("empty string")
		}
		from, to, found := strings.Cut(s, "/")
		if !found {
			to = from
		}
		return newEntry(from, to, "")
	case '{':
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		var o entryObject
		if err := dec.Decode(&o); err != nil {
			return Entry{}, err
		}
		switch {
		case o.Date != nil && (o.From != nil || o.To != nil):
			return Entry{}, fmt.Errorf(`"date" cannot be combined with "from"/"to"`)
		case o.Date != nil:
			return newEntry(*o.Date, *o.Date, o.Reason)
		case o.From != nil && o.To != nil:
			return newEntry(*o.From, *o.To, o.Reason)
		case o.From != nil:
			return Entry{}, fmt.Errorf(`"from" requires "to"`)
		case o.To != nil:
			return Entry{}, fmt.Errorf(`"to" requires "from"`)
		default:
			return Entry{}, fmt.Errorf(`object needs "date", or "from" and "to"`)
		}
	default:
		return Entry{}, fmt.Errorf("want a date string or an object")
	}
}

func newEntry(from, to, reason string) (Entry, error) {
	f, err := util.ParseDate(strings.TrimSpace(from))
	if err != nil {
		return Entry{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", from)
	}
	t, err := util.ParseDate(strings.TrimSpace(to))
	if err != nil {
		return Entry{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", to)
	}
	if t.Before(f) {
		return Entry{}, fmt.Errorf("range ends %s before it starts %s", to, from)
	}
	if n := util.DayIndex(t, f) + 1; n > maxEntryDays {
		return Entry{}, fmt.Errorf("range spans %d days; refusing more than %d", n, maxEntryDays)
	}
	return Entry{From: f, To: t, Reason: reason}, nil
}

// entryJSON is the canonical marshaled form, which is also what the run
// manifest records. It always uses from/to so it round-trips through
// UnmarshalJSON.
type entryJSON struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}

// MarshalJSON always emits the object form {"from","to"} plus "reason" when set.
func (e Entry) MarshalJSON() ([]byte, error) {
	return json.Marshal(entryJSON{
		From:   e.From.Format("2006-01-02"),
		To:     e.To.Format("2006-01-02"),
		Reason: e.Reason,
	})
}

// ParseExclusions parses exclusions JSON data, as read from an exclusions
// file by the caller. Every entry must parse: a malformed date, an inverted
// range, an unknown key, or a top-level key other than "global"/"engineers" is
// an error naming where it occurred, rather than being skipped (a silently
// ignored holiday would quietly bias the forecast).
func ParseExclusions(data []byte) (Exclusions, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return Exclusions{}, fmt.Errorf("exclusions: %w", err)
	}
	for k := range top {
		if k != "global" && k != "engineers" {
			return Exclusions{}, fmt.Errorf("exclusions: unknown top-level key %q (want \"global\" or \"engineers\")", k)
		}
	}

	var exc Exclusions
	if raw, ok := top["global"]; ok {
		entries, err := parseEntryList(raw, "global")
		if err != nil {
			return Exclusions{}, err
		}
		exc.Global = entries
	}
	if raw, ok := top["engineers"]; ok {
		var byName map[string]json.RawMessage
		if err := json.Unmarshal(raw, &byName); err != nil {
			return Exclusions{}, fmt.Errorf("exclusions: engineers: %w", err)
		}
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		sort.Strings(names) // deterministic first-error reporting
		for _, name := range names {
			entries, err := parseEntryList(byName[name], fmt.Sprintf("engineers[%q]", name))
			if err != nil {
				return Exclusions{}, err
			}
			if exc.Engineers == nil {
				exc.Engineers = make(map[string][]Entry)
			}
			exc.Engineers[name] = entries
		}
	}
	return exc, nil
}

// parseEntryList decodes a JSON array of entries, prefixing any error with
// where (e.g. `global` or `engineers["alice"]`) and the offending index.
func parseEntryList(raw json.RawMessage, where string) ([]Entry, error) {
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return nil, fmt.Errorf("exclusions: %s: %w", where, err)
	}
	out := make([]Entry, 0, len(elems))
	for i, el := range elems {
		var e Entry
		if err := e.UnmarshalJSON(el); err != nil {
			return nil, fmt.Errorf("exclusions: %s[%d]: %w", where, i, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// Days returns the sorted, de-duplicated local-midnight days for scope
// ("" = global, else an engineer name). It reports only that scope's own
// entries; a Calendar is what layers global days under a name. nil when the
// scope has none.
func (x Exclusions) Days(scope string) []time.Time {
	entries := x.Global
	if scope != "" {
		entries = x.Engineers[scope]
	}
	seen := make(map[int64]bool)
	var days []time.Time
	for _, e := range entries {
		// Walk by calendar date (not +24h) so a DST change inside a range
		// doesn't skip or repeat a day.
		for d := util.LocalDay(e.From); !d.After(e.To); d = d.AddDate(0, 0, 1) {
			if !seen[d.Unix()] {
				seen[d.Unix()] = true
				days = append(days, d)
			}
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	return days
}

// Scopes returns the engineer names that have at least one entry, sorted.
func (x Exclusions) Scopes() []string {
	var names []string
	for name, entries := range x.Engineers {
		if len(entries) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// IsEmpty reports whether no entries exist in any scope.
func (x Exclusions) IsEmpty() bool {
	return len(x.Global) == 0 && len(x.Scopes()) == 0
}
