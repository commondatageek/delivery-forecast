package main

import (
	"fmt"
	"strconv"
	"strings"
)

// engineerSpec is the flag.Value behind -engineers: either a positive count
// of anonymous equivalent engineers ("3") or a comma-separated list of names
// ("alice,bob,carol") for the same number of named equivalent engineers.
// Names let per-engineer exclusions attach to a slot; they do not have to
// appear in the sample data.
type engineerSpec struct {
	count int
	names []string
}

func (e *engineerSpec) String() string {
	switch {
	case len(e.names) > 0:
		return strings.Join(e.names, ",")
	case e.count > 0:
		return strconv.Itoa(e.count)
	default:
		return ""
	}
}

func (e *engineerSpec) Set(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return fmt.Errorf("-engineers: empty value")
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n <= 0 {
			return fmt.Errorf("-engineers: count must be positive, got %d", n)
		}
		e.count, e.names = n, nil
		return nil
	}

	var names []string
	seen := make(map[string]bool)
	for part := range strings.SplitSeq(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if isAllDigits(part) {
			return fmt.Errorf("-engineers: %q looks like a count inside a name list", part)
		}
		if seen[part] {
			return fmt.Errorf("-engineers: duplicate name %q", part)
		}
		seen[part] = true
		names = append(names, part)
	}
	if len(names) == 0 {
		return fmt.Errorf("-engineers: no names in %q", v)
	}
	e.count, e.names = len(names), names
	return nil
}

// Count is the number of equivalent engineers (0 when the flag was not given).
func (e *engineerSpec) Count() int { return e.count }

// Names returns the named engineers, or nil when -engineers was a count.
func (e *engineerSpec) Names() []string { return e.names }

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
