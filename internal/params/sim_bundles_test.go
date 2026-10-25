package params

import (
	"errors"
	"flag"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

var fixedNow = time.Date(2026, 3, 10, 9, 30, 0, 0, time.Local)

// wantFieldError asserts err is a *app.FieldError with exactly msg and fields.
func wantFieldError(t *testing.T, err error, msg string, fields ...string) {
	t.Helper()
	var fe *app.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("error = %v (%T), want *app.FieldError", err, err)
	}
	if fe.Msg != msg {
		t.Errorf("message = %q, want %q", fe.Msg, msg)
	}
	if !reflect.DeepEqual(fe.Fields, fields) {
		t.Errorf("fields = %v, want %v", fe.Fields, fields)
	}
}

func parse(t *testing.T, fs *flag.FlagSet, args ...string) {
	t.Helper()
	fs.SetOutput(&strings.Builder{})
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func TestSimItems_Resolve(t *testing.T) {
	fs := flag.NewFlagSet("sim items", flag.ContinueOnError)
	c := NewSimItems(fs)
	parse(t, fs, "-input", "x.csv", "-engineers", "3", "-days", "30", "-random-seed", "7", "-confidence", "50,85")
	r, err := c.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	req := r.Request
	if r.InputPath != "x.csv" || len(r.Warnings) != 0 {
		t.Errorf("input = %q warnings = %v", r.InputPath, r.Warnings)
	}
	if req.Engineers != 3 || req.Days != 30 || req.Seed != 7 || !reflect.DeepEqual(req.Confidences, []int{50, 85}) {
		t.Errorf("request = %+v", req)
	}
	if !req.TargetStart.Equal(day(2026, 3, 11)) {
		t.Errorf("TargetStart = %v, want tomorrow", req.TargetStart)
	}

	for _, tc := range []struct {
		name   string
		args   []string
		msg    string
		fields []string
	}{
		{"bad sample-start", []string{"-input", "x", "-engineers", "1", "-days", "3", "-sample-start", "soon"}, "invalid -sample-start date:", []string{"sample-start"}},
		{"bad sample-end", []string{"-input", "x", "-engineers", "1", "-days", "3", "-sample-end", "later"}, "invalid -sample-end date:", []string{"sample-end"}},
		{"bad target end", []string{"-input", "x", "-engineers", "1", "-target-end-date", "later"}, "invalid -target-end-date:", []string{"target-end-date"}},
		{"no input", []string{"-engineers", "1", "-days", "3"}, "-input is required", []string{"input"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("sim items", flag.ContinueOnError)
			c := NewSimItems(fs)
			parse(t, fs, tc.args...)
			_, err := c.Resolve(fixedNow)
			var fe *app.FieldError
			if !errors.As(err, &fe) || !strings.HasPrefix(fe.Msg, tc.msg) || !reflect.DeepEqual(fe.Fields, tc.fields) {
				t.Fatalf("error = %v, want prefix %q fields %v", err, tc.msg, tc.fields)
			}
		})
	}

	t.Run("days and target end together", func(t *testing.T) {
		fs := flag.NewFlagSet("sim items", flag.ContinueOnError)
		c := NewSimItems(fs)
		parse(t, fs, "-input", "x", "-engineers", "1", "-days", "3", "-target-end-date", "2026-04-01")
		if _, err := c.Resolve(fixedNow); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("no mode", func(t *testing.T) {
		fs := flag.NewFlagSet("sim items", flag.ContinueOnError)
		c := NewSimItems(fs)
		parse(t, fs, "-input", "x", "-days", "3")
		if _, err := c.Resolve(fixedNow); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestSimDays_Resolve(t *testing.T) {
	fs := flag.NewFlagSet("sim days", flag.ContinueOnError)
	c := NewSimDays(fs)
	parse(t, fs, "-input", "x.csv", "-whole-team", "-items", "13,12")
	r, err := c.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Request.Items, []int{13, 12}) || !r.Request.WholeTeam {
		t.Errorf("request = %+v", r.Request)
	}

	fs = flag.NewFlagSet("sim days", flag.ContinueOnError)
	c = NewSimDays(fs)
	parse(t, fs, "-input", "x", "-whole-team", "-items", "3", "-target-start-date", "soon")
	_, err = c.Resolve(fixedNow)
	var fe *app.FieldError
	if !errors.As(err, &fe) || !strings.HasPrefix(fe.Msg, "invalid -target-start-date:") || fe.Fields[0] != "target-start-date" {
		t.Errorf("error = %v", err)
	}

	fs = flag.NewFlagSet("sim days", flag.ContinueOnError)
	c = NewSimDays(fs)
	parse(t, fs, "-input", "x", "-whole-team")
	if _, err := c.Resolve(fixedNow); err == nil {
		t.Error("missing -items should fail validation")
	}
}

func TestSimProbability_Resolve(t *testing.T) {
	fs := flag.NewFlagSet("sim probability", flag.ContinueOnError)
	c := NewSimProbability(fs)
	parse(t, fs, "-input", "x.csv", "-engineers", "alice,bob", "-target-end-date", "2026-04-01", "-items", "20")
	r, err := c.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if r.Request.Items != 20 || !reflect.DeepEqual(r.Request.EngineerNames, []string{"alice", "bob"}) || !r.Request.TargetEnd.Equal(day(2026, 4, 1)) {
		t.Errorf("request = %+v", r.Request)
	}

	fs = flag.NewFlagSet("sim probability", flag.ContinueOnError)
	c = NewSimProbability(fs)
	parse(t, fs, "-input", "x", "-engineers", "1", "-days", "3", "-target-end-date", "nope")
	_, err = c.Resolve(fixedNow)
	var fe *app.FieldError
	if !errors.As(err, &fe) || fe.Fields[0] != "target-end-date" {
		t.Errorf("error = %v", err)
	}
}

func TestSimBacktest_Resolve(t *testing.T) {
	fs := flag.NewFlagSet("sim backtest", flag.ContinueOnError)
	c := NewSimBacktest(fs)
	parse(t, fs, "-input", "x.csv", "-whole-team", "-project", "P", "-target-end-date", "2026-05-01", "-replay-start-date", "2026-01-05")
	r, err := c.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if r.Request.Project != "P" || !r.Request.Today.Equal(day(2026, 3, 10)) || !r.Request.ReplayStart.Equal(day(2026, 1, 5)) {
		t.Errorf("request = %+v", r.Request)
	}
	if got := fs.Lookup("simulations").Usage; got != "number of Monte Carlo simulations to run per backtested day" {
		t.Errorf("simulations usage = %q", got)
	}

	cases := []struct {
		name, msg string
		field     string
		args      []string
	}{
		{"sample-start has no 'date'", "invalid -sample-start:", "sample-start", []string{"-sample-start", "x"}},
		{"replay start", "invalid -replay-start-date:", "replay-start-date", []string{"-replay-start-date", "x"}},
		{"format", `-format must be "text" or "csv"`, "format", []string{"-format", "json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("sim backtest", flag.ContinueOnError)
			c := NewSimBacktest(fs)
			args := append([]string{"-input", "x", "-whole-team", "-project", "P", "-target-end-date", "2026-05-01"}, tc.args...)
			parse(t, fs, args...)
			_, err := c.Resolve(fixedNow)
			var fe *app.FieldError
			if !errors.As(err, &fe) || !strings.HasPrefix(fe.Msg, tc.msg) || fe.Fields[0] != tc.field {
				t.Fatalf("error = %v, want prefix %q field %s", err, tc.msg, tc.field)
			}
		})
	}
}

func TestInput_DBWarning(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	c := NewSimItems(fs)
	parse(t, fs, "-db", "x.db", "-engineers", "1", "-days", "3")
	r, err := c.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Warnings, []string{"-db is deprecated; use -input instead"}) || r.InputPath != "x.db" {
		t.Errorf("warnings = %v path = %q", r.Warnings, r.InputPath)
	}

	fs = flag.NewFlagSet("t", flag.ContinueOnError)
	in := AddInput(fs, false)
	if in.DB != nil || fs.Lookup("db") != nil {
		t.Error("withDB=false must not register -db")
	}
	_, _, err = in.Resolve()
	wantFieldError(t, err, "-input is required", "input")
}

// TestResolve_SetEqualsParse is the property the web server relies on:
// filling a FlagSet with fs.Set resolves identically to fs.Parse.
func TestResolve_SetEqualsParse(t *testing.T) {
	strip := func(r app.ItemsRequest) app.ItemsRequest {
		r.Seed, r.Issues, r.Progress = 0, nil, nil
		return r
	}
	fs1 := flag.NewFlagSet("a", flag.ContinueOnError)
	c1 := NewSimItems(fs1)
	parse(t, fs1, "-input", "x.csv", "-engineers", "3", "-days", "30")
	r1, err := c1.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}

	fs2 := flag.NewFlagSet("a", flag.ContinueOnError)
	c2 := NewSimItems(fs2)
	for _, kv := range [][2]string{{"input", "x.csv"}, {"engineers", "3"}, {"days", "30"}} {
		if err := fs2.Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	r2, err := c2.Resolve(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(strip(r1.Request), strip(r2.Request)) {
		t.Errorf("Parse and Set disagree:\n%+v\n%+v", strip(r1.Request), strip(r2.Request))
	}
}
