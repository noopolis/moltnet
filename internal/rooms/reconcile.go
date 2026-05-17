package rooms

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) ReconcileRoomContext(ctx context.Context, request protocol.CreateRoomRequest) (protocol.Room, error) {
	room, err := roomFromCreateRequest(s.networkID, request)
	if err != nil {
		return protocol.Room{}, err
	}
	reconciled, err := s.reconcileRoom(ctx, room)
	if err != nil {
		if errors.Is(err, store.ErrRoomNotFound) {
			return protocol.Room{}, unknownRoomError(room.ID)
		}
		return protocol.Room{}, err
	}
	return reconciled, nil
}

func roomFromCreateRequest(networkID string, request protocol.CreateRoomRequest) (protocol.Room, error) {
	id := strings.TrimSpace(request.ID)
	roomRequest := protocol.CreateRoomRequest{
		ID:          id,
		Name:        strings.TrimSpace(request.Name),
		Members:     append([]string(nil), request.Members...),
		Visibility:  strings.TrimSpace(request.Visibility),
		WritePolicy: strings.TrimSpace(request.WritePolicy),
	}
	if err := protocol.ValidateCreateRoomRequest(roomRequest); err != nil {
		if id == "" {
			return protocol.Room{}, invalidRoomIDError()
		}
		return protocol.Room{}, invalidRoomRequestReasonError(err.Error())
	}

	room := protocol.Room{
		ID:          id,
		NetworkID:   networkID,
		FQID:        protocol.RoomFQID(networkID, id),
		Name:        strings.TrimSpace(request.Name),
		Members:     protocol.SortedUniqueTrimmedStrings(request.Members),
		Visibility:  protocol.NormalizeRoomVisibility(request.Visibility),
		WritePolicy: protocol.NormalizeRoomWritePolicy(request.WritePolicy),
		CreatedAt:   time.Now().UTC(),
	}
	if room.Name == "" {
		room.Name = room.ID
	}
	return room, nil
}
