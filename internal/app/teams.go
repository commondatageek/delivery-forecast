package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/commondatageek/delivery-forecast/issues"
)

// DistinctTeamKeys returns the sorted, deduplicated, non-empty team keys
// present in items — the in-memory equivalent of Store.DistinctTeamKeys, so
// the blending-teams warning works identically for every source.
func DistinctTeamKeys(items []issues.Issue) []string {
	seen := make(map[string]bool)
	var keys []string
	for _, it := range items {
		if it.TeamKey == "" || seen[it.TeamKey] {
			continue
		}
		seen[it.TeamKey] = true
		keys = append(keys, it.TeamKey)
	}
	sort.Strings(keys)
	return keys
}

// BlendingTeamsWarning returns the "blending across all teams" warning line, or
// "" when no warning is warranted: either the user scoped explicitly (teams
// non-empty) or the store holds at most one team (allTeams). Kept pure (no I/O)
// so callers that already know the full team set — like count — can reuse the
// exact message without a second DistinctTeamKeys query, and so it is unit
// testable. teams is []string rather than linear.TeamKeyList because
// this package must not import the linear package; call sites pass a TeamKeyList
// unchanged since its underlying type is []string.
func BlendingTeamsWarning(teams []string, allTeams []string) string {
	if len(teams) > 0 || len(allTeams) <= 1 {
		return ""
	}
	return fmt.Sprintf("no -teams filter given; blending data across all %d teams (%s)",
		len(allTeams), strings.Join(allTeams, ", "))
}
