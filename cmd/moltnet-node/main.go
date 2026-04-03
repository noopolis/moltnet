package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/noopolis/moltnet/internal/node"
	signalctx "github.com/noopolis/moltnet/internal/signals"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
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
