package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"forecasting/internal/linear"
	"forecasting/internal/sqlite"
	internalsync "forecasting/internal/sync"
)

func main() {
	source := flag.String("source", "linear", "data source to sync (currently: linear)")
	var teams linear.KeyList
	flag.Var(&teams, "teams", "comma-separated team keys, e.g. ENG,DESIGN (source=linear only); required unless -all-teams")
	allTeams := flag.Bool("all-teams", false, "fetch issues for all accessible teams (source=linear only); mutually exclusive with -teams")
	listTeamsFlag := flag.Bool("list-teams", false, "list accessible teams and their keys, then exit (source=linear only)")
	syncAll := flag.Bool("sync-all", false, "ignore the stored watermark and do a full reload from the source")
	db := flag.String("db", "items.db", "path to SQLite database file")
	flag.Parse()

	if err := run(context.Background(), *source, teams, *allTeams, *listTeamsFlag, *syncAll, *db); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, source string, teams linear.KeyList, allTeams, listTeams, syncAll bool, dbPath string) error {
	switch source {
	case "linear":
		return syncLinear(ctx, teams, allTeams, listTeams, syncAll, dbPath)
	default:
		return fmt.Errorf("unknown source %q; supported: linear", source)
	}
}

func syncLinear(ctx context.Context, teams linear.KeyList, allTeams, listTeams, syncAll bool, dbPath string) error {
	apiKey := os.Getenv("LINEAR_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("LINEAR_API_KEY environment variable is not set")
	}

	src := linear.New(apiKey, []string(teams))

	if listTeams {
		return src.ListTeams(ctx, os.Stderr)
	}

	if allTeams && len(teams) > 0 {
		return fmt.Errorf("-teams and -all-teams are mutually exclusive")
	}
	if !allTeams && len(teams) == 0 {
		return fmt.Errorf("must specify -teams (comma-separated team keys) or -all-teams")
	}

	if allTeams {
		fmt.Fprintln(os.Stderr, "fetching completed and in-progress issues for all accessible teams")
	} else {
		fmt.Fprintf(os.Stderr, "filtering to teams: %s\n", strings.Join(teams, ", "))
	}

	store, err := sqlite.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	n, err := internalsync.Sync(ctx, src, store, syncAll)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "done. upserted %d items.\n", n)
	return nil
}
