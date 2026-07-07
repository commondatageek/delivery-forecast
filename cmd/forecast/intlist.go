package main

import (
	"fmt"
	"strconv"
	"strings"
)

// intList is a flag.Value for a comma-separated list of ints.
type intList []int

func (p *intList) String() string {
	parts := make([]string, len(*p))
	for i, v := range *p {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

func (p *intList) Set(s string) error {
	*p = nil
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		v, err := strconv.Atoi(part)
		if err != nil {
			return fmt.Errorf("invalid percentile %q: %w", part, err)
		}
		*p = append(*p, v)
	}
	return nil
}
