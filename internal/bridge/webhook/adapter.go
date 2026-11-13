// Package webhook wakes an external runtime, such as a Grok Bot Routine.
// A successful HTTP response is acceptance, not an AI reply.
package webhook

import (
	"context"

	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type Adapter struct{}

func New() *Adapter           { return &Adapter{} }
func (*Adapter) Name() string { return bridgeconfig.RuntimeWebhook }
func (*Adapter) Run(ctx context.Context, config bridgeconfig.Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	key, err := config.Runtime.ResolveRuntimeToken()
	if err != nil {
		return err
	}
	unlock, err := lockReceipts(config.Runtime.ReceiptStorePath)
	if err != nil {
		return err
	}
	defer unlock()
	codec, err := newCodec(config.Runtime.ReceiptStorePath, key.Reveal(), config.Runtime.MaxRunsPerDay)
	if err != nil {
		return err
	}
	return loop.RunControlLoopWithCodec(ctx, config, codec)
}
