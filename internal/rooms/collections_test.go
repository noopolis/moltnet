package rooms

import (
	"errors"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestPaginateSupportsBeforeAndAfter(t *testing.T) {
	t.Parallel()

	rooms := []protocol.Room{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	}

	beforeValues, beforeInfo, err := paginateRooms(rooms, protocol.PageRequest{
		Before: "c",
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("paginateRooms() before error = %v", err)
	}
	if len(beforeValues) != 1 || beforeValues[0].ID != "b" {
		t.Fatalf("unexpected before values %#v", beforeValues)
	}
	if !beforeInfo.HasMore || beforeInfo.NextBefore != "b" || beforeInfo.NextAfter != "b" {
		t.Fatalf("unexpected before page info %#v", beforeInfo)
	}

	afterValues, afterInfo, err := paginateRooms(rooms, protocol.PageRequest{
		After: "a",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("paginateRooms() after error = %v", err)
	}
	if len(afterValues) != 1 || afterValues[0].ID != "b" {
		t.Fatalf("unexpected after values %#v", afterValues)
	}
	if !afterInfo.HasMore || afterInfo.NextBefore != "b" || afterInfo.NextAfter != "b" {
		t.Fatalf("unexpected after page info %#v", afterInfo)
	}
}

func TestServiceCreateRoomDuplicateReturnsDomainError(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); !errors.Is(err, ErrRoomExists) {
		t.Fatalf("expected ErrRoomExists, got %v", err)
	}
}
