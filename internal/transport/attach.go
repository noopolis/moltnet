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
		message := err.Error()
		if policy != nil && policy.Open() {
			if _, ok := authn.ClaimsFromContext(request.Context()); !ok && strings.Contains(message, "already registered") {
				message = fmt.Sprintf("agent %q requires its agent token", agent.ID)
			}
		}
		_ = writer.write(protocol.AttachmentFrame{
			Op:    protocol.AttachmentOpError,
			Error: message,
		})
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
		_ = writer.write(protocol.AttachmentFrame{
			Op:    protocol.AttachmentOpError,
			Error: err.Error(),
		})
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

	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()

	readErrCh := make(chan error, 1)
	go func() {
		readErrCh <- consumeAttachmentFrames(ctx, connection, writer, session, attachmentReadTimeout())
	}()

	heartbeatTicker := time.NewTicker(attachmentHeartbeatInterval() / 2)
	defer heartbeatTicker.Stop()

	filter := attachmentEventFilter(policy, request.Context(), service.Network().ID, agent.ID)
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
			if filter != nil && !filter(event) {
				continue
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

func attachmentEventFilter(
	policy *authn.Policy,
	ctx context.Context,
	networkID string,
	agentID string,
) eventFilter {
	if policy == nil || !policy.Open() {
		return nil
	}
	if claims, ok := authn.ClaimsFromContext(ctx); ok &&
		(claims.Allows(authn.ScopeObserve) || claims.Allows(authn.ScopeAdmin)) {
		return nil
	}

	return func(event protocol.Event) bool {
		return publicOpenEvent(event) || attachedAgentEvent(event, networkID, agentID)
	}
}

func attachedAgentEvent(event protocol.Event, networkID string, agentID string) bool {
	switch event.Type {
	case protocol.EventTypeMessageCreated:
		return event.Message != nil && attachedAgentMessage(event.Message, networkID, agentID)
	case protocol.EventTypeDMCreated:
		return event.DM != nil && participantsIncludeAttachedAgent(event.DM.ParticipantIDs, networkID, agentID)
	default:
		return false
	}
}

func attachedAgentMessage(message *protocol.Message, networkID string, agentID string) bool {
	if message.Target.Kind != protocol.TargetKindDM {
		return false
	}
	if participantsIncludeAttachedAgent(message.Target.ParticipantIDs, networkID, agentID) {
		return true
	}
	return false
}

func participantsIncludeAttachedAgent(participants []string, networkID string, agentID string) bool {
	for _, participantID := range participants {
		if protocol.ActorMatches(networkID, agentID, participantID) {
			return true
		}
	}
	return false
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
