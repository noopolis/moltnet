package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/noopolis/moltnet/internal/bridge"
)

// Run keeps a connection to the relay open until ctx is canceled or Close is called.
func (c *Client) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	state := &clientRun{
		cancel:  cancel,
		inbound: newInboundDispatcher(8),
	}
	if !c.startRun(state) {
		cancel()
		return nil
	}
	finished := make(chan struct{})
	defer func() {
		cancel()
		c.clearConnection(nil)
		state.inbound.stopAndWait()
		c.finishRun(state)
		close(finished)
	}()

	go func() {
		select {
		case <-c.stop:
			cancel()
		case <-runCtx.Done():
		}
	}()
	go func() {
		select {
		case <-runCtx.Done():
			c.clearConnection(nil)
		case <-finished:
		}
	}()

	backoff := bridge.NewBackoff(bridge.DefaultReconnectBaseDelay, bridge.DefaultReconnectMaxDelay)
	attempt := 0
	for {
		if runCtx.Err() != nil {
			return nil
		}

		conn, err := c.dial(runCtx)
		if err == nil {
			err = c.sendHello(runCtx, conn)
		}
		if err == nil {
			err = c.setConnection(runCtx, conn)
		}
		live := err == nil
		if err == nil {
			attempt = 0
			err = c.readLoop(runCtx, conn, state.inbound, func() { attempt = 0 })
		}
		c.clearConnection(conn)
		if !live && conn != nil {
			_ = conn.Close()
		}

		if runCtx.Err() != nil {
			return nil
		}
		if live {
			c.failPending(ErrConnectionLost)
		}
		attempt++
		select {
		case <-runCtx.Done():
			return nil
		case <-time.After(backoff.Delay(attempt)):
		}
	}
}

// Close stops Run, closes the live WebSocket connection, and unblocks pending calls.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.stop)
		state := c.activeRun()
		if state != nil {
			state.cancel()
		}
		c.clearConnection(nil)
		c.failPending(ErrClosed)
		if state != nil {
			state.inbound.stopAndWait()
		}
	})
}

type clientRun struct {
	cancel  context.CancelFunc
	inbound *inboundDispatcher
}

func (c *Client) startRun(state *clientRun) bool {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if c.closed.Load() {
		return false
	}
	c.run = state
	return true
}

func (c *Client) finishRun(state *clientRun) {
	c.runMu.Lock()
	if c.run == state {
		c.run = nil
	}
	c.runMu.Unlock()
}

func (c *Client) activeRun() *clientRun {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	return c.run
}

func (c *Client) dial(ctx context.Context) (*websocket.Conn, error) {
	headers := make(http.Header)
	if token := strings.TrimSpace(c.relayToken); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.relayURL, headers)
	if err != nil {
		return nil, fmt.Errorf("dial relay %s: %w", c.relayURL, err)
	}
	conn.SetReadLimit(maxRelayFrameBytes)
	return conn, nil
}

func (c *Client) setConnection(ctx context.Context, conn *websocket.Conn) error {
	if conn == nil {
		return ErrNotConnected
	}
	if ctx.Err() != nil || c.closed.Load() {
		_ = conn.Close()
		return ErrClosed
	}

	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.closed.Load() {
		_ = conn.Close()
		return ErrClosed
	}
	c.conn = conn
	return nil
}

func (c *Client) clearConnection(expected *websocket.Conn) {
	c.connMu.Lock()
	conn := c.conn
	if expected != nil && conn != expected {
		c.connMu.Unlock()
		return
	}
	c.conn = nil
	c.connMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (c *Client) sendHello(ctx context.Context, conn *websocket.Conn) error {
	frame, err := makeFrame(frameHeader{Type: "hello", Network: c.network}, nil)
	if err != nil {
		return err
	}
	return c.writeFrame(ctx, conn, frame)
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, inbound *inboundDispatcher, onRead func()) error {
	refreshReadDeadline := func() error {
		if err := conn.SetReadDeadline(time.Now().Add(c.readIdleTimeout)); err != nil {
			return fmt.Errorf("set relay read deadline: %w", err)
		}
		return nil
	}
	if err := refreshReadDeadline(); err != nil {
		return err
	}
	conn.SetPongHandler(func(string) error {
		return refreshReadDeadline()
	})

	done := make(chan struct{})
	pingStopped := make(chan struct{})
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()
	go func() {
		defer close(pingStopped)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				if err := c.writePing(conn); err != nil && !c.closed.Load() {
					// Closing unblocks ReadMessage so Run can use its normal
					// reconnect and pending-call failure path.
					c.clearConnection(conn)
					return
				}
			}
		}
	}()
	defer func() {
		close(done)
		<-pingStopped
	}()

	for {
		if err := refreshReadDeadline(); err != nil {
			return err
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read relay frame: %w", err)
		}
		if err := refreshReadDeadline(); err != nil {
			return err
		}
		onRead()

		header, payload, err := parseFrame(raw)
		if err != nil {
			return err
		}
		if header.Type == "res" {
			c.dispatchResponse(header, payload)
		} else if header.Type == "req" {
			if !inbound.start(ctx, func() {
				c.serveInboundRequest(ctx, conn, header, payload)
			}) {
				return nil
			}
		}
	}
}

func (c *Client) writePing(conn *websocket.Conn) error {
	if conn == nil {
		return ErrNotConnected
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() {
		return ErrClosed
	}
	if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(pingWriteTimeout)); err != nil {
		return fmt.Errorf("write relay ping: %w", err)
	}
	return nil
}

func (c *Client) serveInboundRequest(ctx context.Context, conn *websocket.Conn, header frameHeader, payload []byte) {
	response, responseBody := c.inboundResponse(ctx, header, payload)
	frame, err := makeFrame(response, responseBody)
	if err != nil || ctx.Err() != nil || c.closed.Load() || !c.isCurrentConnection(conn) {
		return
	}
	if err := c.writeFrame(ctx, conn, frame); err != nil && ctx.Err() == nil && !c.closed.Load() {
		c.clearConnection(conn)
	}
}

func (c *Client) isCurrentConnection(conn *websocket.Conn) bool {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return conn != nil && c.conn == conn
}

func parseFrame(raw []byte) (frameHeader, []byte, error) {
	headerEnd := bytes.IndexByte(raw, '\n')
	if headerEnd < 0 {
		headerEnd = len(raw)
	}
	if headerEnd > maxHeaderSize {
		return frameHeader{}, nil, fmt.Errorf("relay header exceeds %d bytes", maxHeaderSize)
	}

	var header frameHeader
	if err := json.Unmarshal(raw[:headerEnd], &header); err != nil {
		return frameHeader{}, nil, fmt.Errorf("decode relay header: %w", err)
	}
	if bytes.IndexByte(raw, '\n') < 0 {
		return header, nil, nil
	}
	return header, append([]byte(nil), raw[headerEnd+1:]...), nil
}
