package report

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/simulate"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func TestCheckExclusions_Invalid(t *testing.T) {
	got := ExclusionsLines(app.CheckExclusions(simulate.Exclusions{}, errors.New("exclusions: global[0]: bad"), nil, day(2026, 10, 7)))
	want := []string{"invalid   exclusions: global[0]: bad"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func mustExclusions(t *testing.T, s string) simulate.Exclusions {
	t.Helper()
	exc, err := simulate.ParseExclusions([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return exc
}

func TestCheckExclusions_Summary(t *testing.T) {
	exc := mustExclusions(t, `{
		"global": ["2026-10-01/2026-10-02", "2026-10-07", "2026-12-25"],
		"engineers": {"alice": ["2026-10-02", "2026-11-02/2026-11-03"], "bob": ["2026-09-01"]}
	}`)
	assignees := map[string]bool{"alice": true, "bob": true}
	got := ExclusionsLines(app.CheckExclusions(exc, nil, assignees, day(2026, 10, 7)))
	want := []string{
		"entries   3 global, 2 engineers (alice: 2, bob: 1)",
		// distinct: 9/1, 10/1, 10/2, 10/7, 11/2, 11/3, 12/25 (10/2 shared) = 7;
		// past (<= today): 9/1, 10/1, 10/2, 10/7 = 4; future: 3.
		"span      2026-09-01 .. 2026-12-25 (4 dates past, 3 future, as of 2026-10-07)",
		"engineers ok",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestCheckExclusions_Empty(t *testing.T) {
	got := ExclusionsLines(app.CheckExclusions(simulate.Exclusions{}, nil, nil, day(2026, 10, 7)))
	want := []string{"entries   0 global, 0 engineers", "span      none", "engineers ok"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCheckExclusions_UnmatchedName(t *testing.T) {
	exc := mustExclusions(t, `{"engineers": {"alice": ["2026-10-02"], "zed": ["2026-10-03"], "yan": ["2026-10-04"]}}`)
	got := ExclusionsLines(app.CheckExclusions(exc, nil, map[string]bool{"alice": true}, day(2026, 10, 7)))
	for _, want := range []string{
		`engineers yan: no assignee named "yan" in the input (fine if yan is given to sim -engineers)`,
		`engineers zed: no assignee named "zed" in the input (fine if zed is given to sim -engineers)`,
	} {
		found := false
		for _, l := range got {
			found = found || l == want
		}
		if !found {
			t.Errorf("missing line %q in:\n%s", want, strings.Join(got, "\n"))
		}
	}
	for _, l := range got {
		if l == "engineers ok" {
			t.Errorf("should not report ok with unmatched names:\n%s", strings.Join(got, "\n"))
		}
	}
}
