package flagvals

import "strings"

// StringList is a flag.Value for a comma-separated list of strings.
type StringList []string

func (s *StringList) String() string {
	return strings.Join(*s, ",")
}

func (s *StringList) Set(val string) error {
	*s = nil
	for part := range strings.SplitSeq(val, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}
