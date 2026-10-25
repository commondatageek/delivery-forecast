package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/simulate"
)

// checkExclusionsLines renders the exclusions report lines for check's
// output from the computed report. A report with ParseErr set yields the one
// "invalid" line; otherwise entries, span and engineers lines.
func checkExclusionsLines(r app.ExclusionsReport) []string {
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
	for _, r := range app.CheckReadiness(raw) {
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
		report := app.CheckExclusions(exc, err, app.Assignees(raw), util.LocalDay(nowFunc()))
		fmt.Fprintf(os.Stdout, "Exclusions: %s\n", *exclusions)
		for _, line := range checkExclusionsLines(report) {
			fmt.Fprintf(os.Stdout, "  %s\n", line)
		}
	}
	return nil
}
