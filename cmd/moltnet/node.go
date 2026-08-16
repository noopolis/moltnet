package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

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
		// The flag package stops parsing at the first non-flag argument, so
		// `moltnet node start <path> --id x` never reaches --id as a flag at
		// all: it lands here as three unparsed positional args (path, --id,
		// x), with *id left at its zero value — every flag after the path is
		// silently dropped, not applied and not rejected, even though the
		// usage text (buildNodeUsage) already documents flags-first
		// ([--id <network-id>] [path]). Extra args that still look like a
		// flag get a specific, actionable error instead of the same
		// os.ErrInvalid a genuinely bogus arg list gets, so the fix is
		// obvious from the message alone.
		for _, extra := range flags.Args()[1:] {
			if strings.HasPrefix(extra, "-") {
				return fmt.Errorf("node: flags must precede the path (got %q after it); usage: moltnet node start [--id <network-id>] [path]", extra)
			}
		}
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
