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
	maxHeaderSize      = 8192
	defaultCallTimeout = 15 * time.Second
)

var (
	// ErrNotConnected is returned when Call is made while the relay is reconnecting.
	ErrNotConnected   = errors.New("relay is not connected")
	ErrConnectionLost = errors.New("relay connection lost")
	ErrClosed         = errors.New("relay client is closed")
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

// WithInboundHandler serves requests sent by the relay peer through handler.
// The client pairing token is attached to each synthetic HTTP request.
func WithInboundHandler(handler http.Handler) ClientOption {
	return func(client *Client) {
		client.inbound = NewInboundHandler(handler, client.token)
	}
}

// Client maintains an outbound connection to one relay room.
type Client struct {
	relayURL string
	token    string
	network  string

	connMu sync.RWMutex
	conn   *websocket.Conn

	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan callResult
	nextID    atomic.Uint64

	stop      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
	inbound   *InboundHandler
}

// NewClient creates a relay client. Run must be started before calls can use it.
func NewClient(relayURL, bearerToken, network string, options ...ClientOption) *Client {
	client := &Client{
		relayURL: relayURL,
		token:    bearerToken,
		network:  network,
		pending:  make(map[string]chan callResult),
		stop:     make(chan struct{}),
	}
	for _, option := range options {
		if option != nil {
			option(client)
		}
	}
	return client
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
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

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
