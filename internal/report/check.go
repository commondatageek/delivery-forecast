package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// Check writes the `check` readiness report: how many issues were read and
// whether each command is supported.
func Check(w io.Writer, n int, input string, readiness []app.CheckResult) {
	fmt.Fprintf(w, "Read %d issues from %s\n", n, input)
	for _, r := range readiness {
		fmt.Fprintf(w, "  %-9s %s\n", r.Command, r.Status)
	}
}

// Exclusions writes the exclusions section of the `check` report.
func Exclusions(w io.Writer, path string, r app.ExclusionsReport) {
	fmt.Fprintf(w, "Exclusions: %s\n", path)
	for _, line := range ExclusionsLines(r) {
		fmt.Fprintf(w, "  %s\n", line)
	}
}

// ExclusionsLines renders the exclusions report lines for check's
// output from the computed report. A report with ParseErr set yields the one
// "invalid" line; otherwise entries, span and engineers lines.
func ExclusionsLines(r app.ExclusionsReport) []string {
	if r.ParseErr != nil {
		return []string{fmt.Sprintf("invalid   %v", r.ParseErr)}
	}

	entries := fmt.Sprintf("entries   %d global, %d engineers", r.GlobalEntries, len(r.Engineers))
	if len(r.Engineers) > 0 {
		parts := make([]string, len(r.Engineers))
		for i, e := range r.Engineers {
			parts[i] = fmt.Sprintf("%s: %d", e.Name, e.Entries)
		}
		entries += " (" + strings.Join(parts, ", ") + ")"
	}
	lines := []string{entries}

	if !r.HasDates {
		lines = append(lines, "span      none")
	} else {
		lines = append(lines, fmt.Sprintf("span      %s .. %s (%d dates past, %d future, as of %s)",
			r.First.Format("2006-01-02"), r.Last.Format("2006-01-02"), r.Past, r.Future, r.Today.Format("2006-01-02")))
	}

	for _, n := range r.UnmatchedNames {
		lines = append(lines, fmt.Sprintf("engineers %s: no assignee named %q in the input (fine if %s is given to sim -engineers)", n, n, n))
	}
	if len(r.UnmatchedNames) == 0 {
		lines = append(lines, "engineers ok")
	}
	return lines
}
