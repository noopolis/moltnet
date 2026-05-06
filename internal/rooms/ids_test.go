package rooms

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

var prefixedUUIDPattern = regexp.MustCompile(
	`^[a-z]+_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func TestNewPrefixedIDUsesCursorSafeUUIDs(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{"msg", "evt", "actor"} {
		id := newPrefixedID(prefix)
		if !prefixedUUIDPattern.MatchString(id) {
			t.Fatalf("expected %s id to use a UUID suffix, got %q", prefix, id)
		}
		if err := protocol.ValidateMessageID(id); err != nil {
			t.Fatalf("expected %s id to validate as a cursor, got %v", prefix, err)
		}
	}
}

func TestDeterministicPrefixedIDUsesStableUUIDs(t *testing.T) {
	t.Parallel()

	first := deterministicPrefixedID("evt", "msg_1")
	second := deterministicPrefixedID("evt", "msg_1")
	other := deterministicPrefixedID("evt", "msg_2")
	if first != second {
		t.Fatalf("expected stable deterministic id, got %q then %q", first, second)
	}
	if first == other {
		t.Fatalf("expected distinct deterministic ids, got %q", first)
	}
	if !prefixedUUIDPattern.MatchString(first) {
		t.Fatalf("expected UUID-shaped deterministic id, got %q", first)
	}
}

func TestGeneratedMessageIDsDoNotCollideAcrossServices(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	firstService := newTestServiceWithStore(memory)
	if _, err := firstService.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	first, err := firstService.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "alpha"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "first"}},
	})
	if err != nil {
		t.Fatalf("first SendMessage() error = %v", err)
	}

	secondService := newTestServiceWithStore(memory)
	second, err := secondService.SendMessage(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "beta"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "second"}},
	})
	if err != nil {
		t.Fatalf("second SendMessage() error = %v", err)
	}

	if first.MessageID == second.MessageID {
		t.Fatalf("expected generated IDs to differ across service instances, got %q", first.MessageID)
	}
	for _, id := range []string{first.MessageID, second.MessageID, first.EventID, second.EventID} {
		if !strings.Contains(id, "_") {
			t.Fatalf("expected prefixed id, got %q", id)
		}
		if err := protocol.ValidateMessageID(id); err != nil {
			t.Fatalf("expected generated id %q to validate, got %v", id, err)
		}
	}

	page, err := secondService.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("expected both messages to persist, got %#v", page.Messages)
	}
}

func newTestServiceWithStore(memory *store.MemoryStore) *Service {
	return NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})
}

func TestRegisterAgentUsesUUIDActorUID(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithStore(store.NewMemoryStore())
	registration, err := service.RegisterAgentContext(context.Background(), protocol.RegisterAgentRequest{
		RequestedAgentID: "luna",
		Name:             "Luna",
	})
	if err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if !prefixedUUIDPattern.MatchString(registration.ActorUID) {
		t.Fatalf("expected UUID-shaped actor uid, got %q", registration.ActorUID)
	}
}
