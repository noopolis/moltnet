package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const maxControlResponseBytes = 1 << 20

// ControlDelivery is the runtime-neutral wake material produced by the
// Moltnet stream. Runtime adapters own its wire encoding.
type ControlDelivery struct {
	Target        protocol.Target
	EventID       string
	From          string
	Message       string
	TransportText string
	OccurredAt    time.Time
}

// ControlResult tells the shared loop whether a decoded runtime result should
// be published back to the inbound Moltnet conversation.
type ControlResult struct {
	Text        string
	Publish     bool
	PublishOnce bool
	Acceptance  *ControlAcceptance
}

// ControlAcceptance is runtime-neutral durable acceptance metadata. A codec
// may use it to start asynchronous terminal-receipt delivery after the
// attachment has already accepted the inbound event.
type ControlAcceptance struct {
	ID            string
	AgentID       string
	DeliveryID    string
	RequestDigest string
	AcceptedAt    time.Time
}

// ControlCodec owns a runtime's request and response wire contracts. A nil
// request with no error intentionally skips a delivery, which lets runtimes
// without a bootstrap wire event opt out without branching in the loop.
type ControlCodec interface {
	EncodeRequest(context.Context, bridgeconfig.Config, ControlDelivery) (*http.Request, error)
	DecodeResponse(bridgeconfig.Config, ControlDelivery, *http.Response) (ControlResult, error)
}

// DurableAcceptanceCodec marks codecs whose successful response is the sole
// attachment ACK boundary. Failed acceptance must remain unacknowledged and
// replayable; it cannot be converted into a skipped delivery.
type DurableAcceptanceCodec interface {
	RequiresDurableAcceptance() bool
}

// AsyncControlCodec persists accepted work before the inbound attachment ACK
// and follows its terminal result independently. Moltnet remains transport;
// the runtime remains responsible for execution policy and scheduling.
type AsyncControlCodec interface {
	StartControlAsync(context.Context, *MoltnetClient, bridgeconfig.Config) error
	ControlAccepted(bridgeconfig.Config, protocol.Event, ControlAcceptance) error
}

// ControlPublicationObserver is an optional codec hook called only after a
// requested Moltnet publication succeeds.
type ControlPublicationObserver interface {
	ControlResultPublished(ControlDelivery)
}

// legacyControlCodec preserves the generic control-URL seam still used by
// TinyClaw and PicoClaw. Pi supplies its own byte-compatible codec instead.
type legacyControlCodec struct {
	publish bool
}

type controlRequest struct {
	ContextID     string `json:"context_id,omitempty"`
	EventID       string `json:"event_id,omitempty"`
	From          string `json:"from"`
	Message       string `json:"message"`
	TransportText string `json:"transport_text,omitempty"`
	To            string `json:"to"`
}

type controlResponse struct {
	From    string `json:"from"`
	Message string `json:"message"`
}

func (c *legacyControlCodec) EncodeRequest(
	ctx context.Context,
	config bridgeconfig.Config,
	delivery ControlDelivery,
) (*http.Request, error) {
	body, err := json.Marshal(controlRequest{
		ContextID:     conversationContextIDForTarget(config.Moltnet.NetworkID, delivery.Target),
		EventID:       delivery.EventID,
		From:          delivery.From,
		Message:       delivery.Message,
		TransportText: delivery.TransportText,
		To:            config.Agent.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode control request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, config.Runtime.ControlURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build control request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(config.Runtime.Token.Reveal()); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request, nil
}

func (c *legacyControlCodec) DecodeResponse(
	_ bridgeconfig.Config,
	_ ControlDelivery,
	response *http.Response,
) (ControlResult, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ControlResult{}, fmt.Errorf("control url returned %s", response.Status)
	}
	var payload controlResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, maxControlResponseBytes)).Decode(&payload); err != nil {
		return ControlResult{}, fmt.Errorf("decode control response: %w", err)
	}
	return ControlResult{Text: payload.Message, Publish: c.publish}, nil
}
