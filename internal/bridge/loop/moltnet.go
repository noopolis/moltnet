package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	defaultAttachmentHelloTimeout = 10 * time.Second
	defaultAttachmentReadTimeout  = 60 * time.Second
	attachmentWriteTimeout        = 10 * time.Second
	maxAttachmentFrameBytes       = 1 << 20
)

type MoltnetClient struct {
	baseURL  string
	client   *http.Client
	dialer   *websocket.Dialer
	token    string
	tokenErr error
	mu       sync.Mutex
	cursor   string
}

func NewMoltnetClient(config bridgeconfig.Config) *MoltnetClient {
	token, _, err := config.Moltnet.ResolveToken()
	return &MoltnetClient{
		baseURL:  strings.TrimRight(config.Moltnet.BaseURL, "/"),
		client:   &http.Client{Timeout: 15 * time.Second},
		dialer:   websocket.DefaultDialer,
		token:    token,
		tokenErr: err,
	}
}

func (c *MoltnetClient) StreamEvents(
	ctx context.Context,
	config bridgeconfig.Config,
	handle func(event protocol.Event) error,
) error {
	return c.StreamEventsReady(ctx, config, nil, handle)
}

func (c *MoltnetClient) StreamEventsReady(
	ctx context.Context,
	config bridgeconfig.Config,
	onReady func(),
	handle func(event protocol.Event) error,
) error {
	connection, response, err := c.connect(ctx, config)
	if err != nil {
		if response != nil {
			return fmt.Errorf("request moltnet attach: %s", response.Status)
		}
		return err
	}
	defer connection.Close()

	if err := connection.SetReadDeadline(time.Now().Add(defaultAttachmentHelloTimeout)); err != nil {
		return fmt.Errorf("set attachment hello read deadline: %w", err)
	}

	heartbeat, err := c.expectHello(connection)
	if err != nil {
		return err
	}
	readTimeout := heartbeatReadTimeout(heartbeat)
	if err := c.identify(connection, config); err != nil {
		return err
	}
	ready, err := c.expectReady(connection, readTimeout)
	if err != nil {
		return err
	}
	if err := c.applyReadyToken(config, ready.AgentToken); err != nil {
		return err
	}
	if onReady != nil {
		onReady()
	}

	var writeMu sync.Mutex
	write := func(frame protocol.AttachmentFrame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return c.writeFrame(connection, frame)
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-done:
		}
	}()
	defer close(done)

	frames := c.streamFrames(ctx, connection, readTimeout, write)
	for {
		select {
		case <-ctx.Done():
			return nil
		case result, ok := <-frames:
			if !ok {
				return nil
			}

			if result.err != nil {
				if ctx.Err() != nil {
					return nil
				}
				if websocket.IsCloseError(result.err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return nil
				}
				return result.err
			}

			switch result.frame.Op {
			case protocol.AttachmentOpEvent:
				if result.frame.Event == nil {
					return fmt.Errorf("attachment event frame is missing event payload")
				}
				if err := handle(*result.frame.Event); err != nil {
					return err
				}
				if err := write(protocol.AttachmentFrame{
					Op:      protocol.AttachmentOpAck,
					Version: protocol.AttachmentProtocolV1,
					Cursor:  result.frame.Cursor,
				}); err != nil {
					return err
				}
				c.setCursor(result.frame.Cursor)
			case protocol.AttachmentOpError:
				return fmt.Errorf("attachment gateway error: %s", strings.TrimSpace(result.frame.Error))
			default:
				return fmt.Errorf("unexpected attachment frame op %q", result.frame.Op)
			}
		}
	}
}

func (c *MoltnetClient) SendMessage(
	ctx context.Context,
	requestPayload protocol.SendMessageRequest,
) (protocol.MessageAccepted, error) {
	if err := c.resolveTokenErr(); err != nil {
		return protocol.MessageAccepted{}, err
	}

	body, err := json.Marshal(requestPayload)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("encode moltnet message: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/v1/messages",
		bytes.NewReader(body),
	)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("build moltnet message request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	if token := c.currentToken(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("request moltnet message send: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return protocol.MessageAccepted{}, fmt.Errorf("moltnet message send returned %s", response.Status)
	}

	var accepted protocol.MessageAccepted
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&accepted); err != nil {
		return protocol.MessageAccepted{}, fmt.Errorf("decode moltnet message response: %w", err)
	}

	return accepted, nil
}

func (c *MoltnetClient) connect(
	ctx context.Context,
	config bridgeconfig.Config,
) (*websocket.Conn, *http.Response, error) {
	if err := c.resolveTokenErr(); err != nil {
		return nil, nil, err
	}
	if err := c.prepareOpenClaim(config); err != nil {
		return nil, nil, err
	}

	endpoint, err := attachmentURL(c.baseURL)
	if err != nil {
		return nil, nil, err
	}

	headers := http.Header{}
	if token := c.currentToken(); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}

	connection, response, err := c.dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		return nil, response, fmt.Errorf("dial moltnet attachment: %w", err)
	}
	connection.SetReadLimit(maxAttachmentFrameBytes)

	return connection, response, nil
}

func (c *MoltnetClient) expectHello(connection *websocket.Conn) (time.Duration, error) {
	frame, err := c.readFrame(connection, defaultAttachmentHelloTimeout)
	if err != nil {
		return 0, err
	}
	if frame.Op != protocol.AttachmentOpHello {
		return 0, fmt.Errorf("expected %s frame, got %s", protocol.AttachmentOpHello, frame.Op)
	}
	return heartbeatInterval(frame.HeartbeatIntervalMS), nil
}

func (c *MoltnetClient) identify(connection *websocket.Conn, config bridgeconfig.Config) error {
	return c.writeFrame(connection, protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: config.Moltnet.NetworkID,
		Agent: &protocol.Actor{
			Type: "agent",
			ID:   config.Agent.ID,
			Name: bridgeutil.DisplayName(config.Agent),
		},
		Cursor: c.resumeCursor(),
		Capabilities: protocol.AttachmentCapabilities{
			Rooms:     len(config.Rooms) > 0,
			Threads:   attachmentSupportsThreads(config),
			DMs:       config.DMs != nil && config.DMs.Enabled,
			Artifacts: true,
		},
	})
}

func (c *MoltnetClient) expectReady(connection *websocket.Conn, readTimeout time.Duration) (protocol.AttachmentFrame, error) {
	frame, err := c.readFrame(connection, readTimeout)
	if err != nil {
		return protocol.AttachmentFrame{}, err
	}
	if frame.Op == protocol.AttachmentOpError {
		return protocol.AttachmentFrame{}, fmt.Errorf("attachment gateway error: %s", strings.TrimSpace(frame.Error))
	}
	if frame.Op != protocol.AttachmentOpReady {
		return protocol.AttachmentFrame{}, fmt.Errorf("expected %s frame, got %s", protocol.AttachmentOpReady, frame.Op)
	}
	return frame, nil
}

func (c *MoltnetClient) readFrame(
	connection *websocket.Conn,
	readTimeout time.Duration,
) (protocol.AttachmentFrame, error) {
	if err := connection.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return protocol.AttachmentFrame{}, fmt.Errorf("set attachment read deadline: %w", err)
	}

	messageType, payload, err := connection.ReadMessage()
	if err != nil {
		return protocol.AttachmentFrame{}, err
	}
	if messageType != websocket.TextMessage {
		return protocol.AttachmentFrame{}, fmt.Errorf("attachment protocol only accepts text JSON frames")
	}

	var frame protocol.AttachmentFrame
	if err := json.Unmarshal(payload, &frame); err != nil {
		return protocol.AttachmentFrame{}, fmt.Errorf("decode attachment frame: %w", err)
	}

	return frame, nil
}

func (c *MoltnetClient) writeFrame(connection *websocket.Conn, frame protocol.AttachmentFrame) error {
	if err := connection.SetWriteDeadline(time.Now().Add(attachmentWriteTimeout)); err != nil {
		return fmt.Errorf("set attachment write deadline: %w", err)
	}
	if err := connection.WriteJSON(frame); err != nil {
		return fmt.Errorf("write attachment frame: %w", err)
	}

	return nil
}

func (c *MoltnetClient) setCursor(cursor string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cursor = strings.TrimSpace(cursor)
}

func (c *MoltnetClient) resumeCursor() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cursor
}

func attachmentSupportsThreads(config bridgeconfig.Config) bool {
	for _, binding := range config.Rooms {
		if bridgeutil.ShouldReply(binding.Reply) {
			return true
		}
	}

	return false
}

func heartbeatInterval(valueMS int) time.Duration {
	if valueMS <= 0 {
		return defaultAttachmentReadTimeout / 2
	}

	return time.Duration(valueMS) * time.Millisecond
}

func heartbeatReadTimeout(interval time.Duration) time.Duration {
	if interval <= 0 {
		return defaultAttachmentReadTimeout
	}

	timeout := interval * 2
	if timeout < defaultAttachmentHelloTimeout {
		return defaultAttachmentHelloTimeout
	}

	return timeout
}

func attachmentURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse moltnet base url: %w", err)
	}

	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported moltnet base url scheme %q", parsed.Scheme)
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/attach"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
