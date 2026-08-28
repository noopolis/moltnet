package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const maxControlResponseBytes = 1 << 20

type Codec struct{}

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

func NewCodec() *Codec { return &Codec{} }

func (*Codec) EncodeRequest(
	ctx context.Context,
	config bridgeconfig.Config,
	delivery loop.ControlDelivery,
) (*http.Request, error) {
	body, err := json.Marshal(controlRequest{
		ContextID:     conversationContextID(config.Moltnet.NetworkID, delivery),
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

func (*Codec) DecodeResponse(
	config bridgeconfig.Config,
	_ loop.ControlDelivery,
	response *http.Response,
) (loop.ControlResult, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return loop.ControlResult{}, fmt.Errorf("control url returned %s", response.Status)
	}
	var payload controlResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, maxControlResponseBytes)).Decode(&payload); err != nil {
		return loop.ControlResult{}, fmt.Errorf("decode control response: %w", err)
	}
	return loop.ControlResult{
		Text:    payload.Message,
		Publish: config.Runtime.Kind == bridgeconfig.RuntimePi,
	}, nil
}

func conversationContextID(networkID string, delivery loop.ControlDelivery) string {
	target := delivery.Target
	switch target.Kind {
	case protocol.TargetKindRoom:
		if target.RoomID != "" {
			return fmt.Sprintf("moltnet:%s:room:%s", networkID, target.RoomID)
		}
	case protocol.TargetKindThread:
		if target.ThreadID != "" {
			return fmt.Sprintf("moltnet:%s:thread:%s", networkID, target.ThreadID)
		}
	case protocol.TargetKindDM:
		if target.DMID != "" {
			return fmt.Sprintf("moltnet:%s:dm:%s", networkID, target.DMID)
		}
	}
	return ""
}
