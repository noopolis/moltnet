package claudecode

import (
	"context"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func TestAdapterRequiresWorkspacePath(t *testing.T) {
	t.Parallel()

	adapter := New()
	if adapter.Name() != bridgeconfig.RuntimeClaudeCode {
		t.Fatalf("unexpected name %q", adapter.Name())
	}

	if err := adapter.Run(context.Background(), bridgeconfig.Config{}); err == nil {
		t.Fatal("expected missing workspace error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := adapter.Run(ctx, bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{WorkspacePath: t.TempDir()},
	}); err != nil {
		t.Fatalf("Run() canceled error = %v", err)
	}
}
