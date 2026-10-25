package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

// version is an optional build-time override: go build -ldflags "-X main.version=...".
// The primary SHA source is debug.ReadBuildInfo (auto VCS stamping).
var version string

// buildInfo reads VCS stamping info embedded by `go build` from the git tree
// (Go's automatic vcs.* build settings). It degrades gracefully (empty SHA,
// dirty=false) when build info is absent, e.g. under `go run` or for a binary
// built outside a git checkout.
func buildInfo() app.BinaryInfo {
	bi := app.BinaryInfo{VersionOverride: version}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return bi
	}
	bi.GoVersion = info.GoVersion
	bi.Module = info.Main.Path
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			bi.GitSHA = s.Value
		case "vcs.time":
			bi.GitTime = s.Value
		case "vcs.modified":
			bi.Dirty = s.Value == "true"
		}
	}
	return bi
}

// flagRecords snapshots every flag in fs — the command-unique ones too
// (-days, -items, -percentile, -manifest) — with its default, current value
// and whether it was explicitly set (on the command line or via -config).
func flagRecords(fs *flag.FlagSet) []app.FlagRecord {
	var flags []app.FlagRecord
	fs.VisitAll(func(f *flag.Flag) {
		flags = append(flags, app.FlagRecord{
			Name:    f.Name,
			Value:   f.Value.String(),
			Default: f.DefValue,
			Set:     isFlagSet(fs, f.Name),
		})
	})
	return flags
}

// writeManifest builds and writes a run manifest to path ("-" for stdout). It
// is a no-op when path is empty, so the flag is opt-in and free when unset.
func writeManifest(path string, in app.ManifestInputs) error {
	if path == "" {
		return nil // disabled
	}
	var w io.Writer = os.Stdout
	if path != "-" {
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("creating manifest file: %w", err)
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(app.NewManifest(in))
}
