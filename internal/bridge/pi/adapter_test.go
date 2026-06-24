package pi

import (
	"context"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func TestAdapter(t *testing.T) {
	t.Parallel()

	adapter := New()
	if adapter.Name() != bridgeconfig.RuntimePi {
		t.Fatalf("unexpected name %q", adapter.Name())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.Run(ctx, bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{
			Kind:       bridgeconfig.RuntimePi,
			ControlURL: "http://127.0.0.1:9000/control",
		},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://moltnet"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if err := adapter.Run(context.Background(), bridgeconfig.Config{}); err == nil {
		t.Fatal("expected missing control url error")
	}
}
