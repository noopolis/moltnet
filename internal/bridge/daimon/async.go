package daimon

import (
	"context"
	"fmt"
	"strings"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (c *Codec) StartControlAsync(ctx context.Context, client *loop.MoltnetClient, config bridgeconfig.Config) error {
	if c.receipts != nil {
		return fmt.Errorf("daimon receipt follower is already running")
	}
	store, err := openReceiptStore(strings.TrimSpace(c.receiptStorePath))
	if err != nil {
		return err
	}
	if err := store.ValidateAuthority(config.Agent.ID, runtimeAgentID(config)); err != nil {
		return err
	}
	c.receipts = newReceiptTracker(store, c.token, client, config)
	go c.receipts.Run(ctx)
	return nil
}

func (c *Codec) ControlAccepted(config bridgeconfig.Config, event protocol.Event, acceptance loop.ControlAcceptance) error {
	if c.receipts == nil {
		return fmt.Errorf("daimon receipt follower is not running")
	}
	if event.Message == nil {
		return fmt.Errorf("daimon receipt source event has no message")
	}
	acceptedAt := acceptance.AcceptedAt.UTC()
	if acceptedAt.IsZero() {
		acceptedAt = time.Now().UTC()
	}
	job := receiptJob{
		AcceptanceID:   acceptance.ID,
		RuntimeAgentID: acceptance.AgentID,
		DeliveryID:     acceptance.DeliveryID,
		RequestDigest:  acceptance.RequestDigest,
		MoltnetAgent: protocol.Actor{
			Type: "agent",
			ID:   config.Agent.ID,
			Name: bridgeutil.DisplayName(config.Agent),
		},
		Event: protocol.Event{
			ID:        event.ID,
			Type:      event.Type,
			NetworkID: event.NetworkID,
			Message: &protocol.Message{
				ID:        event.Message.ID,
				NetworkID: event.Message.NetworkID,
				Target:    event.Message.Target,
				Mentions:  append([]string(nil), event.Message.Mentions...),
			},
			CreatedAt: event.CreatedAt,
		},
		State:      receiptJobPending,
		AcceptedAt: acceptedAt,
		UpdatedAt:  acceptedAt,
	}
	return c.receipts.Accept(job)
}
