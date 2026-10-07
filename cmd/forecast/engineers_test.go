package main

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/internal/util"
)

func TestEngineerSpec_Count(t *testing.T) {
	var e engineerSpec
	if err := e.Set(" 3 "); err != nil {
		t.Fatalf("Set(3): %v", err)
	}
	if e.Count() != 3 || e.Names() != nil {
		t.Errorf("got count=%d names=%v, want 3 and nil", e.Count(), e.Names())
	}
}

func TestEngineerSpec_Names(t *testing.T) {
	var e engineerSpec
	if err := e.Set("alice, bob ,carol"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if e.Count() != 3 || !reflect.DeepEqual(e.Names(), []string{"alice", "bob", "carol"}) {
		t.Errorf("got count=%d names=%v", e.Count(), e.Names())
	}

	// A later count replaces earlier names (and vice versa).
	if err := e.Set("2"); err != nil {
		t.Fatal(err)
	}
	if e.Count() != 2 || e.Names() != nil {
		t.Errorf("after Set(2): count=%d names=%v", e.Count(), e.Names())
	}
}

func TestEngineerSpec_Errors(t *testing.T) {
	cases := []struct {
		in      string
		wantErr string // "" = accepted
		count   int
	}{
		{"", "empty value", 0},
		{"   ", "empty value", 0},
		{"0", "must be positive", 0},
		{"-2", "must be positive", 0},
		{"alice,,", "", 1},
		{",,", "no names", 0},
		{"alice,3", "looks like a count", 0},
		{"alice,alice", "duplicate name", 0},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			var e engineerSpec
			err := e.Set(c.in)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("Set(%q): %v", c.in, err)
				}
				if e.Count() != c.count {
					t.Errorf("count = %d, want %d", e.Count(), c.count)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("Set(%q) error = %v, want one containing %q", c.in, err, c.wantErr)
			}
		})
	}
}

func TestEngineerSpec_String(t *testing.T) {
	var e engineerSpec
	if got := e.String(); got != "" {
		t.Errorf("unset String = %q, want empty", got)
	}
	e.Set("4")
	if got := e.String(); got != "4" {
		t.Errorf("count String = %q", got)
	}
	e.Set("alice,bob")
	if got := e.String(); got != "alice,bob" {
		t.Errorf("names String = %q", got)
	}
}

func TestEngineerSpec_ConfigListForm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, []byte("engineers: [alice, bob]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	var e engineerSpec
	fs.Var(&e, "engineers", "")
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := util.ApplyConfig(fs, path); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if !reflect.DeepEqual(e.Names(), []string{"alice", "bob"}) {
		t.Errorf("names = %v, want [alice bob]", e.Names())
	}

	// A bare YAML integer is a count.
	path2 := filepath.Join(t.TempDir(), "cfg2.yaml")
	os.WriteFile(path2, []byte("engineers: 4\n"), 0o644)
	fs2 := flag.NewFlagSet("t", flag.ContinueOnError)
	var e2 engineerSpec
	fs2.Var(&e2, "engineers", "")
	fs2.Parse(nil)
	if err := util.ApplyConfig(fs2, path2); err != nil {
		t.Fatal(err)
	}
	if e2.Count() != 4 || e2.Names() != nil {
		t.Errorf("count=%d names=%v, want 4 and nil", e2.Count(), e2.Names())
	}
}
