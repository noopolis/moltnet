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

	items := make([]threadItem, 0, len(threads))
	for _, thread := range threads {
		items = append(items, threadItem{Thread: thread})
	}
	selected, info, err := paginate(items, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.ThreadPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.ThreadPage{}, err
	}
	values := make([]protocol.Thread, 0, len(selected))
	for _, item := range selected {
		values = append(values, item.Thread)
	}

	return protocol.ThreadPage{
		Threads: values,
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

	items := make([]directConversationItem, 0, len(conversations))
	for _, conversation := range conversations {
		items = append(items, directConversationItem{DirectConversation: conversation})
	}
	selected, info, err := paginate(items, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.DirectConversationPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.DirectConversationPage{}, err
	}
	values := make([]protocol.DirectConversation, 0, len(selected))
	for _, item := range selected {
		values = append(values, item.DirectConversation)
	}

	return protocol.DirectConversationPage{
		DMs:  values,
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
	if s.contextAgents != nil {
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
	agentsByID := make(map[string]*protocol.AgentSummary)
	for _, room := range rooms {
		for _, memberID := range room.Members {
			agent, ok := agentsByID[memberID]
			if !ok {
				agent = &protocol.AgentSummary{
					ID:        memberID,
					FQID:      protocol.AgentFQID(s.networkID, memberID),
					NetworkID: s.networkID,
				}
				agentsByID[memberID] = agent
			}
			agent.Rooms = append(agent.Rooms, room.ID)
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

func paginateAgents(agents []protocol.AgentSummary, page protocol.PageRequest) (protocol.AgentPage, error) {
	items := make([]agentItem, 0, len(agents))
	for _, agent := range agents {
		items = append(items, agentItem{AgentSummary: agent})
	}
	selected, info, err := paginate(items, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.AgentPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.AgentPage{}, err
	}
	values := make([]protocol.AgentSummary, 0, len(selected))
	for _, item := range selected {
		values = append(values, item.AgentSummary)
	}

	return protocol.AgentPage{
		Agents: values,
		Page:   info,
	}, nil
}
