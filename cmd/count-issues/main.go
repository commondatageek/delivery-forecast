// Command count-issues reports how many issues are not yet completed, broken
// down by project (and optionally milestone). It reads a single SQLite database
// (the required positional argument) and never modifies it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"forecasting/internal/linear"
	"forecasting/internal/sqlite"
)

const (
	noProjectLabel   = "(No Project)"
	noMilestoneLabel = "(No Milestone)"
	dateLayout       = "2006-01-02"
)

// project holds the milestone breakdown for a single project, plus its total
// and the timestamp of its most recently updated issue.
type project struct {
	Name        string
	TeamName    string
	Total       int
	LastUpdated time.Time
	Milestones  []milestone
}

// MilestoneCount is the number of milestones with a real name (i.e. excluding
// the synthetic "(No Milestone)" bucket).
func (p project) MilestoneCount() int {
	n := 0
	for _, m := range p.Milestones {
		if m.Name != noMilestoneLabel {
			n++
		}
	}
	return n
}

type milestone struct {
	Name  string
	Count int
}

func main() {
	defaultSince := time.Now().AddDate(0, -3, 0).Format(dateLayout)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [-milestones] [-updated-since YYYY-MM-DD] [-teams k1,k2] <db-path>\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Report the number of not-completed issues, grouped by project (and optionally milestone).")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		flag.PrintDefaults()
	}
	milestones := flag.Bool("milestones", false, "Add a per-milestone breakdown under each project")
	updatedSince := flag.String("updated-since", defaultSince, "Only include projects with an issue updated on/after this date (YYYY-MM-DD)")
	var teams linear.KeyList
	flag.Var(&teams, "teams", "Comma-separated team keys to filter by (e.g. ENG,DESIGN); default: all teams")
	flag.Parse()

	since, err := time.Parse(dateLayout, *updatedSince)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid -updated-since %q (want YYYY-MM-DD): %v\n", *updatedSince, err)
		os.Exit(1)
	}

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "error: exactly one argument (the database file) is required")
		flag.Usage()
		os.Exit(1)
	}
	dbPath := flag.Arg(0)

	projects, total, multiTeam, err := loadProjects(dbPath, teams, since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Show team names only when the user didn't scope to specific teams and the
	// database actually holds more than one team.
	showTeams := len(teams) == 0 && multiTeam

	if *milestones {
		outputGrouped(projects, total, showTeams)
	} else {
		outputSummary(projects, total, showTeams)
	}
}

// loadProjects reads the not-completed issue counts and folds them into a
// per-project, per-milestone structure. Projects whose most recently updated
// issue predates since are dropped, and the result is ordered by most recently
// updated issue first. It also reports whether the database holds more than one
// team.
func loadProjects(dbPath string, teamKeys []string, since time.Time) (projects []project, total int, multiTeam bool, err error) {
	store, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, 0, false, fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	ctx := context.Background()

	allTeams, err := store.DistinctTeamKeys(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	multiTeam = len(allTeams) > 1

	counts, err := store.NotCompletedCounts(ctx, teamKeys)
	if err != nil {
		return nil, 0, false, err
	}

	// "Last touched" is measured across ALL issues (including terminal ones),
	// so a project counts as recently active even if the only recent change was
	// to a completed/canceled/duplicate issue. The counts above, by contrast, deliberately
	// include only non-terminal issues.
	activity, err := store.ProjectLastUpdated(ctx, teamKeys)
	if err != nil {
		return nil, 0, false, err
	}
	type key struct{ team, project string }
	lastUpdated := make(map[key]time.Time, len(activity))
	for _, a := range activity {
		lastUpdated[key{team: a.TeamKey, project: a.ProjectName}] = a.LastUpdated
	}

	byProject := make(map[key]*project)
	var order []key
	for _, c := range counts {
		k := key{team: c.TeamKey, project: c.ProjectName}
		p, ok := byProject[k]
		if !ok {
			name := c.ProjectName
			if name == "" {
				name = noProjectLabel
			}
			p = &project{Name: name, TeamName: c.TeamName, LastUpdated: lastUpdated[k]}
			byProject[k] = p
			order = append(order, k)
		}
		msName := c.MilestoneName
		if msName == "" {
			msName = noMilestoneLabel
		}
		p.Milestones = append(p.Milestones, milestone{Name: msName, Count: c.Count})
		p.Total += c.Count
	}

	for _, k := range order {
		p := byProject[k]
		if p.LastUpdated.Before(since) {
			continue
		}
		sortMilestones(p.Milestones)
		projects = append(projects, *p)
		total += p.Total
	}
	sortProjects(projects)

	return projects, total, multiTeam, nil
}

// sortProjects orders projects by most recently updated issue first, breaking
// ties alphabetically by name.
func sortProjects(projects []project) {
	sort.Slice(projects, func(i, j int) bool {
		if !projects[i].LastUpdated.Equal(projects[j].LastUpdated) {
			return projects[i].LastUpdated.After(projects[j].LastUpdated)
		}
		return projects[i].Name < projects[j].Name
	})
}

// sortMilestones orders milestones alphabetically, with "(No Milestone)" last.
func sortMilestones(ms []milestone) {
	sort.Slice(ms, func(i, j int) bool {
		ni, nj := ms[i].Name, ms[j].Name
		if (ni == noMilestoneLabel) != (nj == noMilestoneLabel) {
			return nj == noMilestoneLabel
		}
		return ni < nj
	})
}

func outputSummary(projects []project, total int, showTeams bool) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showTeams {
		fmt.Fprintln(w, "PROJECT\tTEAM\tISSUES\tMILESTONES")
		for _, p := range projects {
			fmt.Fprintf(w, "%s\t%s\t%d\t%d\n", p.Name, p.TeamName, p.Total, p.MilestoneCount())
		}
		fmt.Fprintf(w, "TOTAL\t\t%d\t\n", total)
	} else {
		fmt.Fprintln(w, "PROJECT\tISSUES\tMILESTONES")
		for _, p := range projects {
			fmt.Fprintf(w, "%s\t%d\t%d\n", p.Name, p.Total, p.MilestoneCount())
		}
		fmt.Fprintf(w, "TOTAL\t%d\t\n", total)
	}
	w.Flush()
}

func outputGrouped(projects []project, total int, showTeams bool) {
	for _, p := range projects {
		if showTeams && p.TeamName != "" {
			fmt.Printf("%s [%s] (%d)\n", p.Name, p.TeamName, p.Total)
		} else {
			fmt.Printf("%s (%d)\n", p.Name, p.Total)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, m := range p.Milestones {
			fmt.Fprintf(w, "  %s\t%d\n", m.Name, m.Count)
		}
		w.Flush()
		fmt.Println()
	}
	fmt.Printf("TOTAL: %d not-completed issues\n", total)
}
