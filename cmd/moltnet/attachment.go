package main

import (
	"context"
	"os"

	"github.com/noopolis/moltnet/internal/bridge/core"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func runAttachment(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return os.ErrInvalid
	}

	cfg, err := bridgeconfig.LoadFile(args[0])
	if err != nil {
		return err
	}

	runner, err := core.New(cfg)
	if err != nil {
		return err
	}

	return runner.Run(ctx)
}
