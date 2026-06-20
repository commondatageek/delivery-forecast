package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"forecasting/internal/linear"
	"forecasting/internal/sqlite"
)

func main() {
	var teams linear.KeyList
	flag.Var(&teams, "teams", "comma-separated team keys, e.g. ENG,DESIGN; required unless -all-teams")
	allTeams := flag.Bool("all-teams", false, "fetch issues for all accessible teams; mutually exclusive with -teams")
	listTeamsFlag := flag.Bool("list-teams", false, "list accessible teams and their keys, then exit")
	syncAll := flag.Bool("sync-all", false, "ignore the stored watermark and do a full reload from Linear")
	db := flag.String("db", "linear.db", "path to SQLite database file")

	flag.Parse()

	apiKey, err := getAPIKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := run(context.Background(), apiKey, teams, *allTeams, *listTeamsFlag, *syncAll, *db); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func getAPIKey() (string, error) {
	apiKey := os.Getenv("LINEAR_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("LINEAR_API_KEY environment variable is not set")
	}
	return apiKey, nil
}

func run(ctx context.Context, apiKey string, teams linear.KeyList, allTeams, listTeams, syncAll bool, dbPath string) error {
	client := linear.New(apiKey, []string(teams))

	if listTeams {
		return client.ListTeams(ctx, os.Stderr)
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

	var since time.Time
	if !syncAll {
		since, err = store.LatestUpdatedAt(ctx)
		if err != nil {
			return fmt.Errorf("watermark: %w", err)
		}
	}

	issues, err := client.Fetch(ctx, since)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if len(issues) > 0 {
		if err := store.Upsert(ctx, issues...); err != nil {
			return fmt.Errorf("upsert: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "done. upserted %d issues.\n", len(issues))
	return nil
}
