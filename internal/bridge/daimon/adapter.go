package daimon

import (
	"context"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (*Adapter) Name() string { return bridgeconfig.RuntimeDaimon }

func (*Adapter) Run(ctx context.Context, config bridgeconfig.Config) error {
	if strings.TrimSpace(config.Runtime.ControlURL) == "" {
		return fmt.Errorf("daimon adapter requires runtime.control_url")
	}
	if strings.TrimSpace(config.Runtime.ReceiptStorePath) == "" {
		return fmt.Errorf("daimon adapter requires runtime.receipt_store_path")
	}
	token, err := config.Runtime.ResolveRuntimeToken()
	if err != nil {
		return fmt.Errorf("daimon adapter: %w", err)
	}
	return loop.RunControlLoopWithCodec(ctx, config, newRuntimeCodec(token, config.Runtime.ReceiptStorePath))
}
