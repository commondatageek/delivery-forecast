package simulate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mustParseExclusions(t *testing.T, s string) Exclusions {
	t.Helper()
	exc, err := ParseExclusions([]byte(s))
	if err != nil {
		t.Fatalf("ParseExclusions(%s): %v", s, err)
	}
	return exc
}

func TestParseExclusions_StringForms(t *testing.T) {
	exc := mustParseExclusions(t, `{
		"global": ["2025-12-25", "2025-12-22/2025-12-26", "2025-12-24"],
		"engineers": {"alice": ["2026-03-02/2026-03-04"], "bob": ["2026-02-16"]}
	}`)

	// Overlapping entries collapse: 12/22..12/26 already covers 12/24 and 12/25.
	wantGlobal := []time.Time{
		day(2025, 12, 22), day(2025, 12, 23), day(2025, 12, 24), day(2025, 12, 25), day(2025, 12, 26),
	}
	if got := exc.Days(""); !reflect.DeepEqual(got, wantGlobal) {
		t.Errorf("Days(\"\") = %v, want %v", got, wantGlobal)
	}
	wantAlice := []time.Time{day(2026, 3, 2), day(2026, 3, 3), day(2026, 3, 4)}
	if got := exc.Days("alice"); !reflect.DeepEqual(got, wantAlice) {
		t.Errorf("Days(alice) = %v, want %v", got, wantAlice)
	}
	if got := exc.Days("bob"); !reflect.DeepEqual(got, []time.Time{day(2026, 2, 16)}) {
		t.Errorf("Days(bob) = %v", got)
	}
	if got := exc.Days("carol"); got != nil {
		t.Errorf("Days(carol) = %v, want nil", got)
	}
	if got, want := exc.Scopes(), []string{"alice", "bob"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Scopes = %v, want %v", got, want)
	}
	if exc.IsEmpty() {
		t.Error("IsEmpty = true for a populated file")
	}
}

func TestParseExclusions_DaysSortedAcrossEntries(t *testing.T) {
	exc := mustParseExclusions(t, `{"global": ["2025-03-05", "2025-03-01", "2025-03-03"]}`)
	want := []time.Time{day(2025, 3, 1), day(2025, 3, 3), day(2025, 3, 5)}
	if got := exc.Days(""); !reflect.DeepEqual(got, want) {
		t.Errorf("Days = %v, want %v", got, want)
	}
}

func TestParseExclusions_ObjectForms(t *testing.T) {
	exc := mustParseExclusions(t, `{
		"global": [
			{"date": "2026-11-26", "reason": "Thanksgiving"},
			{"from": "2026-07-03", "to": "2026-07-06", "reason": "July 4th weekend"}
		],
		"engineers": {"alice": [{"date": "2026-04-10", "reason": "PTO"}, {"from": "2026-05-01", "to": "2026-05-01"}]}
	}`)

	want := []Entry{
		{From: day(2026, 11, 26), To: day(2026, 11, 26), Reason: "Thanksgiving"},
		{From: day(2026, 7, 3), To: day(2026, 7, 6), Reason: "July 4th weekend"},
	}
	if !reflect.DeepEqual(exc.Global, want) {
		t.Errorf("Global = %+v, want %+v", exc.Global, want)
	}
	if got := exc.Engineers["alice"][0].Reason; got != "PTO" {
		t.Errorf("alice[0].Reason = %q, want PTO", got)
	}
	if e := exc.Engineers["alice"][1]; !e.From.Equal(e.To) || e.Reason != "" {
		t.Errorf("alice[1] = %+v, want single day with no reason", e)
	}
	if got := len(exc.Days("")); got != 5 { // 1 + 4
		t.Errorf("len(Days(\"\")) = %d, want 5", got)
	}
}

func TestParseExclusions_Errors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string // substrings the error must contain
	}{
		{"empty string", `{"global": ["2025-01-01", ""]}`, []string{"global[1]", "empty string"}},
		{"bad date", `{"global": ["2025-13-01"]}`, []string{"global[0]", "2025-13-01"}},
		{"bad date in range", `{"global": ["2025-01-01/nope"]}`, []string{"global[0]", `"nope"`}},
		{"inverted range", `{"engineers": {"alice": ["2025-02-10/2025-02-01"]}}`, []string{`engineers["alice"][0]`, "before it starts"}},
		{"object date plus from", `{"global": [{"date": "2025-01-01", "from": "2025-01-01"}]}`, []string{"global[0]", "cannot be combined"}},
		{"from without to", `{"global": [{"from": "2025-01-01"}]}`, []string{"global[0]", `"from" requires "to"`}},
		{"to without from", `{"global": [{"to": "2025-01-01"}]}`, []string{`"to" requires "from"`}},
		{"object with none", `{"global": [{"reason": "x"}]}`, []string{"global[0]", `needs "date"`}},
		{"unknown key", `{"global": [{"date": "2025-01-01", "why": "x"}]}`, []string{"global[0]", "why"}},
		{"too long", `{"global": ["2025-01-01/2026-01-02"]}`, []string{"global[0]", "367 days", "366"}},
		{"not a string or object", `{"global": [42]}`, []string{"global[0]", "42"}},
		{"unknown top-level key", `{"globl": []}`, []string{`"globl"`}},
		{"not json", `not json`, []string{"exclusions:"}},
		{"global not a list", `{"global": "2025-01-01"}`, []string{"global"}},
		{"engineers not a map", `{"engineers": ["alice"]}`, []string{"engineers"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseExclusions([]byte(c.in))
			if err == nil {
				t.Fatalf("ParseExclusions(%s) = nil error", c.in)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestParseExclusions_RangeAtLimit(t *testing.T) {
	// 2025-01-01 .. 2025-12-31 is 365 days; 2024-01-01 .. 2024-12-31 is 366
	// (leap year) and must still be accepted.
	mustParseExclusions(t, `{"global": ["2024-01-01/2024-12-31"]}`)
}

func TestEntry_MarshalRoundTrip(t *testing.T) {
	for _, e := range []Entry{
		{From: day(2026, 7, 3), To: day(2026, 7, 6), Reason: "July 4th weekend"},
		{From: day(2026, 4, 10), To: day(2026, 4, 10)},
	} {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var got Entry
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("Unmarshal(%s): %v", b, err)
		}
		if !reflect.DeepEqual(got, e) {
			t.Errorf("round trip of %s = %+v, want %+v", b, got, e)
		}
	}

	b, _ := json.Marshal(Entry{From: day(2026, 4, 10), To: day(2026, 4, 10)})
	if string(b) != `{"from":"2026-04-10","to":"2026-04-10"}` {
		t.Errorf("marshal without reason = %s", b)
	}
}

func TestParseExclusions_LegacyFileUnchanged(t *testing.T) {
	// The exclusions.json shape shipped before ranges/reasons existed.
	exc := mustParseExclusions(t, `{
		"global": ["2024-12-25"],
		"engineers": {"alice": ["2024-06-17"]}
	}`)
	if got, want := exc.Days(""), []time.Time{day(2024, 12, 25)}; !reflect.DeepEqual(got, want) {
		t.Errorf("Days(\"\") = %v, want %v", got, want)
	}
	if got, want := exc.Days("alice"), []time.Time{day(2024, 6, 17)}; !reflect.DeepEqual(got, want) {
		t.Errorf("Days(alice) = %v, want %v", got, want)
	}
}

func TestExclusions_IsEmpty(t *testing.T) {
	if !(Exclusions{}).IsEmpty() {
		t.Error("zero Exclusions should be empty")
	}
	if !mustParseExclusions(t, `{"global": [], "engineers": {"alice": []}}`).IsEmpty() {
		t.Error("empty lists should be empty")
	}
}
