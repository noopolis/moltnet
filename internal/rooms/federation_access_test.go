package rooms

import (
	"context"
	"errors"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestPairScopedWriteToNonFederatedRoomIsRejected(t *testing.T) {
	t.Parallel()

	service := newFederationAccessTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID: "private", Members: []string{"remote:writer"}, Federation: protocol.DefaultRoomFederation(),
	}); err != nil {
		t.Fatalf("CreateRoom(private): %v", err)
	}

	if _, err := service.SendMessageContext(pairScopedRemoteContext(), remoteSend(protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "private"})); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("non-federated room write error = %v, want ErrWriteForbidden", err)
	}
	if count := roomMessageCount(t, service, "private"); count != 0 {
		t.Fatalf("stored message count = %d, want 0", count)
	}
}

func TestPairScopedThreadWriteUsesStoredRoomFederation(t *testing.T) {
	t.Parallel()

	service := newFederationAccessTestService()
	for _, request := range []protocol.CreateRoomRequest{
		{ID: "private", Members: []string{"remote:writer"}, Federation: protocol.DefaultRoomFederation()},
		{ID: "shared", Members: []string{"remote:writer"}, Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"pair_remote"}}},
	} {
		if _, err := service.CreateRoom(request); err != nil {
			t.Fatalf("CreateRoom(%q): %v", request.ID, err)
		}
	}
	if _, err := service.SendMessage(threadSend("private", "thread_private", "local")); err != nil {
		t.Fatalf("create private thread: %v", err)
	}
	before := roomMessageCount(t, service, "private")

	if _, err := service.SendMessageContext(pairScopedRemoteContext(), remoteSend(protocol.Target{
		Kind: protocol.TargetKindThread, RoomID: "shared", ThreadID: "thread_private", ParentMessageID: "msg_parent",
	})); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("stored non-federated thread write error = %v, want ErrWriteForbidden", err)
	}
	if count := roomMessageCount(t, service, "private"); count != before {
		t.Fatalf("stored private thread message count = %d, want %d", count, before)
	}
}

func TestRuntimeCreatedRoomsDefaultToNonFederated(t *testing.T) {
	t.Parallel()

	room, err := roomFromCreateRequest("local", protocol.CreateRoomRequest{ID: "runtime"})
	if err != nil {
		t.Fatalf("roomFromCreateRequest(): %v", err)
	}
	if room.Federation == nil || room.Federation.Mode != protocol.RoomFederationNone {
		t.Fatalf("runtime federation = %#v, want none", room.Federation)
	}
}

func newFederationAccessTestService() *Service {
	memory := store.NewMemoryStore()
	return NewService(ServiceConfig{
		NetworkID: "local",
		Pairings: []protocol.Pairing{{
			ID: "pair_remote", RemoteNetworkID: "remote",
		}},
		Store: memory, Messages: memory, Broker: events.NewBroker(),
	})
}

func pairScopedRemoteContext() context.Context {
	return authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
		ID: "pair_remote", Network: "remote", Scopes: []authn.Scope{authn.ScopePair},
	}))
}

func remoteSend(target protocol.Target) protocol.SendMessageRequest {
	return protocol.SendMessageRequest{
		Target: target,
		From:   protocol.Actor{Type: "agent", ID: "writer", NetworkID: "remote"},
		Origin: protocol.MessageOrigin{NetworkID: "remote", MessageID: "remote-message"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "from remote"}},
	}
}
