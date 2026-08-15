package main

import (
	"context"
	"errors"
	"flag"
	"os"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/node"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
)

// runNode implements `moltnet node [path]` / `moltnet node start [path]`.
// Config resolution follows the same shared tier as the server config
// (app.ResolveNodeConfigPath): explicit (path argument or
// MOLTNET_NODE_CONFIG) wins outright; with --id given,
// ~/.moltnet/<id>/MoltnetNode is resolved first, falling back to cwd only
// when its config self-identifies as network id <id>; with neither,
// ./MoltnetNode in cwd, then the sole network under ~/.moltnet/.
func runNode(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("moltnet node", flag.ContinueOnError)
	flags.SetOutput(stdout)
	id := flags.String("id", "", "network id to select under ~/.moltnet when several exist")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return os.ErrInvalid
	}

	explicitPath := ""
	if flags.NArg() == 1 {
		explicitPath = flags.Arg(0)
	}
	if envPath := os.Getenv("MOLTNET_NODE_CONFIG"); explicitPath == "" && envPath != "" {
		explicitPath = envPath
	}

	path, found, err := app.ResolveNodeConfigPath(explicitPath, *id)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("MoltnetNode config not found")
	}

	config, err := nodeconfig.LoadFile(path)
	if err != nil {
		return err
	}

	runner, err := node.New(config)
	if err != nil {
		return err
	}

	return runner.Run(ctx)
}
