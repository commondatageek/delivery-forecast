package report

import (
	"fmt"
	"io"

	"github.com/commondatageek/delivery-forecast/internal/linear"
)

// LinearTeams writes the `linear teams` listing.
func LinearTeams(w io.Writer, teams []linear.Team) {
	fmt.Fprintf(w, "accessible teams (%d):\n", len(teams))
	for _, t := range teams {
		fmt.Fprintf(w, "  %-12s %s\n", t.Key, t.Name)
	}
}
