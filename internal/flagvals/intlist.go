package flagvals

import (
	"fmt"
	"strconv"
	"strings"
)

// IntList is a flag.Value for a comma-separated list of ints.
type IntList []int

func (p *IntList) String() string {
	parts := make([]string, len(*p))
	for i, v := range *p {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

func (p *IntList) Set(s string) error {
	*p = nil
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		v, err := strconv.Atoi(part)
		if err != nil {
			return fmt.Errorf("invalid integer %q: %w", part, err)
		}
		*p = append(*p, v)
	}
	return nil
}
