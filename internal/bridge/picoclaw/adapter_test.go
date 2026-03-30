package picoclaw

import (
	"context"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func TestAdapter(t *testing.T) {
	t.Parallel()

	adapter := New()
	if adapter.Name() != bridgeconfig.RuntimePicoClaw {
		t.Fatalf("unexpected name %q", adapter.Name())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := adapter.Run(ctx, bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{ControlURL: "http://control"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://moltnet"},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}
