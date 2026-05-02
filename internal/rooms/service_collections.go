package rooms

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) ListRooms() ([]protocol.Room, error) {
	page, err := s.ListRoomsContext(context.Background(), protocol.PageRequest{})
	if err != nil {
		return nil, err
	}
	return page.Rooms, nil
}

func (s *Service) CreateRoom(request protocol.CreateRoomRequest) (protocol.Room, error) {
	return s.CreateRoomContext(context.Background(), request)
}

func (s *Service) CreateRoomContext(ctx context.Context, request protocol.CreateRoomRequest) (protocol.Room, error) {
	id := strings.TrimSpace(request.ID)
	roomRequest := protocol.CreateRoomRequest{
		ID:      id,
		Name:    strings.TrimSpace(request.Name),
		Members: append([]string(nil), request.Members...),
	}
	if err := protocol.ValidateCreateRoomRequest(roomRequest); err != nil {
		if id == "" {
			return protocol.Room{}, invalidRoomIDError()
		}
		return protocol.Room{}, invalidRoomRequestReasonError(err.Error())
	}

	room := protocol.Room{
		ID:        id,
		NetworkID: s.networkID,
		FQID:      protocol.RoomFQID(s.networkID, id),
		Name:      strings.TrimSpace(request.Name),
		Members:   protocol.SortedUniqueTrimmedStrings(request.Members),
		CreatedAt: time.Now().UTC(),
	}
	if room.Name == "" {
		room.Name = room.ID
	}

	if err := s.createRoom(ctx, room); err != nil {
		if errors.Is(err, store.ErrRoomExists) {
			return protocol.Room{}, roomExistsError(room.ID)
		}
		return protocol.Room{}, err
	}

	s.publishEvent(protocol.Event{
		ID:        s.nextID("evt"),
		Type:      protocol.EventTypeRoomCreated,
		NetworkID: s.networkID,
		Room:      &room,
		CreatedAt: room.CreatedAt,
	})

	return room, nil
}

func (s *Service) ListRoomMessages(roomID string, before string, limit int) (protocol.MessagePage, error) {
	return s.ListRoomMessagesContext(context.Background(), roomID, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *Service) ListRoomMessagesContext(
	ctx context.Context,
	roomID string,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	if _, ok, err := s.getRoom(ctx, roomID); err != nil {
		return protocol.MessagePage{}, err
	} else if !ok {
		return protocol.MessagePage{}, unknownRoomError(roomID)
	}

	return s.listRoomMessages(ctx, roomID, page)
}

func (s *Service) ListThreads(roomID string) (protocol.ThreadPage, error) {
	return s.ListThreadsContext(context.Background(), roomID, protocol.PageRequest{})
}

func (s *Service) ListThreadsContext(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.ThreadPage, error) {
	if _, ok, err := s.getRoom(ctx, roomID); err != nil {
		return protocol.ThreadPage{}, err
	} else if !ok {
		return protocol.ThreadPage{}, unknownRoomError(roomID)
	}

	threads, err := s.listThreads(ctx, roomID)
	if err != nil {
		return protocol.ThreadPage{}, err
	}

	selected, info, err := paginateThreads(threads, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.ThreadPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.ThreadPage{}, err
	}

	return protocol.ThreadPage{
		Threads: selected,
		Page:    info,
	}, nil
}

func (s *Service) ListThreadMessages(threadID string, before string, limit int) (protocol.MessagePage, error) {
	return s.ListThreadMessagesContext(context.Background(), threadID, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *Service) ListThreadMessagesContext(
	ctx context.Context,
	threadID string,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	if _, ok, err := s.getThread(ctx, threadID); err != nil {
		return protocol.MessagePage{}, err
	} else if !ok {
		return protocol.MessagePage{}, unknownThreadError(threadID)
	}

	return s.listThreadMessages(ctx, threadID, page)
}

func (s *Service) ListDirectConversations() (protocol.DirectConversationPage, error) {
	return s.ListDirectConversationsContext(context.Background(), protocol.PageRequest{})
}

func (s *Service) ListDirectConversationsContext(ctx context.Context, page protocol.PageRequest) (protocol.DirectConversationPage, error) {
	conversations, err := s.listDirectConversations(ctx)
	if err != nil {
		return protocol.DirectConversationPage{}, err
	}

	selected, info, err := paginateDirectConversations(conversations, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.DirectConversationPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.DirectConversationPage{}, err
	}

	return protocol.DirectConversationPage{
		DMs:  selected,
		Page: info,
	}, nil
}

func (s *Service) ListDMMessages(dmID string, before string, limit int) (protocol.MessagePage, error) {
	return s.ListDMMessagesContext(context.Background(), dmID, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *Service) ListDMMessagesContext(
	ctx context.Context,
	dmID string,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	if strings.TrimSpace(dmID) == "" {
		return protocol.MessagePage{}, invalidDMIDError()
	}
	if _, ok, err := s.getDirectConversation(ctx, dmID); err != nil {
		return protocol.MessagePage{}, err
	} else if !ok {
		return protocol.MessagePage{}, unknownDirectConversationError(dmID)
	}

	return s.listDMMessages(ctx, dmID, page)
}

func (s *Service) ListArtifacts(
	filter protocol.ArtifactFilter,
	before string,
	limit int,
) (protocol.ArtifactPage, error) {
	return s.ListArtifactsContext(context.Background(), filter, protocol.PageRequest{
		Before: before,
		Limit:  limit,
	})
}

func (s *Service) ListArtifactsContext(
	ctx context.Context,
	filter protocol.ArtifactFilter,
	page protocol.PageRequest,
) (protocol.ArtifactPage, error) {
	if !filter.Scoped() {
		return protocol.ArtifactPage{}, artifactFilterRequiredError()
	}
	if filter.RoomID != "" {
		if _, ok, err := s.getRoom(ctx, filter.RoomID); err != nil {
			return protocol.ArtifactPage{}, err
		} else if !ok {
			return protocol.ArtifactPage{}, unknownRoomError(filter.RoomID)
		}
	}
	if filter.ThreadID != "" {
		if _, ok, err := s.getThread(ctx, filter.ThreadID); err != nil {
			return protocol.ArtifactPage{}, err
		} else if !ok {
			return protocol.ArtifactPage{}, unknownThreadError(filter.ThreadID)
		}
	}

	return s.listArtifacts(ctx, filter, page)
}

func (s *Service) ListAgents() ([]protocol.AgentSummary, error) {
	page, err := s.ListAgentsContext(context.Background(), protocol.PageRequest{})
	if err != nil {
		return nil, err
	}
	return page.Agents, nil
}

func (s *Service) ListAgentsContext(ctx context.Context, page protocol.PageRequest) (protocol.AgentPage, error) {
	if s.agentRegistry == nil && s.contextAgents != nil {
		agents, err := s.contextAgents.ListAgentsContext(ctx)
		if err != nil {
			return protocol.AgentPage{}, err
		}
		return paginateAgents(agents, page)
	}

	rooms, err := s.listRooms(ctx)
	if err != nil {
		return protocol.AgentPage{}, err
	}
	agentsByID := agentSummariesByID(rooms, s.networkID)
	if s.agentRegistry != nil {
		registered, err := s.agentRegistry.ListRegisteredAgentsContext(ctx)
		if err != nil {
			return protocol.AgentPage{}, err
		}
		for _, registration := range registered {
			summary, ok := agentsByID[registration.AgentID]
			if !ok {
				value := registeredAgentSummary(registration, rooms)
				agentsByID[registration.AgentID] = &value
				continue
			}
			summary.Name = registration.DisplayName
			summary.ActorUID = registration.ActorUID
			summary.FQID = registration.ActorURI
			summary.NetworkID = registration.NetworkID
		}
	}

	agents := make([]protocol.AgentSummary, 0, len(agentsByID))
	for _, agent := range agentsByID {
		slices.Sort(agent.Rooms)
		agents = append(agents, *agent)
	}
	slices.SortFunc(agents, func(left, right protocol.AgentSummary) int {
		return strings.Compare(left.ID, right.ID)
	})

	return paginateAgents(agents, page)
}

func agentSummariesByID(rooms []protocol.Room, networkID string) map[string]*protocol.AgentSummary {
	agentsByID := make(map[string]*protocol.AgentSummary)
	for _, room := range rooms {
		for _, memberID := range room.Members {
			agent, ok := agentsByID[memberID]
			if !ok {
				memberNetwork, memberAgent := memberIdentity(networkID, memberID)
				agent = &protocol.AgentSummary{
					ID:        memberID,
					FQID:      protocol.AgentFQID(memberNetwork, memberAgent),
					NetworkID: memberNetwork,
				}
				agentsByID[memberID] = agent
			}
			agent.Rooms = append(agent.Rooms, room.ID)
		}
	}
	return agentsByID
}

func registeredAgentSummary(registration protocol.AgentRegistration, rooms []protocol.Room) protocol.AgentSummary {
	summary := protocol.AgentSummary{
		ID:        registration.AgentID,
		Name:      registration.DisplayName,
		ActorUID:  registration.ActorUID,
		FQID:      registration.ActorURI,
		NetworkID: registration.NetworkID,
	}
	for _, room := range rooms {
		for _, memberID := range room.Members {
			memberNetwork, memberAgent := memberIdentity(registration.NetworkID, memberID)
			if memberNetwork == registration.NetworkID && memberAgent == registration.AgentID {
				summary.Rooms = append(summary.Rooms, room.ID)
				break
			}
		}
	}
	slices.Sort(summary.Rooms)
	return summary
}

func paginateAgents(agents []protocol.AgentSummary, page protocol.PageRequest) (protocol.AgentPage, error) {
	selected, info, err := paginateAgentValues(agents, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.AgentPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.AgentPage{}, err
	}

	return protocol.AgentPage{
		Agents: selected,
		Page:   info,
	}, nil
}
