package app

import (
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/issues"
)

// readCSV parses an in-memory CSV fixture.
func readCSV(t *testing.T, csv string) []issues.Issue {
	t.Helper()
	items, err := issues.ReadCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	return items
}
