package main

import (
	"context"

	"github.com/noopolis/moltnet/internal/app"
)

func runServer(ctx context.Context, buildVersion string) error {
	cfg, err := app.LoadConfig(buildVersion)
	if err != nil {
		return err
	}

	instance, err := app.New(cfg)
	if err != nil {
		return err
	}

	return instance.Run(ctx)
}
