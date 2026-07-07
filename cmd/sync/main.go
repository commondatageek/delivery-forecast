package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"forecasting/internal/linear"
	"forecasting/internal/sqlite"
	"forecasting/internal/syncer"
	"forecasting/internal/util"
)

func main() {
	var teams linear.KeyList
	flag.Var(&teams, "teams", "comma-separated team keys, e.g. ENG,DESIGN; limits the candidate team set")
	allTeams := flag.Bool("all-teams", false, "expand the candidate team set to every accessible Linear team; mutually exclusive with -teams")
	fullReload := flag.Bool("full-reload", false, "ignore each team's stored watermark and do a full reload")
	configFile := flag.String("config", "", "path to a YAML config file supplying flag values (CLI flags override)")

	flag.Parse()

	if err := util.ApplyConfig(flag.CommandLine, *configFile); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	dbPath := flag.Arg(0)
	if dbPath == "" {
		fmt.Fprintln(os.Stderr, "error: usage: sync [-teams k1,k2] [-all-teams] [-full-reload] <db-path>")
		os.Exit(1)
	}

	apiKey, err := linear.GetAPIKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	client := linear.New(apiKey)

	store, err := sqlite.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx := context.Background()
	if err := syncer.Run(ctx, client, store, syncer.Options{
		Teams:      teams,
		AllTeams:   *allTeams,
		FullReload: *fullReload,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
