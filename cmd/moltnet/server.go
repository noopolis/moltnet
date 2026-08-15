package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/noopolis/moltnet/internal/app"
)

// runServer implements `moltnet start` (and its `server` alias). Config
// resolution follows the shared explicit --config > ./Moltnet in cwd > sole
// network under ~/.moltnet/ order (app.ResolveConfigPath); when nothing is
// found anywhere in that order, it falls back to environment-only defaults
// exactly as before phase 4, so a bare `moltnet start` with no config file
// still works in tests and minimal setups.
func runServer(ctx context.Context, args []string, buildVersion string) error {
	flags := flag.NewFlagSet("moltnet start", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath = flags.String("config", "", "Moltnet config path")
		id         = flags.String("id", "", "network id to select under ~/.moltnet when several exist")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("start does not accept positional arguments")
	}

	path, found, err := app.ResolveConfigPath(*configPath, *id)
	if err != nil {
		return err
	}

	var cfg app.Config
	if found {
		cfg, err = app.LoadConfigForPath(path, buildVersion)
	} else {
		cfg, err = app.ConfigFromEnv(buildVersion)
	}
	if err != nil {
		return err
	}

	instance, err := app.New(cfg)
	if err != nil {
		return err
	}

	return instance.Run(ctx)
}
