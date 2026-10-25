package params

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/commondatageek/delivery-forecast/internal/util"
)

func TestCommandLine(t *testing.T) {
	fs := flag.NewFlagSet("sim backtest", flag.ContinueOnError)
	c := NewSimBacktest(fs)
	parse(t, fs, "-input", "x.csv", "-project", "Search Revamp", "-whole-team", "-config", "ignored.yaml")
	_ = c
	argv := CommandLine("sim backtest", fs)
	want := []string{"forecast", "sim", "backtest", "-input", "x.csv", "-project", "Search Revamp", "-whole-team"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
	if got, want := ShellJoin(argv), `forecast sim backtest -input x.csv -project 'Search Revamp' -whole-team`; got != want {
		t.Errorf("ShellJoin = %s, want %s", got, want)
	}

	fs = flag.NewFlagSet("sim items", flag.ContinueOnError)
	NewSimItems(fs)
	parse(t, fs, "-whole-team=false")
	if got := CommandLine("sim items", fs); !reflect.DeepEqual(got, []string{"forecast", "sim", "items", "-whole-team=false"}) {
		t.Errorf("bool false = %q", got)
	}
}

func TestShellJoin_Quotes(t *testing.T) {
	if got, want := ShellJoin([]string{"a", "it's", "", "-3 months", "x,y=z/w.v"}), `a 'it'\''s' '' '-3 months' x,y=z/w.v`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestConfigYAML_RoundTrip(t *testing.T) {
	fs := flag.NewFlagSet("sim items", flag.ContinueOnError)
	NewSimItems(fs)
	parse(t, fs, "-input", "x.csv", "-engineers", "alice,bob", "-days", "30", "-confidence", "50,85", "-whole-team=false", "-sample-start", "-6 months", "-random-seed", "42")
	yml, err := ConfigYAML(fs)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	fs2 := flag.NewFlagSet("sim items", flag.ContinueOnError)
	NewSimItems(fs2)
	if err := util.ApplyConfig(fs2, path); err != nil {
		t.Fatal(err)
	}
	fs.VisitAll(func(f *flag.Flag) {
		if g := fs2.Lookup(f.Name); g.Value.String() != f.Value.String() {
			t.Errorf("flag %s: %q after round trip, want %q", f.Name, g.Value.String(), f.Value.String())
		}
	})
}
