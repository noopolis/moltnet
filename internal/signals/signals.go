package signals

import (
	"context"
	"os/signal"
	"syscall"
)

type ContextFactory func() (context.Context, context.CancelFunc)

func Default() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}
