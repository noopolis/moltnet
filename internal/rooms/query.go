package rooms

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type replayingBroker interface {
	SubscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event
}

func (s *Service) SubscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event {
	if broker, ok := s.broker.(replayingBroker); ok {
		return broker.SubscribeFrom(ctx, lastEventID)
	}

	return s.broker.Subscribe(ctx)
}

func (s *Service) Health(ctx context.Context) error {
	checker, ok := s.store.(store.HealthChecker)
	if !ok {
		return nil
	}

	return checker.Health(ctx)
}

func (s *Service) GetRoom(roomID string) (protocol.Room, error) {
	room, ok, err := s.getRoom(context.Background(), strings.TrimSpace(roomID))
	if err != nil {
		return protocol.Room{}, err
	}
	if !ok {
		return protocol.Room{}, unknownRoomError(roomID)
	}

	return room, nil
}

func (s *Service) GetThread(threadID string) (protocol.Thread, error) {
	thread, ok, err := s.getThread(context.Background(), strings.TrimSpace(threadID))
	if err != nil {
		return protocol.Thread{}, err
	}
	if !ok {
		return protocol.Thread{}, unknownThreadError(threadID)
	}

	return thread, nil
}

func (s *Service) GetDirectConversation(dmID string) (protocol.DirectConversation, error) {
	id := strings.TrimSpace(dmID)
	if id == "" {
		return protocol.DirectConversation{}, invalidDMIDError()
	}

	conversation, ok, err := s.getDirectConversation(context.Background(), id)
	if err != nil {
		return protocol.DirectConversation{}, err
	}
	if ok {
		return conversation, nil
	}

	return protocol.DirectConversation{}, unknownDirectConversationError(id)
}

func (s *Service) GetAgent(agentID string) (protocol.AgentSummary, error) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return protocol.AgentSummary{}, unknownAgentError(id)
	}
	if registration, ok, err := s.registeredAgent(context.Background(), id); err != nil {
		return protocol.AgentSummary{}, err
	} else if ok {
		rooms, err := s.listRooms(context.Background())
		if err != nil {
			return protocol.AgentSummary{}, err
		}
		return registeredAgentSummary(registration, rooms), nil
	}
	if s.contextAgents != nil {
		agent, ok, err := s.contextAgents.GetAgentContext(context.Background(), id)
		if err != nil {
			return protocol.AgentSummary{}, err
		}
		if ok {
			return agent, nil
		}
		return protocol.AgentSummary{}, unknownAgentError(id)
	}
	rooms, err := s.listRooms(context.Background())
	if err != nil {
		return protocol.AgentSummary{}, err
	}
	agent := protocol.AgentSummary{
		ID:        id,
		FQID:      protocol.AgentFQID(s.networkID, id),
		NetworkID: s.networkID,
	}
	found := false
	for _, room := range rooms {
		for _, memberID := range room.Members {
			if memberID == id {
				agent.Rooms = append(agent.Rooms, room.ID)
				found = true
			}
		}
	}
	if !found {
		return protocol.AgentSummary{}, unknownAgentError(id)
	}
	slices.Sort(agent.Rooms)
	return agent, nil
}

func (s *Service) getRoom(ctx context.Context, roomID string) (protocol.Room, bool, error) {
	if s.contextStore != nil {
		return s.contextStore.GetRoomContext(ctx, strings.TrimSpace(roomID))
	}
	return s.store.GetRoom(strings.TrimSpace(roomID))
}

func (s *Service) getThread(ctx context.Context, threadID string) (protocol.Thread, bool, error) {
	if s.contextStore != nil {
		return s.contextStore.GetThreadContext(ctx, strings.TrimSpace(threadID))
	}
	return s.store.GetThread(strings.TrimSpace(threadID))
}

func (s *Service) getDirectConversation(ctx context.Context, dmID string) (protocol.DirectConversation, bool, error) {
	if s.contextMessages != nil {
		return s.contextMessages.GetDirectConversationContext(ctx, strings.TrimSpace(dmID))
	}
	conversations, err := s.messages.ListDirectConversations()
	if err != nil {
		return protocol.DirectConversation{}, false, err
	}
	for _, conversation := range conversations {
		if conversation.ID == strings.TrimSpace(dmID) {
			return conversation, true, nil
		}
	}
	return protocol.DirectConversation{}, false, nil
}

func (s *Service) listRooms(ctx context.Context) ([]protocol.Room, error) {
	if s.contextStore != nil {
		return s.contextStore.ListRoomsContext(ctx)
	}
	return s.store.ListRooms()
}

func (s *Service) createRoom(ctx context.Context, room protocol.Room) error {
	if s.contextStore != nil {
		return s.contextStore.CreateRoomContext(ctx, room)
	}
	return s.store.CreateRoom(room)
}

func (s *Service) updateRoomMembers(ctx context.Context, roomID string, add []string, remove []string) (protocol.Room, error) {
	if s.contextStore != nil {
		return s.contextStore.UpdateRoomMembersContext(ctx, roomID, add, remove)
	}
	return s.store.UpdateRoomMembers(roomID, add, remove)
}

func (s *Service) appendMessage(ctx context.Context, message protocol.Message) error {
	if s.contextMessages != nil {
		return s.contextMessages.AppendMessageContext(ctx, message)
	}
	return s.messages.AppendMessage(message)
}

func (s *Service) listRoomMessages(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	if s.contextMessages != nil {
		messages, err := s.contextMessages.ListRoomMessagesContext(ctx, roomID, page)
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.MessagePage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return messages, err
	}
	return s.messages.ListRoomMessages(roomID, page.Before, page.Limit)
}

func (s *Service) listThreads(ctx context.Context, roomID string) ([]protocol.Thread, error) {
	if s.contextMessages != nil {
		return s.contextMessages.ListThreadsContext(ctx, roomID)
	}
	return s.messages.ListThreads(roomID)
}

func (s *Service) listThreadMessages(ctx context.Context, threadID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	if s.contextMessages != nil {
		messages, err := s.contextMessages.ListThreadMessagesContext(ctx, threadID, page)
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.MessagePage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return messages, err
	}
	return s.messages.ListThreadMessages(threadID, page.Before, page.Limit)
}

func (s *Service) listDirectConversations(ctx context.Context) ([]protocol.DirectConversation, error) {
	if s.contextMessages != nil {
		return s.contextMessages.ListDirectConversationsContext(ctx)
	}
	return s.messages.ListDirectConversations()
}

func (s *Service) listDMMessages(ctx context.Context, dmID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	if s.contextMessages != nil {
		messages, err := s.contextMessages.ListDMMessagesContext(ctx, dmID, page)
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.MessagePage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return messages, err
	}
	return s.messages.ListDMMessages(dmID, page.Before, page.Limit)
}

func (s *Service) listArtifacts(ctx context.Context, filter protocol.ArtifactFilter, page protocol.PageRequest) (protocol.ArtifactPage, error) {
	if s.contextMessages != nil {
		artifacts, err := s.contextMessages.ListArtifactsContext(ctx, filter, page)
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.ArtifactPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return artifacts, err
	}
	return s.messages.ListArtifacts(filter, page.Before, page.Limit)
}
