package rooms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) SendMessage(request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	return s.SendMessageContext(context.Background(), request)
}

func (s *Service) SendMessageContext(ctx context.Context, request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	if strings.TrimSpace(request.Target.Kind) == protocol.TargetKindDM && s.disableDirectMessages {
		return protocol.MessageAccepted{}, directMessagesDisabledError()
	}
	if request.From.Type == "human" && !s.allowHumanIngress {
		return protocol.MessageAccepted{}, humanIngressDisabledError()
	}
	if err := s.validateSenderIdentity(ctx, request.From, request.Origin); err != nil {
		return protocol.MessageAccepted{}, err
	}

	messageID := strings.TrimSpace(request.ID)
	if messageID == "" {
		messageID = s.nextID("msg")
	}

	if err := validateSendMessageRequest(request); err != nil {
		return protocol.MessageAccepted{}, err
	}

	if request.Target.Kind == protocol.TargetKindRoom {
		if _, ok, err := s.getRoom(ctx, request.Target.RoomID); err != nil {
			return protocol.MessageAccepted{}, err
		} else if !ok {
			return protocol.MessageAccepted{}, unknownRoomError(request.Target.RoomID)
		}
	}
	if request.Target.Kind == protocol.TargetKindThread {
		if _, ok, err := s.getRoom(ctx, request.Target.RoomID); err != nil {
			return protocol.MessageAccepted{}, err
		} else if !ok {
			return protocol.MessageAccepted{}, unknownRoomError(request.Target.RoomID)
		}
	}

	mentions, err := s.resolveMentions(ctx, request.Target, protocol.NormalizeMentions(request.Parts, request.Mentions))
	if err != nil {
		return protocol.MessageAccepted{}, err
	}

	now := time.Now().UTC()
	from := protocol.NormalizeActor(s.networkID, request.From)
	target := s.normalizeTarget(request.Target, from)
	origin := s.normalizeOrigin(request.Origin, messageID)
	message := protocol.Message{
		ID:        messageID,
		NetworkID: s.networkID,
		Origin:    origin,
		Target:    target,
		From:      from,
		Parts:     append([]protocol.Part(nil), request.Parts...),
		Mentions:  mentions,
		CreatedAt: now,
	}

	event := protocol.Event{
		ID:        eventIDForMessage(message.ID),
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: s.networkID,
		Message:   &message,
		CreatedAt: now,
	}

	lifecycle := store.AppendLifecycle{}
	if s.lifecycleMessages != nil {
		lifecycle, err = s.lifecycleMessages.AppendMessageWithLifecycleContext(ctx, message)
		if err != nil {
			if errors.Is(err, store.ErrDuplicateMessage) {
				return protocol.MessageAccepted{
					MessageID: message.ID,
					EventID:   event.ID,
					Accepted:  true,
				}, nil
			}
			return protocol.MessageAccepted{}, err
		}
	} else if err := s.appendMessage(ctx, message); err != nil {
		if errors.Is(err, store.ErrDuplicateMessage) {
			return protocol.MessageAccepted{
				MessageID: message.ID,
				EventID:   event.ID,
				Accepted:  true,
			}, nil
		}
		return protocol.MessageAccepted{}, err
	} else {
		lifecycle, err = s.conversationLifecycle(ctx, message)
		if err != nil {
			return protocol.MessageAccepted{}, err
		}
	}

	if lifecycle.Thread != nil {
		s.publishEvent(protocol.Event{
			ID:        s.nextID("evt"),
			Type:      protocol.EventTypeThreadCreated,
			NetworkID: s.networkID,
			Thread:    lifecycle.Thread,
			CreatedAt: now,
		})
	}
	if lifecycle.DM != nil {
		s.publishEvent(protocol.Event{
			ID:        s.nextID("evt"),
			Type:      protocol.EventTypeDMCreated,
			NetworkID: s.networkID,
			DM:        lifecycle.DM,
			CreatedAt: now,
		})
	}
	s.publishEvent(event)
	s.relayMessage(message)

	return protocol.MessageAccepted{
		MessageID:     message.ID,
		EventID:       event.ID,
		Accepted:      true,
		ThreadCreated: lifecycle.Thread != nil,
		DMCreated:     lifecycle.DM != nil,
	}, nil
}

func (s *Service) validateSenderIdentity(ctx context.Context, actor protocol.Actor, origin protocol.MessageOrigin) error {
	if s.remoteOriginRequiresPairCheck(ctx, actor, origin) {
		return agentConflictError("remote origin actor")
	}
	if strings.TrimSpace(actor.Type) == "human" {
		return s.validateHumanSender(ctx, origin)
	}
	if s.isPairedRemoteOriginActor(ctx, actor, origin) {
		return nil
	}
	agentID, local := s.agentCollisionID(actor)
	if agentID == "" {
		return nil
	}
	mode := authn.ModeFromContext(ctx)
	claims, hasClaims := authn.ClaimsFromContext(ctx)
	if mode == authn.ModeOpen && !local {
		return agentForbiddenError(fmt.Sprintf("remote-origin agent %q requires pair scope", agentID))
	}
	if !local && hasClaims {
		return agentConflictError(agentID)
	}
	if s.agentRegistry == nil {
		return nil
	}
	if hasClaims {
		if claims.Allows(authn.ScopePair) && !claims.Allows(authn.ScopeWrite) {
			return agentForbiddenError("pair tokens cannot assert local agents")
		}
		if !claims.AllowsAgent(agentID) {
			if claims.AgentToken() {
				return agentTokenInvalidForAgentError(agentID)
			}
			return agentForbiddenError(fmt.Sprintf("agent %q is not allowed for this token", agentID))
		}
	}

	registration, ok, err := s.registeredAgent(ctx, agentID)
	if err != nil {
		return err
	}
	if !ok {
		if mode == authn.ModeOpen {
			return agentRegistrationRequiredError(agentID)
		}
		return nil
	}
	credentialKey := registrationCredentialKey(ctx)
	if strings.TrimSpace(credentialKey) == "anonymous" {
		if mode == authn.ModeOpen {
			return agentRequiresTokenError(agentID)
		}
		if registration.CredentialKey == credentialKey {
			return nil
		}
	}
	if registration.CredentialKey == credentialKey {
		if mode == authn.ModeOpen && (!hasClaims || !claims.Allows(authn.ScopeWrite)) {
			return agentForbiddenError(fmt.Sprintf("agent %q requires write scope", agentID))
		}
		return nil
	}
	if mode == authn.ModeOpen && hasClaims && claims.Allows(authn.ScopeAdmin) && claims.Allows(authn.ScopeWrite) {
		return nil
	}
	if hasClaims && claims.AgentToken() {
		return agentTokenInvalidForAgentError(agentID)
	}
	if mode == authn.ModeOpen && !hasClaims {
		return agentRequiresTokenError(agentID)
	}
	if mode == authn.ModeOpen && hasClaims && !claims.Allows(authn.ScopeWrite) {
		return agentForbiddenError(fmt.Sprintf("agent %q requires write scope", agentID))
	}
	if mode == authn.ModeOpen {
		return agentConflictError(agentID)
	}
	if registration.CredentialKey != credentialKey {
		return agentConflictError(agentID)
	}
	return nil
}

func (s *Service) remoteOriginRequiresPairCheck(
	ctx context.Context,
	actor protocol.Actor,
	origin protocol.MessageOrigin,
) bool {
	originNetworkID := strings.TrimSpace(origin.NetworkID)
	if originNetworkID == "" || originNetworkID == s.networkID {
		return false
	}
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) {
		return false
	}
	return !actorHasExplicitConsistentNetworkID(actor, originNetworkID)
}

func (s *Service) validateHumanSender(ctx context.Context, origin protocol.MessageOrigin) error {
	if originNetworkID := strings.TrimSpace(origin.NetworkID); originNetworkID != "" && originNetworkID != s.networkID {
		claims, ok := authn.ClaimsFromContext(ctx)
		if !ok || !claims.Allows(authn.ScopePair) {
			return agentForbiddenError("remote-origin human sender requires pair scope")
		}
	}
	if authn.ModeFromContext(ctx) != authn.ModeOpen {
		return nil
	}
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok {
		return &Error{
			status: http.StatusUnauthorized,
			msg:    "human ingress requires a write token",
			cause:  ErrAgentUnauthorized,
		}
	}
	if claims.AgentToken() || !claims.Allows(authn.ScopeWrite) {
		return agentForbiddenError("human ingress requires a static write token")
	}
	return nil
}

func (s *Service) isPairedRemoteOriginActor(ctx context.Context, actor protocol.Actor, origin protocol.MessageOrigin) bool {
	originNetworkID := strings.TrimSpace(origin.NetworkID)
	if originNetworkID == "" || originNetworkID == s.networkID {
		return false
	}
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok || !claims.Allows(authn.ScopePair) {
		return false
	}
	return actorHasExplicitConsistentNetworkID(actor, originNetworkID)
}

func (s *Service) agentCollisionID(actor protocol.Actor) (string, bool) {
	if networkID, agentID, ok := protocol.ParseScopedAgentID(actor.ID); ok {
		return strings.TrimSpace(agentID), strings.TrimSpace(networkID) == s.networkID
	}
	if networkID, agentID, ok := protocol.ParseAgentFQID(actor.ID); ok {
		return strings.TrimSpace(agentID), strings.TrimSpace(networkID) == s.networkID
	}
	if networkID, _, ok := protocol.ParseAgentFQID(actor.FQID); ok && strings.TrimSpace(networkID) != s.networkID {
		return strings.TrimSpace(actor.ID), false
	}
	if networkID := strings.TrimSpace(actor.NetworkID); networkID != "" && networkID != s.networkID {
		return strings.TrimSpace(actor.ID), false
	}
	return strings.TrimSpace(actor.ID), true
}

func actorHasExplicitConsistentNetworkID(actor protocol.Actor, networkID string) bool {
	if strings.TrimSpace(actor.NetworkID) != networkID {
		return false
	}
	if parsedNetworkID, _, ok := protocol.ParseAgentFQID(actor.FQID); ok &&
		strings.TrimSpace(parsedNetworkID) != networkID {
		return false
	}
	if parsedNetworkID, _, ok := protocol.ParseScopedAgentID(actor.ID); ok &&
		strings.TrimSpace(parsedNetworkID) != networkID {
		return false
	}
	if parsedNetworkID, _, ok := protocol.ParseAgentFQID(actor.ID); ok &&
		strings.TrimSpace(parsedNetworkID) != networkID {
		return false
	}
	return true
}

func (s *Service) Subscribe(ctx context.Context) <-chan protocol.Event {
	return s.filterEvents(ctx, s.broker.Subscribe(ctx))
}

func (s *Service) nextID(prefix string) string {
	id := s.counter.Add(1)
	return fmt.Sprintf("%s_%s_%d", prefix, sanitizeIDComponent(s.networkID), id)
}
