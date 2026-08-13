package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// ReportWakeFailed posts a terminal (permanent or retry-budget-exhausted)
// control-delivery wake failure to POST /v1/agents/wake-failed, so the
// server records agent.wake.failed for an event the bridge already ACKed
// and skipped, without tearing down the attachment socket to do so. See
// internal/bridge/loop/control_delivery.go's reportWakeFailure, the sole
// caller.
func (c *MoltnetClient) ReportWakeFailed(
	ctx context.Context,
	requestPayload protocol.ReportWakeFailedRequest,
) error {
	if err := c.resolveTokenErr(); err != nil {
		return err
	}

	body, err := json.Marshal(requestPayload)
	if err != nil {
		return fmt.Errorf("encode wake failed report: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/agents/wake-failed",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("build wake failed report request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	if token := c.currentToken(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("request wake failed report: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("wake failed report returned %s", response.Status)
	}

	return nil
}
