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
	defer cancel()
	finished := make(chan struct{})
	defer close(finished)

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
			err = c.readLoop(runCtx, conn, func() { attempt = 0 })
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
		c.clearConnection(nil)
		c.failPending(ErrClosed)
	})
}

func (c *Client) dial(ctx context.Context) (*websocket.Conn, error) {
	headers := make(http.Header)
	if token := strings.TrimSpace(c.token); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.relayURL, headers)
	if err != nil {
		return nil, fmt.Errorf("dial relay %s: %w", c.relayURL, err)
	}
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

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, onRead func()) error {
	for {
		if deadline, ok := ctx.Deadline(); ok {
			if err := conn.SetReadDeadline(deadline); err != nil {
				return fmt.Errorf("set relay read deadline: %w", err)
			}
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read relay frame: %w", err)
		}
		onRead()

		header, payload, err := parseFrame(raw)
		if err != nil {
			return err
		}
		if header.Type == "res" {
			c.dispatchResponse(header, payload)
		} else if header.Type == "req" {
			response, responseBody := c.inboundResponse(ctx, header, payload)
			frame, err := makeFrame(response, responseBody)
			if err != nil {
				return err
			}
			if err := c.writeFrame(ctx, conn, frame); err != nil {
				return err
			}
		}
	}
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
