package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/commondatageek/delivery-forecast/internal/linear"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/sqlite"
	"github.com/commondatageek/delivery-forecast/internal/syncer"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdLinearSync(args []string) error {
	cmd := flag.NewFlagSet("linear sync", flag.ExitOnError)
	c := params.NewLinearSync(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *c.Config); err != nil {
		return err
	}

	r, err := c.Resolve()
	printWarnings(r.Warnings)
	if err != nil {
		return err
	}

	apiKey, err := linear.GetAPIKey()
	if err != nil {
		return err
	}

	client := linear.New(apiKey)

	store, err := sqlite.Open(r.InputPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	return syncer.Run(context.Background(), client, store, r.Request)
}

func cmdLinearTeams(args []string) error {
	cmd := flag.NewFlagSet("linear teams", flag.ExitOnError)
	c := params.NewLinearTeams(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *c.Config); err != nil {
		return err
	}

	apiKey, err := linear.GetAPIKey()
	if err != nil {
		return err
	}

	client := linear.New(apiKey)
	teams, err := client.ListTeams(context.Background())
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "accessible teams (%d):\n", len(teams))
	for _, t := range teams {
		fmt.Fprintf(os.Stdout, "  %-12s %s\n", t.Key, t.Name)
	}
	return nil
}
