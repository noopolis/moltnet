package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	attachmentHeartbeatIntervalMS = 5000
	attachmentWriteTimeout        = 10 * time.Second
	attachmentHelloTimeout        = 10 * time.Second
	maxPendingAttachmentAcks      = 1024
)

func handleAttachment(
	response http.ResponseWriter,
	request *http.Request,
	service Service,
	policy *authn.Policy,
	attachments *attachmentRegistry,
) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			if policy == nil {
				return true
			}
			return policy.CheckOrigin(r)
		},
	}

	connection, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		observability.Logger(request.Context(), "transport.attach").Error("upgrade attachment websocket", "error", err)
		return
	}
	defer connection.Close()
	connection.SetReadLimit(maxAttachmentFrameBytes)
	observability.DefaultMetrics.AddActiveAttachments(1)
	defer observability.DefaultMetrics.AddActiveAttachments(-1)

	writer := &attachmentWriter{connection: connection}
	if err := writer.write(protocol.AttachmentFrame{
		Op:                  protocol.AttachmentOpHello,
		Version:             protocol.AttachmentProtocolV1,
		HeartbeatIntervalMS: attachmentHeartbeatIntervalMS,
	}); err != nil {
		return
	}

	frame, err := readAttachmentFrame(connection, attachmentHelloTimeout)
	if err != nil {
		if message, ok := attachmentFrameErrorMessage(err); ok {
			_ = writer.write(protocol.AttachmentFrame{
				Op:    protocol.AttachmentOpError,
				Error: message,
			})
		}
		return
	}

	agent, err := identifiedAgent(frame, service.Network().ID)
	if err != nil {
		_ = writer.write(protocol.AttachmentFrame{
			Op:    protocol.AttachmentOpError,
			Error: err.Error(),
		})
		return
	}
	if claims, ok := authn.ClaimsFromContext(request.Context()); ok && !claims.AllowsAgent(agent.ID) {
		_ = writer.write(protocol.AttachmentFrame{
			Op:    protocol.AttachmentOpError,
			Error: fmt.Sprintf("attachment agent.id %q is not allowed for this token", agent.ID),
		})
		return
	}
	registration, err := service.RegisterAgentContext(request.Context(), protocol.RegisterAgentRequest{
		RequestedAgentID: agent.ID,
		Name:             agent.Name,
	})
	if err != nil {
		_ = writer.write(protocol.AttachmentFrame{
			Op:    protocol.AttachmentOpError,
			Error: err.Error(),
		})
		return
	}
	agent.ID = registration.AgentID
	agent.FQID = registration.ActorURI
	agent.NetworkID = registration.NetworkID
	if agent.Name == "" {
		agent.Name = registration.DisplayName
	}
	release, err := attachments.acquire(agent.ID, attachmentCredentialKey(request.Context()))
	if err != nil {
		_ = writer.write(protocol.AttachmentFrame{
			Op:    protocol.AttachmentOpError,
			Error: err.Error(),
		})
		return
	}
	defer release()
	session := newAttachmentSession(strings.TrimSpace(frame.Cursor))

	if err := writer.write(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpReady,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: service.Network().ID,
		AgentID:   agent.ID,
		ActorUID:  registration.ActorUID,
		ActorURI:  registration.ActorURI,
	}); err != nil {
		return
	}

	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()

	readErrCh := make(chan error, 1)
	go func() {
		readErrCh <- consumeAttachmentFrames(ctx, connection, writer, session, attachmentReadTimeout())
	}()

	heartbeatTicker := time.NewTicker(attachmentHeartbeatInterval() / 2)
	defer heartbeatTicker.Stop()

	stream := service.SubscribeFrom(ctx, session.ResumeCursor())
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-readErrCh:
			if err != nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				observability.Logger(request.Context(), "transport.attach", "agent_id", agent.ID).
					Warn("attachment websocket read error", "error", err)
			}
			return
		case <-heartbeatTicker.C:
			if err := writer.write(protocol.AttachmentFrame{
				Op:      protocol.AttachmentOpPing,
				Version: protocol.AttachmentProtocolV1,
			}); err != nil {
				return
			}
		case event, ok := <-stream:
			if !ok {
				return
			}
			session.NoteSent(event.ID)

			if err := writer.write(protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpEvent,
				Version:   protocol.AttachmentProtocolV1,
				NetworkID: service.Network().ID,
				Cursor:    event.ID,
				Event:     &event,
			}); err != nil {
				return
			}
		}
	}
}

type attachmentWriter struct {
	connection *websocket.Conn
	mu         sync.Mutex
}

func (w *attachmentWriter) write(frame protocol.AttachmentFrame) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.connection.SetWriteDeadline(time.Now().Add(attachmentWriteTimeout)); err != nil {
		return fmt.Errorf("set attachment write deadline: %w", err)
	}

	return w.connection.WriteJSON(frame)
}

func identifiedAgent(frame protocol.AttachmentFrame, networkID string) (protocol.Actor, error) {
	if frame.Op != protocol.AttachmentOpIdentify {
		return protocol.Actor{}, fmt.Errorf("expected %s frame", protocol.AttachmentOpIdentify)
	}
	if strings.TrimSpace(frame.Version) != protocol.AttachmentProtocolV1 {
		return protocol.Actor{}, fmt.Errorf("attachment version %q does not match %q", frame.Version, protocol.AttachmentProtocolV1)
	}

	if strings.TrimSpace(frame.NetworkID) != networkID {
		return protocol.Actor{}, fmt.Errorf("attachment network_id %q does not match %q", frame.NetworkID, networkID)
	}

	if frame.Agent == nil || strings.TrimSpace(frame.Agent.ID) == "" {
		return protocol.Actor{}, fmt.Errorf("attachment agent.id is required")
	}
	if err := protocol.ValidateMessageID(strings.TrimSpace(frame.Agent.ID)); err != nil {
		return protocol.Actor{}, fmt.Errorf("attachment agent.id %w", err)
	}

	agent := *frame.Agent
	if strings.TrimSpace(agent.Type) == "" {
		agent.Type = "agent"
	}

	return agent, nil
}

func consumeAttachmentFrames(
	ctx context.Context,
	connection *websocket.Conn,
	writer *attachmentWriter,
	session *attachmentSession,
	readTimeout time.Duration,
) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		frame, err := readAttachmentFrame(connection, readTimeout)
		if err != nil {
			if message, ok := attachmentFrameErrorMessage(err); ok {
				if writeErr := writer.write(protocol.AttachmentFrame{
					Op:    protocol.AttachmentOpError,
					Error: message,
				}); writeErr != nil {
					return writeErr
				}
			}
			return err
		}

		switch frame.Op {
		case protocol.AttachmentOpAck:
			if !session.Ack(frame.Cursor) {
				if err := writer.write(protocol.AttachmentFrame{
					Op:    protocol.AttachmentOpError,
					Error: "unexpected ACK cursor",
				}); err != nil {
					return err
				}
				return fmt.Errorf("unexpected ACK cursor %q", frame.Cursor)
			}
		case protocol.AttachmentOpPong:
			continue
		case protocol.AttachmentOpPing:
			if err := writer.write(protocol.AttachmentFrame{
				Op:      protocol.AttachmentOpPong,
				Version: protocol.AttachmentProtocolV1,
			}); err != nil {
				return err
			}
		default:
			if err := writer.write(protocol.AttachmentFrame{
				Op:    protocol.AttachmentOpError,
				Error: fmt.Sprintf("unexpected frame op %q", frame.Op),
			}); err != nil {
				return err
			}
			return fmt.Errorf("unexpected attachment frame op %q", frame.Op)
		}
	}
}

func readAttachmentFrame(connection *websocket.Conn, readTimeout time.Duration) (protocol.AttachmentFrame, error) {
	if err := connection.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return protocol.AttachmentFrame{}, fmt.Errorf("set attachment read deadline: %w", err)
	}

	messageType, payload, err := connection.ReadMessage()
	if err != nil {
		return protocol.AttachmentFrame{}, err
	}
	if messageType != websocket.TextMessage {
		return protocol.AttachmentFrame{}, invalidAttachmentFrameError("attachment protocol only accepts text JSON frames", nil)
	}

	var frame protocol.AttachmentFrame
	if err := json.Unmarshal(payload, &frame); err != nil {
		return protocol.AttachmentFrame{}, invalidAttachmentFrameError("attachment frame must be valid JSON", err)
	}

	return frame, nil
}

func attachmentHeartbeatInterval() time.Duration {
	return time.Duration(attachmentHeartbeatIntervalMS) * time.Millisecond
}

func attachmentReadTimeout() time.Duration {
	return attachmentHeartbeatInterval() * 2
}

type attachmentSession struct {
	mu           sync.Mutex
	resumeCursor string
	pending      map[string]struct{}
	order        []string
}

type attachmentFrameError struct {
	message string
	err     error
}

func (e *attachmentFrameError) Error() string {
	return e.message
}

func (e *attachmentFrameError) Unwrap() error {
	return e.err
}

func invalidAttachmentFrameError(message string, err error) error {
	return &attachmentFrameError{
		message: strings.TrimSpace(message),
		err:     err,
	}
}

func attachmentFrameErrorMessage(err error) (string, bool) {
	var frameErr *attachmentFrameError
	if !errors.As(err, &frameErr) || strings.TrimSpace(frameErr.message) == "" {
		return "", false
	}
	return frameErr.message, true
}

func newAttachmentSession(resumeCursor string) *attachmentSession {
	return &attachmentSession{
		resumeCursor: strings.TrimSpace(resumeCursor),
		pending:      make(map[string]struct{}),
		order:        make([]string, 0, maxPendingAttachmentAcks),
	}
}

func (s *attachmentSession) NoteSent(cursor string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return
	}
	if _, ok := s.pending[cursor]; ok {
		return
	}
	s.pending[cursor] = struct{}{}
	s.order = append(s.order, cursor)
	s.compactPendingLocked()
}

func (s *attachmentSession) Ack(cursor string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return false
	}
	if _, ok := s.pending[cursor]; !ok {
		return false
	}

	delete(s.pending, cursor)
	s.resumeCursor = cursor
	s.trimAckedPrefixLocked()
	return true
}

func (s *attachmentSession) ResumeCursor() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resumeCursor
}

func (s *attachmentSession) compactPendingLocked() {
	for len(s.pending) > maxPendingAttachmentAcks && len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		if _, ok := s.pending[oldest]; !ok {
			continue
		}
		delete(s.pending, oldest)
		s.resumeCursor = oldest
	}
}

func (s *attachmentSession) trimAckedPrefixLocked() {
	trim := 0
	for trim < len(s.order) {
		if _, ok := s.pending[s.order[trim]]; ok {
			break
		}
		trim++
	}
	if trim == 0 {
		return
	}
	if trim >= len(s.order) {
		s.order = make([]string, 0, maxPendingAttachmentAcks)
		return
	}
	remaining := append(make([]string, 0, len(s.order)-trim), s.order[trim:]...)
	s.order = remaining
}
