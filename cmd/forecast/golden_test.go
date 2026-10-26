package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// goldenCase is one CLI invocation whose stdout, stderr and returned error are
// frozen in testdata/golden/<name>.golden.
type goldenCase struct {
	name string
	run  func([]string) error
	args []string
}

// gargs expands the shorthands S/M/X/B/SIM in a space-separated argument string
// and splits it, honouring double quotes so "Search Revamp" stays one element.
func gargs(s string) []string {
	r := strings.NewReplacer(
		"SIM", "-sample-start 2025-01-01 -sample-end 2025-03-15 -simulations 2000 -random-seed 42 -goroutines 4",
		"@S", "../../testdata/sample-issues.csv",
		"@M", "testdata/golden/multiteam.csv",
		"@X", "testdata/golden/exclusions.json",
		"@B", "testdata/golden/bad-exclusions.json",
	)
	s = r.Replace(s)
	var out []string
	var cur strings.Builder
	inQuote, have := false, false
	for _, c := range s {
		switch {
		case c == '"':
			inQuote = !inQuote
			have = true
		case c == ' ' && !inQuote:
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(c)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

func goldenCases() []goldenCase {
	const simPins = "-simulations 2000 -random-seed 42 -goroutines 4"
	c := func(name string, run func([]string) error, args string) goldenCase {
		return goldenCase{name, run, gargs(args)}
	}
	return []goldenCase{
		c("sim-items-count", cmdSimItems, "-input @S -engineers 3 SIM -target-start-date 2025-03-16 -days 30"),
		c("sim-items-defaults", cmdSimItems, "-input @S -engineers 3 -sample-start 2025-01-01 "+simPins+" -days 30"),
		c("sim-items-named-exclusions", cmdSimItems, "-input @S -engineers alice,bob -exclusions @X SIM -target-start-date 2025-03-16 -target-end-date 2025-04-30 -confidence 50,90"),
		c("sim-items-wholeteam-exclusions", cmdSimItems, "-input @S -whole-team -exclusions @X SIM -target-start-date 2025-03-16 -days 30"),
		c("sim-items-typical-unknown", cmdSimItems, "-input @S -engineers 2 -typical-engineers alice,zed SIM -target-start-date 2025-03-16 -days 30"),
		c("sim-days-single", cmdSimDays, "-input @S -engineers 2 SIM -target-start-date 2025-03-16 -items 10"),
		c("sim-days-trajectory", cmdSimDays, "-input @S -engineers 2 SIM -target-start-date 2025-03-16 -items 5,4,3 -confidence 50,85"),
		c("sim-days-exclusions", cmdSimDays, "-input @S -engineers alice,bob -exclusions @X SIM -target-start-date 2025-03-16 -items 8"),
		c("sim-prob-items", cmdSimProbability, "-input @S -engineers 3 SIM -target-start-date 2025-03-16 -days 21 -items 12"),
		c("sim-prob-table", cmdSimProbability, "-input @S -whole-team SIM -target-start-date 2025-03-16 -days 14"),
		c("sim-backtest-text", cmdSimBacktest, `-input @S -engineers 3 SIM -project "Search Revamp" -target-end-date 2025-03-31`),
		c("sim-backtest-csv", cmdSimBacktest, `-input @S -engineers 3 SIM -project "Search Revamp" -milestone Indexing -replay-start-date 2025-02-01 -target-end-date 2025-03-31 -format csv`),
		c("sim-backtest-exclusions", cmdSimBacktest, `-input @S -engineers alice,bob,carol -exclusions @X SIM -project "Search Revamp" -replay-start-date 2025-03-01 -target-end-date 2025-04-15`),
		c("aging-text", cmdAging, "-input @S -sample-start 2025-01-01 -sample-end 2025-03-15"),
		c("aging-show-completed", cmdAging, "-input @S -sample-start 2025-01-01 -sample-end 2025-03-15 -show-completed -percentile 70 -min-cycle-time 1d"),
		c("aging-json", cmdAging, "-input @S -sample-start 2025-01-01 -sample-end 2025-03-15 -format json"),
		c("aging-html", cmdAging, "-input @S -sample-start 2025-01-01 -sample-end 2025-03-15 -format html -show-completed"),
		c("aging-defaults", cmdAging, "-input @S"),
		c("aging-multiteam", cmdAging, "-input @M -sample-start 2025-01-01 -sample-end 2025-03-15"),
		c("aging-multiteam-scoped", cmdAging, "-input @M -sample-start 2025-01-01 -sample-end 2025-03-15 -teams eng"),
		c("cfd-json", cmdCFD, "-input @S -start 2025-01-01 -end 2025-03-15 -format json"),
		c("cfd-html", cmdCFD, "-input @S -start 2025-01-01 -end 2025-03-15"),
		c("cfd-multiteam", cmdCFD, "-input @M -start 2025-01-01 -end 2025-03-15 -format json"),
		c("cfd-multiteam-scoped", cmdCFD, "-input @M -start 2025-01-01 -end 2025-03-15 -format json -teams DATA"),
		c("count-summary", cmdCount, "-input @S -updated-since 2025-01-01"),
		c("count-milestones", cmdCount, "-input @S -updated-since 2025-01-01 -milestones"),
		c("count-multiteam", cmdCount, "-input @M -updated-since 2025-01-01"),
		c("count-multiteam-grouped", cmdCount, "-input @M -updated-since 2025-01-01 -milestones"),
		c("count-defaults", cmdCount, "-input @S"),
		c("history-csv", cmdHistory, "-input @S -end 2025-03-15"),
		c("history-json-scoped", cmdHistory, `-input @S -project "Search Revamp" -milestone Indexing -window 14 -end 2025-03-15 -format json`),
		c("history-text", cmdHistory, "-input @S -start 2025-02-01 -end 2025-03-01 -format text"),
		c("history-multiteam", cmdHistory, "-input @M -end 2025-03-15"),
		c("check-clean", cmdCheck, "-input @S"),
		c("check-exclusions", cmdCheck, "-input @M -exclusions @X"),
		c("check-bad-exclusions", cmdCheck, "-input @S -exclusions @B"),
		c("err-sim-no-mode", cmdSimItems, "-input @S SIM -days 30"),
		c("err-sim-days-and-end", cmdSimItems, "-input @S -engineers 2 SIM -days 30 -target-end-date 2025-04-30"),
		c("err-sim-days-zero-throughput", cmdSimDays, "-input @S -whole-team -sample-start 2025-03-10 -sample-end 2025-03-15 -items 5 -random-seed 1 -goroutines 1"),
		c("err-sim-items-bad-confidence", cmdSimItems, "-input @S -engineers 2 SIM -days 30 -confidence 0"),
		c("err-sim-days-no-items", cmdSimDays, "-input @S -engineers 2 SIM"),
		c("err-backtest-unknown-project", cmdSimBacktest, "-input @S -engineers 2 SIM -project Nope -target-end-date 2025-03-31"),
		c("err-backtest-no-project", cmdSimBacktest, "-input @S -engineers 2 SIM -target-end-date 2025-03-31"),
		c("err-aging-percentile", cmdAging, "-input @S -percentile 0"),
		c("err-aging-window", cmdAging, "-input @S -sample-start 2025-03-01 -sample-end 2025-02-01"),
		c("err-cfd-window", cmdCFD, "-input @S -start 2025-03-01 -end 2025-02-01"),
		c("err-history-unknown-project", cmdHistory, "-input @S -project Nope"),
		c("err-history-window", cmdHistory, "-input @S -window 0"),
	}
}

// runCaptured runs fn while capturing stdout and stderr concurrently. Both
// pipes are drained while fn runs so outputs larger than the OS pipe buffer
// (HTML) can't deadlock.
func runCaptured(t *testing.T, fn func() error) (stdout, stderr string, err error) {
	t.Helper()
	outR, outW, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	errR, errW, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	var wg sync.WaitGroup
	var outB, errB []byte
	wg.Add(2)
	go func() { defer wg.Done(); outB, _ = io.ReadAll(outR) }()
	go func() { defer wg.Done(); errB, _ = io.ReadAll(errR) }()

	err = fn()

	os.Stdout, os.Stderr = origOut, origErr
	outW.Close()
	errW.Close()
	wg.Wait()
	outR.Close()
	errR.Close()
	return string(outB), string(errB), err
}

func firstDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g || i >= len(wl) || i >= len(gl) {
			return fmt.Sprintf("first difference at line %d:\n  want: %q\n  got:  %q", i+1, w, g)
		}
	}
	return "no difference"
}

// pinGoldenClock fixes time.Local and the CLI clock to the golden tests'
// zone and instant for the duration of t.
func pinGoldenClock(t *testing.T) {
	t.Helper()
	origLocal, origNow := time.Local, nowFunc
	t.Cleanup(func() { time.Local, nowFunc = origLocal, origNow })
	time.Local = time.FixedZone("GOLDEN", -5*60*60)
	fixed := time.Date(2025, 3, 15, 10, 30, 0, 0, time.Local)
	nowFunc = func() time.Time { return fixed }
}

func TestGolden(t *testing.T) {
	pinGoldenClock(t)

	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, err := runCaptured(t, func() error { return c.run(c.args) })
			errText := "<nil>"
			if err != nil {
				errText = err.Error()
			}
			got := "==== stdout\n" + stdout + "==== stderr\n" + stderr + "==== error\n" + errText + "\n"
			path := filepath.Join("testdata", "golden", c.name+".golden")
			if *updateGolden {
				if werr := os.WriteFile(path, []byte(got), 0o644); werr != nil {
					t.Fatal(werr)
				}
				return
			}
			wantB, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("read %s: %v", path, rerr)
			}
			if want := string(wantB); want != got {
				t.Errorf("%s mismatch: %s\n--- want ---\n%s\n--- got ---\n%s", path, firstDiff(want, got), want, got)
			}
		})
	}
}
