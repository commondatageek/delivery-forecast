package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/commondatageek/delivery-forecast/internal/app"
	"github.com/commondatageek/delivery-forecast/internal/importmap"
	"github.com/commondatageek/delivery-forecast/internal/params"
	"github.com/commondatageek/delivery-forecast/internal/report"
	"github.com/commondatageek/delivery-forecast/internal/source"
	"github.com/commondatageek/delivery-forecast/internal/util"
)

func cmdImport(args []string) error {
	cmd := flag.NewFlagSet("import", flag.ExitOnError)
	c := params.NewImport(cmd)
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *c.Config); err != nil {
		return err
	}

	r, err := c.Resolve()
	printWarnings(r.Warnings)
	if err != nil {
		return err
	}
	req := r.Request

	if req.Example != "" {
		return printTemplate(req.Example)
	}

	res, err := source.Import(context.Background(), req.From, req.Map, req.Out, req.DryRun)
	if err != nil {
		return err
	}
	report.Import(os.Stdout, req.From, req.Out, req.DryRun, res.Deleted, res.Report, app.CheckReadiness(res.Issues))
	return nil
}

// printTemplate prints an embedded mapping template verbatim, or with
// "list", every template's name.
func printTemplate(name string) error {
	if name == "list" {
		for _, t := range importmap.Templates() {
			fmt.Println(t)
		}
		return nil
	}
	data, ok := importmap.Template(name)
	if !ok {
		return &app.FieldError{Fields: []string{"example"}, Msg: fmt.Sprintf("-example: no template %q; available: %s", name, strings.Join(importmap.Templates(), ", "))}
	}
	_, err := os.Stdout.Write(data)
	return err
}
