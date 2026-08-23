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

// TestPairScopedWriteWithUnboundNetworkIsRejected is 7B.1's required
// regression: it fails against pre-fix main. The credential here has the
// exact shape `pair invite` writes into auth.tokens[] for a peer that has
// never yet contacted this network back — pair scope, no bound network —
// while the pairing it belongs to (matched by TokenID, per
// pairingForPairScopedContext's doc comment) is already known locally. Pre-fix,
// the empty-network early return let this write through regardless of the
// room's federation list; post-fix it is enforced like any other pairing.
func TestPairScopedWriteWithUnboundNetworkIsRejected(t *testing.T) {
	t.Parallel()

	service := newFederationAccessTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID: "private", Members: []string{"remote:writer"}, Federation: protocol.DefaultRoomFederation(),
	}); err != nil {
		t.Fatalf("CreateRoom(private): %v", err)
	}

	unboundCtx := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
		ID:     "pair_remote", // matches the pairing id configured in newFederationAccessTestService
		Scopes: []authn.Scope{authn.ScopePair},
		// Network deliberately left unset -- the shape of a `pair invite`
		// token before the peer's real network id is ever confirmed.
	}))

	if _, err := service.SendMessageContext(unboundCtx, remoteSend(protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "private"})); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("unbound pair-scoped write to non-federated room error = %v, want ErrWriteForbidden", err)
	}
	if count := roomMessageCount(t, service, "private"); count != 0 {
		t.Fatalf("stored message count = %d, want 0", count)
	}
}

// TestPairScopedListingWithUnboundNetworkIsFiltered is the room-listing
// counterpart of TestPairScopedWriteWithUnboundNetworkIsRejected: 7B.1 closed
// the fail-open in both pairScopedWriteAllowed and
// roomVisibleToPairScopedContext, so an unbound pair-scoped credential must
// not see a room its pairing's federation list excludes, either.
func TestPairScopedListingWithUnboundNetworkIsFiltered(t *testing.T) {
	t.Parallel()

	service := newFederationAccessTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID: "private", Visibility: protocol.RoomVisibilityPublic, Federation: protocol.DefaultRoomFederation(),
	}); err != nil {
		t.Fatalf("CreateRoom(private): %v", err)
	}

	unboundCtx := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
		ID:     "pair_remote",
		Scopes: []authn.Scope{authn.ScopePair},
	}))

	page, err := service.ListRoomsContext(unboundCtx, protocol.PageRequest{})
	if err != nil {
		t.Fatalf("ListRoomsContext(): %v", err)
	}
	for _, room := range page.Rooms {
		if room.ID == "private" {
			t.Fatalf("ListRoomsContext() leaked non-federated room %q to unbound pair-scoped credential", room.ID)
		}
	}
}

// TestOperatorNotLockedOutByPairScopedFix is F1's required lockout guard,
// targeted at the actual minted scope set: `--bearer` init (and every
// existing config it already wrote before F1's fix) mints the operator
// token as [observe, write, admin, pair], not [observe, write, admin]. A
// test built without `pair` never exercised the bug at all -- both gates
// already short-circuited to true for a claim with no `pair` scope, before
// and after this fix. The canonical four-scope shape below is the one that
// found no pairing named "operator" and denied every write and every room
// listing; isOperatorClaims (federation_access.go) is what makes this pass.
func TestOperatorNotLockedOutByPairScopedFix(t *testing.T) {
	t.Parallel()

	service := newFederationAccessTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID: "private", Visibility: protocol.RoomVisibilityPublic, Federation: protocol.DefaultRoomFederation(),
	}); err != nil {
		t.Fatalf("CreateRoom(private): %v", err)
	}

	operatorCtx := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
		ID:     "operator",
		Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin, authn.ScopePair},
	}))

	if _, err := service.SendMessageContext(operatorCtx, roomSend("private", "operator")); err != nil {
		t.Fatalf("operator write to non-federated room error = %v, want nil", err)
	}

	page, err := service.ListRoomsContext(operatorCtx, protocol.PageRequest{})
	if err != nil {
		t.Fatalf("ListRoomsContext(): %v", err)
	}
	found := false
	for _, room := range page.Rooms {
		if room.ID == "private" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListRoomsContext() hid operator's own room %q", "private")
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
