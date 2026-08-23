package rooms

import (
	"context"
	"errors"
	"strings"
	"time"

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
		messageID = newPrefixedID("msg")
	}

	if err := validateSendMessageRequest(request); err != nil {
		return protocol.MessageAccepted{}, err
	}

	from := protocol.NormalizeActor(s.networkID, request.From)
	// CredentialBound is server authority, never caller testimony. The request
	// uses the same Actor wire type as stored messages, so overwrite it after
	// normalization even when a caller supplied true.
	from.CredentialBound = s.senderCredentialBound(ctx, from)
	if err := s.validateDMSenderTopology(request.Target, from); err != nil {
		return protocol.MessageAccepted{}, err
	}
	if request.Target.Kind == protocol.TargetKindRoom || request.Target.Kind == protocol.TargetKindThread {
		if err := s.enforceTargetWritePolicy(ctx, request.Target, from); err != nil {
			// A canWriteRoom denial (non-member / write-policy rejection)
			// must always be ledger-visible, never a silent drop: stamp
			// message.denied before returning the existing error. Other
			// enforceTargetWritePolicy failures (e.g. unknown room) are not
			// write-policy denials and are left unstamped.
			if errors.Is(err, ErrWriteForbidden) {
				s.stampMessageDenied(ctx, messageID, request, err.Error())
			}
			return protocol.MessageAccepted{}, err
		}
	}

	mentions, err := s.resolveMentions(ctx, request.Target, protocol.NormalizeMentions(request.Parts, request.Mentions))
	if err != nil {
		return protocol.MessageAccepted{}, err
	}

	now := time.Now().UTC()
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
			if errors.Is(err, store.ErrDMTopologyConflict) {
				return protocol.MessageAccepted{}, dmTopologyConflictError()
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
		if errors.Is(err, store.ErrDMTopologyConflict) {
			return protocol.MessageAccepted{}, dmTopologyConflictError()
		}
		return protocol.MessageAccepted{}, err
	} else {
		lifecycle, err = s.conversationLifecycle(ctx, message)
		if err != nil {
			return protocol.MessageAccepted{}, err
		}
	}

	// The durable append above has succeeded and this is not a duplicate
	// (both ErrDuplicateMessage branches return before reaching here), so
	// this is exactly one message.accepted causal stamp per accepted message.
	s.stampMessageAccepted(ctx, message, request)

	if lifecycle.Thread != nil {
		s.publishEvent(protocol.Event{
			ID:        newPrefixedID("evt"),
			Type:      protocol.EventTypeThreadCreated,
			NetworkID: s.networkID,
			Thread:    lifecycle.Thread,
			CreatedAt: now,
		})
	}
	if lifecycle.DM != nil {
		s.publishEvent(protocol.Event{
			ID:        newPrefixedID("evt"),
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

func (s *Service) validateDMSenderTopology(target protocol.Target, actor protocol.Actor) error {
	if target.Kind != protocol.TargetKindDM {
		return nil
	}

	participantIDs := protocol.UniqueTrimmedStrings(target.ParticipantIDs)
	canonicalIDs := make(map[string]struct{}, len(participantIDs))
	senderIncluded := false
	for _, participantID := range participantIDs {
		networkID, agentID := normalizedAgentIdentity(s.networkID, participantID)
		canonicalID := protocol.ScopedAgentID(networkID, agentID)
		if _, duplicate := canonicalIDs[canonicalID]; duplicate {
			return dmTopologyConflictError()
		}
		canonicalIDs[canonicalID] = struct{}{}
		if AgentIdentityMatches(actor.NetworkID, actor.ID, s.networkID, participantID) {
			senderIncluded = true
		}
	}
	if len(canonicalIDs) < 2 || !senderIncluded {
		return dmTopologyConflictError()
	}
	return nil
}

func (s *Service) Subscribe(ctx context.Context) <-chan protocol.Event {
	return s.filterEvents(ctx, s.broker.Subscribe(ctx))
}
