package rooms

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) ListRoomsContext(ctx context.Context, page protocol.PageRequest) (protocol.RoomPage, error) {
	rooms, err := s.listRooms(ctx)
	if err != nil {
		return protocol.RoomPage{}, err
	}

	selected, info, err := paginateRooms(rooms, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.RoomPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.RoomPage{}, err
	}

	return protocol.RoomPage{
		Rooms: selected,
		Page:  info,
	}, nil
}

func (s *Service) UpdateRoomMembers(
	ctx context.Context,
	roomID string,
	request protocol.UpdateRoomMembersRequest,
) (protocol.Room, error) {
	if err := validateUpdateRoomMembersRequest(request); err != nil {
		return protocol.Room{}, err
	}

	id := strings.TrimSpace(roomID)
	current, ok, err := s.getRoom(ctx, id)
	if err != nil {
		return protocol.Room{}, err
	}
	if !ok {
		return protocol.Room{}, unknownRoomError(id)
	}

	room, err := s.updateRoomMembers(ctx, id, request.Add, request.Remove)
	if err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			return protocol.Room{}, unknownRoomError(roomID)
		}
		return protocol.Room{}, err
	}
	if membersEqual(current.Members, room.Members) {
		return room, nil
	}

	s.publishEvent(protocol.Event{
		ID:        s.nextID("evt"),
		Type:      protocol.EventTypeRoomMembersUpdated,
		NetworkID: s.networkID,
		Room:      &room,
		CreatedAt: room.CreatedAt,
	})

	return room, nil
}

func membersEqual(left []string, right []string) bool {
	return slices.Equal(protocol.SortedUniqueTrimmedStrings(left), protocol.SortedUniqueTrimmedStrings(right))
}

func cursorForPage(page protocol.PageRequest) string {
	if page.After != "" {
		return page.After
	}
	return page.Before
}

func paginateRooms(rooms []protocol.Room, page protocol.PageRequest) ([]protocol.Room, protocol.PageInfo, error) {
	return paginateByID(rooms, page, func(room protocol.Room) string { return room.ID })
}

func paginateAgentValues(agents []protocol.AgentSummary, page protocol.PageRequest) ([]protocol.AgentSummary, protocol.PageInfo, error) {
	return paginateByID(agents, page, func(agent protocol.AgentSummary) string { return agent.ID })
}

func paginateThreads(threads []protocol.Thread, page protocol.PageRequest) ([]protocol.Thread, protocol.PageInfo, error) {
	return paginateByID(threads, page, func(thread protocol.Thread) string { return thread.ID })
}

func paginateDirectConversations(
	conversations []protocol.DirectConversation,
	page protocol.PageRequest,
) ([]protocol.DirectConversation, protocol.PageInfo, error) {
	return paginateByID(conversations, page, func(conversation protocol.DirectConversation) string { return conversation.ID })
}

func paginatePairings(pairings []protocol.Pairing, page protocol.PageRequest) ([]protocol.Pairing, protocol.PageInfo, error) {
	return paginateByID(pairings, page, func(pairing protocol.Pairing) string { return pairing.ID })
}

func paginateByID[T any](values []T, page protocol.PageRequest, id func(T) string) ([]T, protocol.PageInfo, error) {
	selected, info, err := protocol.PaginateByID(values, page, id)
	if err != nil {
		return nil, protocol.PageInfo{}, store.ErrInvalidCursor
	}
	return selected, info, nil
}
