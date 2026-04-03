package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/noopolis/moltnet/internal/bridge/core"
	signalctx "github.com/noopolis/moltnet/internal/signals"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type signalContextFactory = signalctx.ContextFactory

var version = "0.0.0-dev"
var stdout io.Writer = os.Stdout

func main() {
	if err := runWithSignals(os.Args[1:], defaultSignalContext); err != nil {
		log.Fatal(err)
	}
}

func defaultSignalContext() (context.Context, context.CancelFunc) {
	return signalctx.Default()
}

func runWithSignals(args []string, factory signalContextFactory) error {
	ctx, stop := factory()
	defer stop()

	return run(ctx, args)
}

func run(ctx context.Context, args []string) error {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(stdout, version)
		return nil
	}
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
