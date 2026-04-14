package node

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
)

type fakeRunner struct {
	run func(ctx context.Context) error
}

func (f *fakeRunner) Run(ctx context.Context) error {
	return f.run(ctx)
}

func TestNew(t *testing.T) {
	t.Parallel()

	runner, err := New(nodeconfig.Config{
		Version: nodeconfig.VersionV1,
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://127.0.0.1:8787",
			NetworkID: "local",
		},
		Attachments: []nodeconfig.AttachmentConfig{
			{
				Agent: bridgeconfig.AgentConfig{ID: "alpha"},
				Runtime: bridgeconfig.RuntimeConfig{
					Kind:       bridgeconfig.RuntimeOpenClaw,
					GatewayURL: "ws://127.0.0.1:9100/gateway",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if len(runner.configs) != 1 {
		t.Fatalf("unexpected configs %#v", runner.configs)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	if _, err := New(nodeconfig.Config{}); err == nil {
		t.Fatal("expected invalid config error")
	}
}

func TestRunnerRunSuccess(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		configs: []bridgeconfig.Config{
			{Agent: bridgeconfig.AgentConfig{ID: "alpha"}, Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw}},
			{Agent: bridgeconfig.AgentConfig{ID: "beta"}, Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimePicoClaw}},
		},
		newRunner: func(config bridgeconfig.Config) (attachmentRunner, error) {
			return &fakeRunner{
				run: func(ctx context.Context) error {
					<-ctx.Done()
					return nil
				},
			}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerRunFailure(t *testing.T) {
	t.Parallel()

	expected := errors.New("boom")
	runner := &Runner{
		configs: []bridgeconfig.Config{
			{Agent: bridgeconfig.AgentConfig{ID: "alpha"}, Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw}},
		},
		newRunner: func(config bridgeconfig.Config) (attachmentRunner, error) {
			return &fakeRunner{
				run: func(ctx context.Context) error {
					return expected
				},
			}, nil
		},
	}

	if err := runner.Run(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}

func TestRunnerRunFactoryFailure(t *testing.T) {
	t.Parallel()

	expected := errors.New("factory")
	runner := &Runner{
		configs: []bridgeconfig.Config{
			{Agent: bridgeconfig.AgentConfig{ID: "alpha"}, Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw}},
		},
		newRunner: func(config bridgeconfig.Config) (attachmentRunner, error) {
			return nil, expected
		},
	}

	if err := runner.Run(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}

func TestRunnerRunWithoutAttachments(t *testing.T) {
	t.Parallel()

	runner := &Runner{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestNewCoreRunner(t *testing.T) {
	t.Parallel()

	runner, err := newCoreRunner(bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "alpha"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://127.0.0.1:8787",
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{
			Kind:       bridgeconfig.RuntimeOpenClaw,
			GatewayURL: "ws://127.0.0.1:9100/gateway",
		},
	})
	if err != nil {
		t.Fatalf("newCoreRunner() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("runner.Run() error = %v", err)
	}
}

func TestNewCoreRunnerRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	if _, err := newCoreRunner(bridgeconfig.Config{}); err == nil {
		t.Fatal("expected invalid bridge config error")
	}
}
