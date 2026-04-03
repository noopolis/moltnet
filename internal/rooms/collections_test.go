package rooms

import (
	"errors"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestPaginateSupportsBeforeAndAfter(t *testing.T) {
	t.Parallel()

	items := []roomItem{
		{Room: protocol.Room{ID: "a"}},
		{Room: protocol.Room{ID: "b"}},
		{Room: protocol.Room{ID: "c"}},
	}

	beforeValues, beforeInfo, err := paginate(items, protocol.PageRequest{
		Before: "c",
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("paginate() before error = %v", err)
	}
	if len(beforeValues) != 1 || beforeValues[0].GetID() != "b" {
		t.Fatalf("unexpected before values %#v", beforeValues)
	}
	if !beforeInfo.HasMore || beforeInfo.NextBefore != "b" || beforeInfo.NextAfter != "b" {
		t.Fatalf("unexpected before page info %#v", beforeInfo)
	}

	afterValues, afterInfo, err := paginate(items, protocol.PageRequest{
		After: "a",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("paginate() after error = %v", err)
	}
	if len(afterValues) != 1 || afterValues[0].GetID() != "b" {
		t.Fatalf("unexpected after values %#v", afterValues)
	}
	if !afterInfo.HasMore || afterInfo.NextBefore != "b" || afterInfo.NextAfter != "b" {
		t.Fatalf("unexpected after page info %#v", afterInfo)
	}
}

func TestCollectionGetIDHelpers(t *testing.T) {
	t.Parallel()

	if got := (roomItem{Room: protocol.Room{ID: "room"}}).GetID(); got != "room" {
		t.Fatalf("unexpected room id %q", got)
	}
	if got := (agentItem{AgentSummary: protocol.AgentSummary{ID: "agent"}}).GetID(); got != "agent" {
		t.Fatalf("unexpected agent id %q", got)
	}
	if got := (threadItem{Thread: protocol.Thread{ID: "thread"}}).GetID(); got != "thread" {
		t.Fatalf("unexpected thread id %q", got)
	}
	if got := (directConversationItem{DirectConversation: protocol.DirectConversation{ID: "dm"}}).GetID(); got != "dm" {
		t.Fatalf("unexpected dm id %q", got)
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
