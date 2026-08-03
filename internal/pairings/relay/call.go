package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxHeaderSize               = 8192
	maxRelayPayloadBytes        = 1 << 20                                  // One MiB matches the service's normal JSON and attachment frame allowance.
	maxRelayFrameBytes          = maxHeaderSize + 1 + maxRelayPayloadBytes // Includes the largest JSON header and its newline delimiter.
	maxInboundResponseBodyBytes = maxRelayPayloadBytes                     // Bound relayed handler buffering to a normal Moltnet response size.
	defaultCallTimeout          = 15 * time.Second

	// Ping every 25 seconds and declare a connection idle after 90 seconds:
	// this leaves room for scheduling jitter and several missed pings while
	// remaining below common 100-second-plus proxy and NAT idle windows.
	defaultPingInterval    = 25 * time.Second
	defaultReadIdleTimeout = 90 * time.Second
	pingWriteTimeout       = 5 * time.Second
)

var (
	// ErrNotConnected is returned when Call is made while the relay is reconnecting.
	ErrNotConnected    = errors.New("relay is not connected")
	ErrConnectionLost  = errors.New("relay connection lost")
	ErrClosed          = errors.New("relay client is closed")
	ErrPayloadTooLarge = errors.New("relay payload too large")
)

type frameHeader struct {
	Type    string `json:"t"`
	ID      string `json:"id,omitempty"`
	Method  string `json:"method,omitempty"`
	Path    string `json:"path,omitempty"`
	Status  int    `json:"status,omitempty"`
	Network string `json:"network,omitempty"`
}

type callResult struct {
	status int
	body   []byte
	err    error
}

// ClientOption configures a relay Client.
type ClientOption func(*Client)

// withKeepaliveDurations overrides the connection liveness timings for one
// client. It is intentionally unexported so production callers retain the
// stable Client API while package tests can use short, deterministic timings.
func withKeepaliveDurations(ping, readIdle time.Duration) ClientOption {
	return func(client *Client) {
		if ping > 0 {
			client.pingInterval = ping
		}
		if readIdle > 0 {
			client.readIdleTimeout = readIdle
		}
	}
}

// WithInboundHandler serves requests sent by the relay peer through handler.
// The client pairing token is attached to each synthetic HTTP request. Handlers run
// on bounded per-request goroutines and may call Call and its helpers through this Client.
// A handler must not synchronously call this Client's Close: Close waits for in-flight
// handlers, including the caller, to return.
func WithInboundHandler(handler http.Handler) ClientOption {
	return func(client *Client) {
		client.SetHandler(handler)
	}
}

// Client maintains an outbound connection to one relay room.
type Client struct {
	relayURL string
	token    string
	network  string

	pingInterval    time.Duration
	readIdleTimeout time.Duration

	connMu sync.RWMutex
	conn   *websocket.Conn

	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan callResult
	nextID    atomic.Uint64

	stop      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
	runMu     sync.Mutex
	run       *clientRun
	inboundMu sync.RWMutex
	inbound   *InboundHandler
}

// NewClient creates a relay client. Run must be started before calls can use it.
func NewClient(relayURL, bearerToken, network string, options ...ClientOption) *Client {
	client := &Client{
		relayURL:        relayURL,
		token:           bearerToken,
		network:         network,
		pingInterval:    defaultPingInterval,
		readIdleTimeout: defaultReadIdleTimeout,
		pending:         make(map[string]chan callResult),
		stop:            make(chan struct{}),
	}
	for _, option := range options {
		if option != nil {
			option(client)
		}
	}
	return client
}

// SetHandler serves requests received from the relay peer through handler.
// It may be called after NewClient to resolve startup ordering with the
// application's HTTP handler.
func (c *Client) SetHandler(handler http.Handler) {
	c.inboundMu.Lock()
	c.inbound = NewInboundHandler(handler, c.token)
	c.inboundMu.Unlock()
}

// Call sends a request through the current relay connection and waits for its response.
// Calls made while the client is reconnecting return ErrNotConnected rather than waiting
// for a future connection.
func (c *Client) Call(
	ctx context.Context,
	method string,
	path string,
	body []byte,
) (status int, respBody []byte, err error) {
	if len(body) > maxRelayPayloadBytes {
		return 0, nil, fmt.Errorf("relay payload exceeds %d bytes: got %d bytes: %w", maxRelayPayloadBytes, len(body), ErrPayloadTooLarge)
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	if c.closed.Load() {
		return 0, nil, ErrClosed
	}

	id := fmt.Sprintf("req-%d-%d", time.Now().UnixNano(), c.nextID.Add(1))
	result := make(chan callResult, 1)
	c.pendingMu.Lock()
	c.pending[id] = result
	c.pendingMu.Unlock()
	defer c.removePending(id, result)

	conn := c.currentConnection()
	if conn == nil {
		return 0, nil, ErrNotConnected
	}

	frame, err := makeFrame(frameHeader{
		Type:   "req",
		ID:     id,
		Method: method,
		Path:   path,
	}, body)
	if err != nil {
		return 0, nil, err
	}
	if err := c.writeFrame(ctx, conn, frame); err != nil {
		return 0, nil, err
	}

	select {
	case response := <-result:
		return response.status, response.body, response.err
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func (c *Client) currentConnection() *websocket.Conn {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.conn
}

func (c *Client) removePending(id string, result chan callResult) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if c.pending[id] == result {
		delete(c.pending, id)
	}
}

func (c *Client) dispatchResponse(header frameHeader, body []byte) {
	c.pendingMu.Lock()
	result, ok := c.pending[header.ID]
	if ok {
		delete(c.pending, header.ID)
	}
	c.pendingMu.Unlock()
	if !ok {
		return
	}

	responseBody := append([]byte(nil), body...)
	result <- callResult{status: header.Status, body: responseBody}
}

func (c *Client) failPending(err error) {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[string]chan callResult)
	c.pendingMu.Unlock()

	for _, result := range pending {
		result <- callResult{err: err}
	}
}

func (c *Client) writeFrame(ctx context.Context, conn *websocket.Conn, frame []byte) error {
	if conn == nil {
		return ErrNotConnected
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closed.Load() {
		return ErrClosed
	}

	if err := conn.SetWriteDeadline(resolveDeadline(ctx)); err != nil {
		return fmt.Errorf("set relay write deadline: %w", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return fmt.Errorf("write relay frame: %w", err)
	}
	return nil
}

func makeFrame(header frameHeader, payload []byte) ([]byte, error) {
	rawHeader, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("marshal relay header: %w", err)
	}
	if len(rawHeader) > maxHeaderSize {
		return nil, fmt.Errorf("relay header exceeds %d bytes", maxHeaderSize)
	}
	if len(payload) == 0 {
		return rawHeader, nil
	}

	frame := make([]byte, 0, len(rawHeader)+1+len(payload))
	frame = append(frame, rawHeader...)
	frame = append(frame, '\n')
	frame = append(frame, payload...)
	return frame, nil
}

func resolveDeadline(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Now().Add(defaultCallTimeout)
}
