package main

import (
	"context"

	signalctx "github.com/noopolis/moltnet/internal/signals"
)

type signalContextFactory = signalctx.ContextFactory

func defaultSignalContext() (context.Context, context.CancelFunc) {
	return signalctx.Default()
}
