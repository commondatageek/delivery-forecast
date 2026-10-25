package flagvals

import (
	"strconv"
	"strings"

	"github.com/commondatageek/delivery-forecast/simulate"
)

// Engineers is the flag.Value behind -engineers: either a positive count
// of anonymous equivalent engineers ("3") or a comma-separated list of names
// ("alice,bob,carol") for the same number of named equivalent engineers.
// Names let per-engineer exclusions attach to a slot; they do not have to
// appear in the sample data.
type Engineers struct {
	count int
	names []string
}

func (e *Engineers) String() string {
	switch {
	case len(e.names) > 0:
		return strings.Join(e.names, ",")
	case e.count > 0:
		return strconv.Itoa(e.count)
	default:
		return ""
	}
}

func (e *Engineers) Set(v string) error {
	count, names, err := simulate.ParseEngineers(v)
	if err != nil {
		return err
	}
	e.count, e.names = count, names
	return nil
}

// Count is the number of equivalent engineers (0 when the flag was not given).
func (e *Engineers) Count() int { return e.count }

// Names returns the named engineers, or nil when -engineers was a count.
func (e *Engineers) Names() []string { return e.names }
