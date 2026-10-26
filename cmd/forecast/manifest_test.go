package main

import (
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
