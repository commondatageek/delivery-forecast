package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"forecasting/internal/cfd"
	"forecasting/internal/linear"
	"forecasting/internal/sqlite"
	"forecasting/internal/util"
)

func main() {
	dbFile := flag.String("db", "", "path to SQLite database")
	startStr := flag.String("start", "", "Start date, inclusive (YYYY-MM-DD; default: today minus 3 months)")
	endStr := flag.String("end", "", "End date, inclusive (YYYY-MM-DD; default: today)")
	format := flag.String("format", "html", "Output format: html, json")
	outPath := flag.String("out", "", "Write output to this file instead of stdout")
	var teams linear.KeyList
	flag.Var(&teams, "teams", "Comma-separated team keys to filter by (e.g. ENG,DATA); default: all teams")
	configFile := flag.String("config", "", "path to a YAML config file supplying flag values (CLI flags override)")
	flag.Parse()

	if err := util.ApplyConfig(flag.CommandLine, *configFile); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *dbFile == "" {
		fmt.Fprintln(os.Stderr, "error: -db is required")
		os.Exit(1)
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)

	windowEnd := today
	if *endStr != "" {
		t, err := util.ParseDate(*endStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid -end %q: %v\n", *endStr, err)
			os.Exit(1)
		}
		windowEnd = t
	}

	windowStart := today.AddDate(0, -3, 0)
	if *startStr != "" {
		t, err := util.ParseDate(*startStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid -start %q: %v\n", *startStr, err)
			os.Exit(1)
		}
		windowStart = t
	}

	if !windowStart.Before(windowEnd) {
		fmt.Fprintln(os.Stderr, "error: -start must be before -end")
		os.Exit(1)
	}

	store, err := sqlite.Open(*dbFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	raw, err := store.CFDIssues(context.Background(), teams)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: query issues: %v\n", err)
		os.Exit(1)
	}

	var normalized []cfd.NormalizedIssue
	skipped := 0
	for _, r := range raw {
		ni, ok := cfd.Normalize(r)
		if !ok {
			skipped++
			continue
		}
		normalized = append(normalized, ni)
	}

	rows := cfd.BuildGrid(normalized, windowStart, windowEnd)

	if err := cfd.AssertInvariants(rows); err != nil {
		fmt.Fprintf(os.Stderr, "error: CFD invariant violated: %v\n", err)
		os.Exit(1)
	}

	health := cfd.ComputeHealth(rows, normalized, windowStart, windowEnd)
	health.TotalIssues = len(raw)
	health.SkippedIssues = skipped

	out := os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: create output file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}

	switch *format {
	case "html":
		if err := cfd.RenderHTML(out, rows, health, len(raw), skipped, windowStart, windowEnd); err != nil {
			fmt.Fprintf(os.Stderr, "error: render HTML: %v\n", err)
			os.Exit(1)
		}
	case "json":
		if err := cfd.RenderJSON(out, rows, health); err != nil {
			fmt.Fprintf(os.Stderr, "error: encode JSON: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "error: unknown -format %q (use html or json)\n", *format)
		os.Exit(1)
	}
}
