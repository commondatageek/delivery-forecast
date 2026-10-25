package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/logx"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/source"

	"github.com/mattn/go-isatty"
)

// --- Progress reporting ---

type progressBar struct {
	enabled bool
	total   int
	step    int
	mu      sync.Mutex
}

func newProgressBar(total int) *progressBar {
	return &progressBar{
		enabled: isatty.IsTerminal(os.Stderr.Fd()) && total > 0,
		total:   total,
		step:    max(1, total/200),
	}
}

func (b *progressBar) update(done, _ int) {
	if !b.enabled || (done != b.total && done%b.step != 0) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	const width = 30
	filled := width * done / b.total
	fmt.Fprintf(os.Stderr, "\r[%s%s] %d/%d", strings.Repeat("=", filled), strings.Repeat(" ", width-filled), done, b.total)
	if done == b.total {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

// --- Sim request helpers ---

// loadSimInputs loads the issue set and the exclusions file into b.
func loadSimInputs(b *app.SimBase, inputPath, stdinFormat, exclusionsFile string) error {
	all, err := loadIssues(context.Background(), inputPath, stdinFormat)
	if err != nil {
		return fmt.Errorf("load issues: %w", err)
	}
	exc, err := source.LoadExclusions(exclusionsFile)
	if err != nil {
		return err
	}
	b.Issues, b.Exclusions = all, exc
	return nil
}

// printWarnings logs every warning an app call returned, in order.
func printWarnings(warnings []string) {
	for _, w := range warnings {
		logx.Warnf("%s", w)
	}
}

// fingerprintForManifest fingerprints the input file as it was when loaded, so
// the manifest describes the data the run actually used even if the file
// changes during a long simulation. It skips the hash when no manifest is
// wanted.
func fingerprintForManifest(manifestPath, inputPath string) app.DataFile {
	if manifestPath == "" {
		return app.DataFile{}
	}
	return source.Fingerprint(inputPath)
}

// writeSimManifest writes the run manifest for a finished sim run to path. It
// is a no-op (and skips the input file's hash) when path is empty.
func writeSimManifest(path, subcommand string, cmd *flag.FlagSet, sf *params.SimFlags, inputFile app.DataFile, run app.SimRun, extra map[string]any) error {
	if path == "" {
		return nil
	}
	wd, _ := os.Getwd()
	return writeManifest(path, params.SimManifest(subcommand, cmd, sf, run, extra, params.ManifestContext{
		Invocation:  app.Invocation{Subcommand: subcommand, Args: os.Args, WorkingDir: wd},
		Binary:      buildInfo(),
		Data:        inputFile,
		GeneratedAt: nowFunc(),
	}))
}

// --- Flag helpers ---

// nowFunc is the CLI's clock. Tests replace it to make date defaults
// ("now", "today", "-3 months") and aging/backtest "today" deterministic.
var nowFunc = time.Now
