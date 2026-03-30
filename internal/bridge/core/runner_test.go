package core

import (
	"context"
	"errors"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type fakeAdapter struct {
	name   string
	config bridgeconfig.Config
	err    error
	called bool
}

func (f *fakeAdapter) Name() string { return f.name }
func (f *fakeAdapter) Run(_ context.Context, config bridgeconfig.Config) error {
	f.called = true
	f.config = config
	return f.err
}

func TestSelectAdapter(t *testing.T) {
	t.Parallel()

	for _, runtimeKind := range []string{
		bridgeconfig.RuntimeTinyClaw,
		bridgeconfig.RuntimeOpenClaw,
		bridgeconfig.RuntimePicoClaw,
	} {
		runtimeKind := runtimeKind
		t.Run(runtimeKind, func(t *testing.T) {
			t.Parallel()

			adapter, err := selectAdapter(runtimeKind)
			if err != nil {
				t.Fatalf("selectAdapter() error = %v", err)
			}
			if adapter.Name() != runtimeKind {
				t.Fatalf("unexpected adapter name %q", adapter.Name())
			}
		})
	}

	if _, err := selectAdapter("weird"); err == nil {
		t.Fatal("expected unsupported adapter error")
	}
}

func TestRunnerRun(t *testing.T) {
	t.Parallel()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
	}

	adapter := &fakeAdapter{name: "fake"}
	runner := &Runner{config: config, adapter: adapter}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !adapter.called {
		t.Fatal("expected adapter to be called")
	}
	if adapter.config.Agent.ID != "researcher" {
		t.Fatalf("unexpected config %#v", adapter.config)
	}

	expected := errors.New("boom")
	failing := &Runner{config: config, adapter: &fakeAdapter{name: "fake", err: expected}}
	if err := failing.Run(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	config := bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw},
	}

	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if runner.adapter == nil {
		t.Fatal("expected adapter")
	}

	config.Runtime.Kind = "bad"
	if _, err := New(config); err == nil {
		t.Fatal("expected unsupported runtime error")
	}
}
