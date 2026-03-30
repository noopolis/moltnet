package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/noopolis/moltnet/internal/bridge/core"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type signalContextFactory func() (context.Context, context.CancelFunc)

func main() {
	if err := runWithSignals(os.Args[1:], defaultSignalContext); err != nil {
		log.Fatal(err)
	}
}

func defaultSignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

func runWithSignals(args []string, factory signalContextFactory) error {
	ctx, stop := factory()
	defer stop()

	return run(ctx, args)
}

func run(ctx context.Context, args []string) error {
	if len(args) < 1 {
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
