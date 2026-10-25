package main

import (
	"flag"
	"testing"
)

func TestBuildInfo_Degrades(t *testing.T) {
	// Under `go test`, debug.ReadBuildInfo() succeeds but vcs.* settings are
	// typically absent (no git-tree build), so GitSHA should come back empty
	// rather than panicking or erroring.
	bi := buildInfo()
	if bi.GoVersion == "" {
		t.Fatalf("buildInfo().GoVersion = %q, want non-empty", bi.GoVersion)
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
	records := flagRecords(cmd)
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
