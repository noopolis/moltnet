package rooms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) SendMessage(request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	return s.SendMessageContext(context.Background(), request)
}

func (s *Service) SendMessageContext(ctx context.Context, request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	if request.From.Type == "human" && !s.allowHumanIngress {
		return protocol.MessageAccepted{}, humanIngressDisabledError()
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
		Mentions:  protocol.NormalizeMentions(request.Parts, request.Mentions),
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
	var err error
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

func (s *Service) Subscribe(ctx context.Context) <-chan protocol.Event {
	return s.broker.Subscribe(ctx)
}

func (s *Service) nextID(prefix string) string {
	id := s.counter.Add(1)
	return fmt.Sprintf("%s_%s_%d", prefix, sanitizeIDComponent(s.networkID), id)
}
