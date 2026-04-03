package main

import (
	"context"
	"errors"
	"os"

	"github.com/noopolis/moltnet/internal/node"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
)

func runNode(ctx context.Context, args []string) error {
	explicitPath := ""
	if len(args) > 1 {
		return os.ErrInvalid
	}
	if len(args) == 1 {
		explicitPath = args[0]
	}
	if envPath := os.Getenv("MOLTNET_NODE_CONFIG"); explicitPath == "" && envPath != "" {
		explicitPath = envPath
	}

	path, ok, err := nodeconfig.DiscoverPath(explicitPath)
	if err != nil {
		return err
	}
	if !ok {
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
