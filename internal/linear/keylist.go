package linear

import "strings"

// KeyList is a flag.Value for a comma-separated list of Linear team keys
// (e.g. "ENG,DESIGN"). Keys are upper-cased and trimmed on Set.
type KeyList []string

func (k *KeyList) String() string { return strings.Join(*k, ",") }

func (k *KeyList) Set(val string) error {
	*k = nil
	for _, part := range strings.Split(val, ",") {
		part = strings.ToUpper(strings.TrimSpace(part))
		if part != "" {
			*k = append(*k, part)
		}
	}
	return nil
}
