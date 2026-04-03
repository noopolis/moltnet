package rooms

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type roomItem struct{ protocol.Room }
type agentItem struct{ protocol.AgentSummary }
type threadItem struct{ protocol.Thread }
type directConversationItem struct{ protocol.DirectConversation }
type pairingItem struct{ protocol.Pairing }

func (r roomItem) GetID() string               { return r.Room.ID }
func (a agentItem) GetID() string              { return a.AgentSummary.ID }
func (t threadItem) GetID() string             { return t.Thread.ID }
func (d directConversationItem) GetID() string { return d.DirectConversation.ID }
func (p pairingItem) GetID() string            { return p.Pairing.ID }

func (s *Service) ListRoomsContext(ctx context.Context, page protocol.PageRequest) (protocol.RoomPage, error) {
	rooms, err := s.listRooms(ctx)
	if err != nil {
		return protocol.RoomPage{}, err
	}

	items := make([]roomItem, 0, len(rooms))
	for _, room := range rooms {
		items = append(items, roomItem{Room: room})
	}
	selected, info, err := paginate(items, page)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return protocol.RoomPage{}, invalidCursorReasonError(cursorForPage(page))
		}
		return protocol.RoomPage{}, err
	}
	values := make([]protocol.Room, 0, len(selected))
	for _, item := range selected {
		values = append(values, item.Room)
	}

	return protocol.RoomPage{
		Rooms: values,
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

type pageable interface {
	GetID() string
}

func paginate[T pageable](values []T, page protocol.PageRequest) ([]T, protocol.PageInfo, error) {
	if err := protocol.ValidatePageRequest(page); err != nil {
		return nil, protocol.PageInfo{}, store.ErrInvalidCursor
	}

	limit := page.Limit
	if limit <= 0 {
		limit = len(values)
	}

	start := 0
	end := len(values)
	if page.After != "" {
		value, ok := indexAfter(values, page.After)
		if !ok {
			return nil, protocol.PageInfo{}, store.ErrInvalidCursor
		}
		start = value
	}
	if page.Before != "" {
		value, ok := indexBefore(values, page.Before)
		if !ok {
			return nil, protocol.PageInfo{}, store.ErrInvalidCursor
		}
		end = value
	}
	if end < start {
		end = start
	}

	windowStart := start
	windowEnd := end
	if windowEnd-windowStart > limit {
		if page.Before != "" && page.After == "" {
			windowStart = windowEnd - limit
		} else {
			windowEnd = windowStart + limit
		}
	}

	selected := append([]T(nil), values[windowStart:windowEnd]...)
	info := protocol.PageInfo{
		HasMore: windowStart > 0 || windowEnd < len(values),
	}
	if windowStart > 0 && len(selected) > 0 {
		info.NextBefore = selected[0].GetID()
	}
	if windowEnd < len(values) && len(selected) > 0 {
		info.NextAfter = selected[len(selected)-1].GetID()
	}

	return selected, info, nil
}

func indexAfter[T pageable](values []T, after string) (int, bool) {
	for index, value := range values {
		if value.GetID() == after {
			return index + 1, true
		}
	}
	return 0, false
}

func indexBefore[T pageable](values []T, before string) (int, bool) {
	for index, value := range values {
		if value.GetID() == before {
			return index, true
		}
	}
	return len(values), false
}

func cursorForPage(page protocol.PageRequest) string {
	if page.After != "" {
		return page.After
	}
	return page.Before
}
