package app

import (
	"context"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	t.Parallel()

	instance := New(Config{
		ListenAddr:  ":0",
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
	})

	if instance.server == nil {
		t.Fatal("expected server")
	}
	if instance.server.Addr != ":0" {
		t.Fatalf("unexpected addr %q", instance.server.Addr)
	}
}

func TestRunSuccessAndFailure(t *testing.T) {
	t.Parallel()

	success := New(Config{
		ListenAddr:  "127.0.0.1:0",
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	if err := success.Run(ctx); err != nil {
		t.Fatalf("Run() success path error = %v", err)
	}

	failure := New(Config{
		ListenAddr:  "bad::addr",
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
	})

	if err := failure.Run(context.Background()); err == nil {
		t.Fatal("expected invalid addr error")
	}
}
