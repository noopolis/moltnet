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
			_ = writeAttachmentError(writer, message)
		}
		return
	}

	agent, err := identifiedAgent(frame, service.Network().ID)
	if err != nil {
		_ = writeAttachmentError(writer, err.Error())
		return
	}
	if claims, ok := authn.ClaimsFromContext(request.Context()); ok && !claims.AllowsAgent(agent.ID) {
		_ = writeAttachmentError(writer, fmt.Sprintf("attachment agent.id %q is not allowed for this token", agent.ID))
		return
	}
	registration, err := service.RegisterAgentContext(request.Context(), protocol.RegisterAgentRequest{
		RequestedAgentID: agent.ID,
		Name:             agent.Name,
	})
	if err != nil {
		message := err.Error()
		if policy != nil && policy.AgentRegistration() == authn.AgentRegistrationOpen {
			if _, ok := authn.ClaimsFromContext(request.Context()); !ok && strings.Contains(message, "already registered") {
				message = fmt.Sprintf("agent %q requires its agent token", agent.ID)
			}
		}
		_ = writeAttachmentError(writer, message)
		return
	}
	agent.ID = registration.AgentID
	agent.FQID = registration.ActorURI
	agent.NetworkID = registration.NetworkID
	if agent.Name == "" {
		agent.Name = registration.DisplayName
	}
	release, err := attachments.acquire(
		agent.ID,
		attachmentCredentialForRegistration(request.Context(), registration.CredentialKey),
	)
	if err != nil {
		_ = writeAttachmentError(writer, err.Error())
		return
	}
	defer release()
	session := newAttachmentSession(strings.TrimSpace(frame.Cursor))

	if err := writer.write(protocol.AttachmentFrame{
		Op:         protocol.AttachmentOpReady,
		Version:    protocol.AttachmentProtocolV1,
		NetworkID:  service.Network().ID,
		AgentID:    agent.ID,
		ActorUID:   registration.ActorUID,
		ActorURI:   registration.ActorURI,
		AgentToken: registration.AgentToken,
	}); err != nil {
		return
	}
	service.AgentConnected(request.Context(), agent)
	disconnectReason := "attachment_closed"
	var disconnectErr error
	defer func() {
		service.AgentDisconnected(context.Background(), agent, disconnectReason, disconnectErr)
	}()

	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()

	readErrCh := make(chan error, 1)
	go func() {
		readErrCh <- consumeAttachmentFrames(ctx, connection, writer, session, attachmentReadTimeout(), func(event protocol.Event) {
			service.AgentWakeDelivered(context.Background(), agent, event)
		})
	}()

	heartbeatTicker := time.NewTicker(attachmentHeartbeatInterval() / 2)
	defer heartbeatTicker.Stop()

	filter := attachmentEventFilter(policy, request.Context(), service, service.Network().ID, agent.ID)
	stream := service.SubscribeFrom(ctx, session.ResumeCursor())
	for {
		select {
		case <-ctx.Done():
			disconnectReason = "request_context_done"
			disconnectErr = ctx.Err()
			return
		case err := <-readErrCh:
			disconnectReason, disconnectErr = attachmentDisconnectCause(err)
			if err != nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				observability.Logger(request.Context(), "transport.attach", "agent_id", agent.ID).
					Warn("attachment websocket read error", "error", err)
			}
			publishPendingWakeFailures(service, agent, session, err)
			return
		case <-heartbeatTicker.C:
			if err := writer.write(protocol.AttachmentFrame{
				Op:      protocol.AttachmentOpPing,
				Version: protocol.AttachmentProtocolV1,
			}); err != nil {
				disconnectReason = "heartbeat_write_failed"
				disconnectErr = err
				publishPendingWakeFailures(service, agent, session, err)
				return
			}
		case event, ok := <-stream:
			if !ok {
				disconnectReason = "event_stream_closed"
				return
			}
			if attachmentRemovedEvent(event, service.Network().ID, agent.ID) {
				disconnectReason = "agent_removed"
				disconnectErr = writeAttachmentError(writer, "agent was removed from the network")
				return
			}
			if filter != nil && !filter(request.Context(), event) {
				continue
			}
			session.NoteSent(event.ID)
			if attachmentWakeEvent(event, service.Network().ID, agent) {
				session.NoteWakeSent(event.ID, event)
			}

			if err := writer.write(protocol.AttachmentFrame{
				Op:        protocol.AttachmentOpEvent,
				Version:   protocol.AttachmentProtocolV1,
				NetworkID: service.Network().ID,
				Cursor:    event.ID,
				Event:     &event,
			}); err != nil {
				disconnectReason = "event_write_failed"
				disconnectErr = err
				publishPendingWakeFailures(service, agent, session, err)
				return
			}
		}
	}
}

func attachmentDisconnectCause(err error) (string, error) {
	if err == nil {
		return "client_closed", nil
	}
	var clientErr attachmentClientError
	switch {
	case errors.As(err, &clientErr):
		return "client_error", err
	case websocket.IsCloseError(err, websocket.CloseNormalClosure):
		return "client_closed", nil
	case websocket.IsCloseError(err, websocket.CloseGoingAway):
		return "client_going_away", nil
	case errors.Is(err, context.Canceled):
		return "request_context_done", nil
	case errors.Is(err, context.DeadlineExceeded):
		return "read_timeout", err
	default:
		return "read_error", err
	}
}

func publishPendingWakeFailures(service Service, agent protocol.Actor, session *attachmentSession, err error) {
	for _, event := range session.PendingWakes() {
		service.AgentWakeFailed(context.Background(), agent, event, err)
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
	if err := requireAttachmentFrameVersion(frame); err != nil {
		return protocol.Actor{}, err
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
	onWakeAck func(protocol.Event),
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
				if writeErr := writeAttachmentError(writer, message); writeErr != nil {
					return writeErr
				}
			}
			return err
		}

		if err := validateAttachmentFrameVersion(frame); err != nil {
			return writeAttachmentFrameVersionError(writer, err)
		}

		switch frame.Op {
		case protocol.AttachmentOpAck:
			event, wake, ok := session.Ack(frame.Cursor)
			if !ok {
				if err := writeAttachmentError(writer, "unexpected ACK cursor"); err != nil {
					return err
				}
				return fmt.Errorf("unexpected ACK cursor %q", frame.Cursor)
			}
			if wake && onWakeAck != nil {
				onWakeAck(event)
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
		case protocol.AttachmentOpError:
			return attachmentClientError(strings.TrimSpace(frame.Error))
		default:
			if err := writeAttachmentError(writer, fmt.Sprintf("unexpected frame op %q", frame.Op)); err != nil {
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

type attachmentClientError string

func (e attachmentClientError) Error() string {
	message := strings.TrimSpace(string(e))
	if message == "" {
		return "attachment client error"
	}
	return "attachment client error: " + message
}

func attachmentFrameErrorMessage(err error) (string, bool) {
	var frameErr *attachmentFrameError
	if !errors.As(err, &frameErr) || strings.TrimSpace(frameErr.message) == "" {
		return "", false
	}
	return frameErr.message, true
}
