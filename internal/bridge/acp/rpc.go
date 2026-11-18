package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
)

const (
	rpcVersion     = "2.0"
	maxFrameBytes  = 16 << 20
	controlTimeout = 2 * time.Second
)

type rpcMessage struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

type rpcRequest struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  any             `json:"params"`
}

type rpcResponse struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (c *connection) call(ctx context.Context, method string, params, result any, cancelSession string) error {
	responses := make(chan rpcMessage, 1)
	c.mu.Lock()
	c.nextID++
	id := strconv.FormatUint(c.nextID, 10)
	c.pending[id] = responses
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	request := rpcRequest{Version: rpcVersion, ID: json.RawMessage(id), Method: method, Params: params}
	if err := c.send(ctx, request); err != nil {
		return fmt.Errorf("ACP %s: %w", method, err)
	}

	var response rpcMessage
	select {
	case response = <-responses:
	case <-ctx.Done():
		if cancelSession != "" {
			c.cancelPrompt(cancelSession, responses)
		} else {
			c.stop(ctx.Err())
		}
		return fmt.Errorf("ACP %s: %w", method, ctx.Err())
	case <-c.done:
		// The agent can close stdout immediately after replying.
		select {
		case response = <-responses:
		default:
			return fmt.Errorf("ACP %s: %w", method, c.err)
		}
	}

	if response.Error != nil {
		return fmt.Errorf("ACP %s: %w", method, response.Error)
	}
	if len(response.Result) == 0 {
		return fmt.Errorf("ACP %s: missing result", method)
	}
	if result != nil {
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode ACP %s result: %w", method, err)
		}
	}
	return nil
}

func (c *connection) cancelPrompt(sessionID string, responses <-chan rpcMessage) {
	select {
	case <-responses:
		return
	default:
	}

	c.mu.Lock()
	c.cancelling[sessionID] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.cancelling, sessionID)
		c.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()
	request := rpcRequest{
		Version: rpcVersion,
		Method:  "session/cancel",
		Params:  sessionReference{SessionID: sessionID},
	}
	if err := c.send(ctx, request); err != nil {
		c.stop(err)
		return
	}
	select {
	case <-responses:
		// Even if the turn already completed when we cancelled, its reply
		// is the agent's signal that this session can start another turn.
	case <-c.done:
	case <-ctx.Done():
		c.stop(fmt.Errorf("ACP agent did not finish the cancelled prompt: %w", ctx.Err()))
	}
}

func (c *connection) send(ctx context.Context, payload any) error {
	select {
	case c.write <- struct{}{}:
		defer func() { <-c.write }()
	case <-c.done:
		return c.err
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.isClosed() {
		return c.err
	}
	// Closing the pipe interrupts a blocked write even if the agent has
	// stopped reading its input. A partially written frame is unusable.
	stopOnCancel := context.AfterFunc(ctx, func() { c.stop(ctx.Err()) })
	defer stopOnCancel()
	if err := json.NewEncoder(c.input).Encode(payload); err != nil {
		c.stop(fmt.Errorf("write ACP frame: %w", err))
		return c.err
	}
	return nil
}

func (c *connection) readMessages() error {
	scanner := bufio.NewScanner(c.output)
	scanner.Buffer(make([]byte, 4096), maxFrameBytes)
	for scanner.Scan() {
		var message rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			return fmt.Errorf("decode ACP frame: %w", err)
		}
		if message.Version != rpcVersion {
			return fmt.Errorf("ACP agent sent a frame without JSON-RPC 2.0")
		}
		if message.Method != "" {
			if len(message.ID) == 0 {
				// Notifications, including agent output, are not Moltnet replies.
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
			err := c.send(ctx, c.clientResponse(message))
			cancel()
			if err != nil {
				return err
			}
			continue
		}
		c.mu.Lock()
		waiter := c.pending[string(message.ID)]
		delete(c.pending, string(message.ID))
		c.mu.Unlock()
		if waiter != nil {
			waiter <- message
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read ACP frame: %w", err)
	}
	return fmt.Errorf("ACP agent closed stdout: %w", io.EOF)
}

func (c *connection) clientResponse(request rpcMessage) rpcResponse {
	if request.Method != "session/request_permission" {
		return rpcResponse{
			Version: rpcVersion, ID: request.ID,
			Error: &rpcError{Code: -32601, Message: "Method not found"},
		}
	}
	return rpcResponse{
		Version: rpcVersion, ID: request.ID,
		Result: map[string]any{"outcome": c.deniedPermission(request.Params)},
	}
}

func (c *connection) deniedPermission(params json.RawMessage) map[string]string {
	var request struct {
		SessionID string `json:"sessionId"`
		Options   []struct {
			ID   string `json:"optionId"`
			Kind string `json:"kind"`
		} `json:"options"`
	}
	cancelled := map[string]string{"outcome": "cancelled"}
	if err := json.Unmarshal(params, &request); err != nil {
		return cancelled
	}
	c.mu.Lock()
	cancelling := c.cancelling[request.SessionID]
	c.mu.Unlock()
	if cancelling {
		return cancelled
	}
	for _, option := range request.Options {
		if option.Kind == "reject_once" && option.ID != "" {
			return map[string]string{"outcome": "selected", "optionId": option.ID}
		}
	}
	return cancelled
}
