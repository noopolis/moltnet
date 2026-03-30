package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/noopolis/moltnet/internal/app"
)

const version = "0.0.0-dev"

type signalContextFactory func() (context.Context, context.CancelFunc)

func main() {
	if err := runWithSignals(version, defaultSignalContext); err != nil {
		log.Fatal(err)
	}
}

func defaultSignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

func runWithSignals(buildVersion string, factory signalContextFactory) error {
	ctx, stop := factory()
	defer stop()

	return run(ctx, buildVersion)
}

func run(ctx context.Context, buildVersion string) error {
	cfg := app.ConfigFromEnv(buildVersion)
	instance := app.New(cfg)
	return instance.Run(ctx)
}
