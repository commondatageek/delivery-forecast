package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/issues"
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

	var noCreatedAt, noStartedAtCompleted, noAssigneeCompleted int
	for _, it := range items {
		if it.CreatedAt.IsZero() {
			noCreatedAt++
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
		{"count", "ok"},
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

func cmdCheck(args []string) error {
	cmd := flag.NewFlagSet("check", flag.ExitOnError)
	input := addInputFlag(cmd)
	inputFormat := cmd.String("input-format", "", `format of -input when reading stdin ("-"): "csv" or "json"`)
	configFile := addConfigFlag(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *configFile); err != nil {
		return err
	}

	if *input == "" {
		return fmt.Errorf("-input is required")
	}

	var raw []issues.Issue
	var err error
	if *input == "-" {
		if *inputFormat == "" {
			return fmt.Errorf("-input-format is required when reading from stdin (-input -)")
		}
		raw, err = issues.ReadStream(os.Stdin, *inputFormat)
	} else {
		raw, err = loadIssues(context.Background(), *input)
	}
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Read %d issues from %s\n", len(raw), *input)
	for _, r := range checkResults(raw) {
		fmt.Fprintf(os.Stdout, "  %-9s %s\n", r.Command, r.Status)
	}
	return nil
}
