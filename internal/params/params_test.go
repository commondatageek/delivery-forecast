package params

import (
	"flag"
	"testing"
	"time"
)

// newRandomSeedFlags returns a FlagSet defining the flag ResolveSeed inspects.
// It's only registered as "set" via fs.Set, which is exactly what IsSet
// (backed by fs.Visit) keys off of.
func newRandomSeedFlags() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Int64("random-seed", 0, "")
	return fs
}

func TestResolveSeed(t *testing.T) {
	now := time.Date(2025, 1, 5, 14, 30, 0, 0, time.Local)

	// Unset -random-seed: time-based seed derived from now.
	fs := newRandomSeedFlags()
	if got := ResolveSeed(fs, 99, now); got != now.UnixNano() {
		t.Errorf("ResolveSeed (unset) = %d, want %d", got, now.UnixNano())
	}

	// Explicit -random-seed: returned verbatim, now ignored.
	fs = newRandomSeedFlags()
	if err := fs.Set("random-seed", "99"); err != nil {
		t.Fatal(err)
	}
	if got := ResolveSeed(fs, 99, now); got != 99 {
		t.Errorf("ResolveSeed (set) = %d, want 99", got)
	}
}

func TestFlagRecords(t *testing.T) {
	cmd := flag.NewFlagSet("sim items", flag.ContinueOnError)
	cmd.String("db", "", "")
	cmd.Int("days", 30, "")
	if err := cmd.Parse([]string{"-db", "test.db"}); err != nil {
		t.Fatal(err)
	}

	byName := map[string]int{}
	records := FlagRecords(cmd)
	for i, r := range records {
		byName[r.Name] = i
	}
	dbFlag, daysFlag := records[byName["db"]], records[byName["days"]]
	if !dbFlag.Set || dbFlag.Value != "test.db" {
		t.Fatalf("db flag = %+v, want Set=true Value=test.db", dbFlag)
	}
	if daysFlag.Set || daysFlag.Value != "30" || daysFlag.Default != "30" {
		t.Fatalf("days flag = %+v, want Set=false Value=30 Default=30", daysFlag)
	}
}
