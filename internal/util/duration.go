package util

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var durationTermRe = regexp.MustCompile(`^([0-9]*\.?[0-9]+)([a-zµ]+)`)

// ParseFlexibleDuration parses a duration string, extending time.ParseDuration
// with a "d" (day) unit so expressions like "1d" or "1d12h" are accepted.
func ParseFlexibleDuration(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if s == "" {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	var total time.Duration
	rest := s
	for rest != "" {
		m := durationTermRe.FindStringSubmatch(rest)
		if m == nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		var unitDur time.Duration
		if m[2] == "d" {
			unitDur = 24 * time.Hour
		} else {
			d, err := time.ParseDuration("1" + m[2])
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q: unknown unit %q", s, m[2])
			}
			unitDur = d
		}
		total += time.Duration(n * float64(unitDur))
		rest = rest[len(m[0]):]
	}
	return total, nil
}
