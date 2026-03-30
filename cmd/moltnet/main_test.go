package main

import (
	"context"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	t.Setenv("MOLTNET_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MOLTNET_NETWORK_ID", "local")
	t.Setenv("MOLTNET_NETWORK_NAME", "Local")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	if err := run(ctx, "test"); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunWithSignals(t *testing.T) {
	t.Setenv("MOLTNET_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MOLTNET_NETWORK_ID", "local")
	t.Setenv("MOLTNET_NETWORK_NAME", "Local")

	err := runWithSignals("test", func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()
		return ctx, cancel
	})
	if err != nil {
		t.Fatalf("runWithSignals() error = %v", err)
	}
}

func TestDefaultSignalContext(t *testing.T) {
	ctx, stop := defaultSignalContext()
	stop()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected canceled signal context")
	}
}
