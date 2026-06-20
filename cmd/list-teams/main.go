package main

import (
	"context"
	"fmt"
	"forecasting/internal/linear"
	"io"
	"os"
)

func main() {
	// get our API Key
	apiKey, err := linear.GetAPIKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// get a client
	client := linear.New(apiKey)

	// get a context
	ctx := context.Background()

	// stderr
	stderr := os.Stderr

	if err := writeTeamsList(ctx, client, stderr); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func writeTeamsList(ctx context.Context, client *linear.Client, w io.Writer) error {
	teams, err := client.ListTeams(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "accessible teams (%d):\n", len(teams))
	for _, t := range teams {
		fmt.Fprintf(w, "  %-12s %s\n", t.Key, t.Name)
	}

	return nil
}
