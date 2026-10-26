package params

import (
	"flag"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

func TestInput_Resolve(t *testing.T) {
	tests := []struct {
		name      string
		input, db string
		want      string
		warn      bool
		wantErr   bool
	}{
		{"input set", "issues.csv", "", "issues.csv", false, false},
		{"input wins over db", "issues.csv", "legacy.db", "issues.csv", false, false},
		{"db fallback", "", "legacy.db", "legacy.db", true, false},
		{"neither set", "", "", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := Input{Path: &tt.input, DB: &tt.db}
			got, warnings, err := in.Resolve()
			if tt.wantErr {
				wantFieldError(t, err, "-input is required", "input")
				return
			}
			if err != nil || got != tt.want || (len(warnings) > 0) != tt.warn {
				t.Errorf("got %q %v %v", got, warnings, err)
			}
		})
	}
}

func TestAging_Resolve(t *testing.T) {
	fs := flag.NewFlagSet("aging", flag.ContinueOnError)
	c := NewAging(fs)
	parse(t, fs, "-input", "x.csv", "-min-cycle-time", "1h", "-percentile", "90", "-show-completed", "-teams", "eng")
	r, err := c.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	o := r.Request.Options
	if o.MinCycleTime != time.Hour || o.Percentile != 90 || !r.Request.ShowCompleted || !reflect.DeepEqual([]string(o.Teams), []string{"ENG"}) {
		t.Errorf("request = %+v", r.Request)
	}
	if !r.Request.Today.Equal(day(2026, 3, 10)) {
		t.Errorf("Today = %v", r.Request.Today)
	}

	for _, tc := range []struct {
		args   []string
		prefix string
		field  string
	}{
		{[]string{"-min-cycle-time", "bogus"}, `invalid -min-cycle-time "bogus":`, "min-cycle-time"},
		{[]string{"-sample-end", "bogus"}, `invalid -sample-end "bogus":`, "sample-end"},
		{[]string{"-sample-start", "bogus"}, `invalid -sample-start "bogus":`, "sample-start"},
		{[]string{"-percentile", "0"}, "", "percentile"},
	} {
		fs := flag.NewFlagSet("aging", flag.ContinueOnError)
		c := NewAging(fs)
		parse(t, fs, append([]string{"-input", "x"}, tc.args...)...)
		_, err := c.Resolve(fixedNow)
		fe, ok := err.(*app.FieldError)
		if !ok || !strings.HasPrefix(fe.Msg, tc.prefix) || fe.Fields[0] != tc.field {
			t.Errorf("args %v: error = %v", tc.args, err)
		}
	}
}

func TestCFD_Resolve(t *testing.T) {
	fs := flag.NewFlagSet("cfd", flag.ContinueOnError)
	c := NewCFD(fs)
	parse(t, fs, "-input", "x.csv", "-start", "2026-01-01", "-end", "2026-02-01")
	r, err := c.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Request.Start.Equal(day(2026, 1, 1)) || !r.Request.End.Equal(day(2026, 2, 1)) {
		t.Errorf("request = %+v", r.Request)
	}
	for field, flagName := range map[string]string{"end": "-end", "start": "-start"} {
		fs := flag.NewFlagSet("cfd", flag.ContinueOnError)
		c := NewCFD(fs)
		parse(t, fs, "-input", "x", flagName, "bogus")
		_, err := c.Resolve(fixedNow)
		wantFieldError(t, err, "invalid "+flagName+` "bogus": `+strings.TrimPrefix(err.Error(), "invalid "+flagName+` "bogus": `), field)
		if !strings.HasPrefix(err.Error(), "invalid "+flagName+` "bogus":`) {
			t.Errorf("error = %v", err)
		}
	}
}

func TestCount_Resolve(t *testing.T) {
	fs := flag.NewFlagSet("count", flag.ContinueOnError)
	c := NewCount(fs, fixedNow)
	if got := *c.UpdatedSince; got != "2025-12-10" {
		t.Errorf("default -updated-since = %q", got)
	}
	parse(t, fs, "-input", "x.csv", "-milestones")
	r, err := c.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Request.Since.Equal(day(2025, 12, 10)) || !*c.Milestones {
		t.Errorf("request = %+v", r.Request)
	}

	fs = flag.NewFlagSet("count", flag.ContinueOnError)
	c = NewCount(fs, fixedNow)
	parse(t, fs, "-input", "x", "-updated-since", "bogus")
	_, err = c.Resolve(fixedNow)
	if fe, ok := err.(*app.FieldError); !ok || !strings.HasPrefix(fe.Msg, `invalid -updated-since "bogus":`) || fe.Fields[0] != "updated-since" {
		t.Errorf("error = %v", err)
	}
}

func TestHistory_Resolve(t *testing.T) {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	c := NewHistory(fs)
	if fs.Lookup("db") != nil {
		t.Error("history must not register -db")
	}
	parse(t, fs, "-input", "x.csv", "-window", "14", "-start", "2026-01-01")
	r, err := c.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if r.Request.WindowDays != 14 || !r.Request.Start.Equal(day(2026, 1, 1)) || !r.Request.End.Equal(day(2026, 3, 10)) {
		t.Errorf("request = %+v", r.Request)
	}

	// The format check runs before the input check.
	fs = flag.NewFlagSet("history", flag.ContinueOnError)
	c = NewHistory(fs)
	parse(t, fs, "-format", "xml")
	_, err = c.Resolve(fixedNow)
	wantFieldError(t, err, `-format must be "csv", "json", or "text"`, "format")

	fs = flag.NewFlagSet("history", flag.ContinueOnError)
	c = NewHistory(fs)
	parse(t, fs)
	_, err = c.Resolve(fixedNow)
	wantFieldError(t, err, "-input is required", "input")

	for field, flagName := range map[string]string{"end": "-end", "start": "-start"} {
		fs := flag.NewFlagSet("history", flag.ContinueOnError)
		c := NewHistory(fs)
		parse(t, fs, "-input", "x", flagName, "bogus")
		_, err := c.Resolve(fixedNow)
		if fe, ok := err.(*app.FieldError); !ok || !strings.HasPrefix(fe.Msg, "invalid "+flagName+` "bogus":`) || fe.Fields[0] != field {
			t.Errorf("%s: error = %v", flagName, err)
		}
	}
}

func TestCheck_Resolve(t *testing.T) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	c := NewCheck(fs)
	parse(t, fs, "-input", "x.csv", "-exclusions", "e.json")
	r, err := c.Resolve()
	if err != nil || r.InputPath != "x.csv" || r.Request.Exclusions != "e.json" {
		t.Errorf("got %+v %v", r, err)
	}
	fs = flag.NewFlagSet("check", flag.ContinueOnError)
	c = NewCheck(fs)
	parse(t, fs)
	_, err = c.Resolve()
	wantFieldError(t, err, "-input is required", "input")
}

func TestLinear_Resolve(t *testing.T) {
	fs := flag.NewFlagSet("linear sync", flag.ContinueOnError)
	c := NewLinearSync(fs)
	parse(t, fs)
	_, err := c.Resolve()
	wantFieldError(t, err, "-db is required", "db")

	fs = flag.NewFlagSet("linear sync", flag.ContinueOnError)
	c = NewLinearSync(fs)
	parse(t, fs, "-db", "l.db", "-teams", "eng,data", "-full-reload")
	r, err := c.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if r.InputPath != "l.db" || !r.Request.FullReload || r.Request.AllTeams || !reflect.DeepEqual([]string(r.Request.Teams), []string{"ENG", "DATA"}) {
		t.Errorf("got %+v", r)
	}

	fs = flag.NewFlagSet("linear teams", flag.ContinueOnError)
	lt := NewLinearTeams(fs)
	if lt.Config == nil || fs.Lookup("config") == nil {
		t.Error("linear teams must register -config")
	}
}

func TestResolve_SetEqualsParse_Others(t *testing.T) {
	// aging: -teams must upper-case through fs.Set as it does through Parse.
	a1 := flag.NewFlagSet("a", flag.ContinueOnError)
	ca1 := NewAging(a1)
	parse(t, a1, "-input", "x.csv", "-teams", "eng")
	ra1, err := ca1.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	a2 := flag.NewFlagSet("a", flag.ContinueOnError)
	ca2 := NewAging(a2)
	for _, kv := range [][2]string{{"input", "x.csv"}, {"teams", "eng"}} {
		if err := a2.Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	ra2, err := ca2.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ra1.Request, ra2.Request) {
		t.Errorf("aging: Parse and Set disagree:\n%+v\n%+v", ra1.Request, ra2.Request)
	}
	if !reflect.DeepEqual([]string(ra2.Request.Options.Teams), []string{"ENG"}) {
		t.Errorf("teams = %v, want [ENG]", ra2.Request.Options.Teams)
	}

	h1 := flag.NewFlagSet("h", flag.ContinueOnError)
	ch1 := NewHistory(h1)
	parse(t, h1, "-input", "x.csv", "-window", "7", "-project", "P")
	rh1, err := ch1.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	h2 := flag.NewFlagSet("h", flag.ContinueOnError)
	ch2 := NewHistory(h2)
	for _, kv := range [][2]string{{"input", "x.csv"}, {"window", "7"}, {"project", "P"}} {
		if err := h2.Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	rh2, err := ch2.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rh1.Request, rh2.Request) {
		t.Errorf("history: Parse and Set disagree")
	}
}
