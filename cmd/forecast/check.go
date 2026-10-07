package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/issues"
	"github.com/commondatageek/delivery-forecast/simulate"
)

// checkResult is one command's readiness verdict against a loaded issue set.
type checkResult struct {
	Command string
	Status  string
}

// checkResults evaluates a loaded issue set against every forecast command's
// data requirements (see DATA_REQUIREMENTS.md's per-command table), reporting
// "ok" or a specific reason some issues will be silently excluded. It never
// returns an error — a bad fit for one command doesn't stop the others from
// being reported — which is what makes it safe to run against an unfamiliar
// export before trying any real command.
func checkResults(items []issues.Issue) []checkResult {
	if len(items) == 0 {
		const msg = "no issues in input"
		return []checkResult{
			{"history", msg}, {"cfd", msg}, {"aging", msg}, {"count", msg}, {"sim", msg},
		}
	}

	var noCreatedAt, noUpdatedAt, noStartedAtCompleted, noAssigneeCompleted int
	for _, it := range items {
		if it.CreatedAt.IsZero() {
			noCreatedAt++
		}
		if it.UpdatedAt.IsZero() {
			noUpdatedAt++
		}
		if !it.IsCompleted() {
			continue
		}
		if it.StartedAt.IsZero() {
			noStartedAtCompleted++
		}
		if it.Assignee == "" {
			noAssigneeCompleted++
		}
	}

	return []checkResult{
		{"history", excludedStatus(noCreatedAt, "issue", "no created_at and will be excluded")},
		{"cfd", excludedStatus(noCreatedAt, "issue", "no created_at and will be excluded")},
		{"aging", excludedStatus(noStartedAtCompleted, "completed issue", "no started_at and will be excluded from the cycle-time distribution")},
		{"count", excludedStatus(noUpdatedAt, "issue", "no updated_at; a project is hidden unless one of its issues was updated since -updated-since")},
		{"sim", excludedStatus(noAssigneeCompleted, "completed issue", "no assignee and will be excluded")},
	}
}

// excludedStatus renders "ok" for n == 0, otherwise a count-and-reason
// sentence, e.g. "12 completed issues have no assignee and will be excluded"
// (n > 1) or "1 completed issue has no assignee and will be excluded" (n == 1).
func excludedStatus(n int, noun, reason string) string {
	if n == 0 {
		return "ok"
	}
	if n == 1 {
		return fmt.Sprintf("1 %s has %s", noun, reason)
	}
	return fmt.Sprintf("%d %ss have %s", n, noun, reason)
}

// checkExclusions renders the exclusions report lines for check's output.
// parseErr non-nil means the file did not parse; the one line returned then
// is the error. assignees is the set of non-empty Assignee values in the
// loaded issues; today is the caller's clock (local midnight). A date counts
// as "past" when it is today or earlier (today's completions are already in
// the default sample window) and "future" when it is after today.
func checkExclusions(exc simulate.Exclusions, parseErr error, assignees map[string]bool, today time.Time) []string {
	if parseErr != nil {
		return []string{fmt.Sprintf("invalid   %v", parseErr)}
	}

	names := exc.Scopes()
	entries := fmt.Sprintf("entries   %d global, %d engineers", len(exc.Global), len(names))
	if len(names) > 0 {
		parts := make([]string, len(names))
		for i, n := range names {
			parts[i] = fmt.Sprintf("%s: %d", n, len(exc.Engineers[n]))
		}
		entries += " (" + strings.Join(parts, ", ") + ")"
	}
	lines := []string{entries}

	distinct := make(map[int64]time.Time)
	for _, scope := range append([]string{""}, names...) {
		for _, d := range exc.Days(scope) {
			distinct[d.Unix()] = d
		}
	}
	if len(distinct) == 0 {
		lines = append(lines, "span      none")
	} else {
		var first, last time.Time
		past := 0
		for _, d := range distinct {
			if first.IsZero() || d.Before(first) {
				first = d
			}
			if last.IsZero() || d.After(last) {
				last = d
			}
			if !d.After(today) {
				past++
			}
		}
		lines = append(lines, fmt.Sprintf("span      %s .. %s (%d dates past, %d future, as of %s)",
			first.Format("2006-01-02"), last.Format("2006-01-02"), past, len(distinct)-past, today.Format("2006-01-02")))
	}

	unmatched := 0
	for _, n := range names {
		if !assignees[n] {
			unmatched++
			lines = append(lines, fmt.Sprintf("engineers %s: no assignee named %q in the input (fine if %s is given to sim -engineers)", n, n, n))
		}
	}
	if unmatched == 0 {
		lines = append(lines, "engineers ok")
	}
	return lines
}

func cmdCheck(args []string) error {
	cmd := flag.NewFlagSet("check", flag.ExitOnError)
	input := addInputFlag(cmd)
	exclusions := cmd.String("exclusions", "", "path to an exclusions JSON file to validate against the input's assignees")
	stdinFormat := addStdinFormatFlag(cmd)
	configFile := addConfigFlag(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *configFile); err != nil {
		return err
	}

	if *input == "" {
		return fmt.Errorf("-input is required")
	}

	raw, err := loadIssues(context.Background(), *input, *stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Read %d issues from %s\n", len(raw), *input)
	for _, r := range checkResults(raw) {
		fmt.Fprintf(os.Stdout, "  %-9s %s\n", r.Command, r.Status)
	}

	if *exclusions != "" {
		// A file that can't be read is a finding about the file, not a failure
		// of check itself, so it prints as an "invalid" line like a parse error.
		var exc simulate.Exclusions
		data, err := os.ReadFile(*exclusions)
		if err == nil {
			exc, err = simulate.ParseExclusions(data)
		}
		assignees := make(map[string]bool)
		for _, it := range raw {
			if it.Assignee != "" {
				assignees[it.Assignee] = true
			}
		}
		fmt.Fprintf(os.Stdout, "Exclusions: %s\n", *exclusions)
		for _, line := range checkExclusions(exc, err, assignees, util.LocalDay(time.Now())) {
			fmt.Fprintf(os.Stdout, "  %s\n", line)
		}
	}
	return nil
}
