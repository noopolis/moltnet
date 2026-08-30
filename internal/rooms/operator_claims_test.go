package rooms

import (
	"context"
	"errors"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// The two tests below are the loosening half of the operator-credential
// guarantee. TestOperatorNotLockedOutByPairScopedFix (federation_access_test.go)
// and TestHTTPOperatorNotLockedOutOfRoomDiscovery (internal/transport) already
// fail if "operator" is defined too narrowly and an owner is locked out of
// their own network. Nothing failed if it was defined too *broadly* -- which
// is the mutation that actually matters, because it hands a peer credential
// the owner's bypass.
//
// "Operator" is now one predicate, authn.Claims.Operator: admin AND write.
// Relax it to admin OR write at its single definition, or re-inline a
// divergent copy at either of the two rooms-package call sites these tests
// drive (pairScopedWriteAllowed / roomVisibleToPairScopedContext in
// federation_access.go, and canWriteRoom in access_policy.go), and one of
// these goes red.

// TestPairScopedCredentialWithWriteScopeIsStillEnforced uses a `[write, pair]`
// credential: not an operator (no admin), so both federation gates must
// enforce the room's federation list against its pairing exactly as they do
// for a plain `[pair]` peer. Under an `admin || write` definition of
// "operator" this credential bypasses both gates -- writing into a
// `federation: none` room and seeing it listed.
func TestPairScopedCredentialWithWriteScopeIsStillEnforced(t *testing.T) {
	t.Parallel()

	service := newFederationAccessTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:         "private",
		Visibility: protocol.RoomVisibilityPublic,
		Members:    []string{"remote:writer"},
		Federation: protocol.DefaultRoomFederation(),
	}); err != nil {
		t.Fatalf("CreateRoom(private): %v", err)
	}

	writePairCtx := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
		ID:      "pair_remote",
		Network: "remote",
		Scopes:  []authn.Scope{authn.ScopeWrite, authn.ScopePair},
	}))

	if _, err := service.SendMessageContext(writePairCtx, remoteSend(protocol.Target{
		Kind: protocol.TargetKindRoom, RoomID: "private",
	})); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("[write pair] credential write to non-federated room error = %v, want ErrWriteForbidden", err)
	}
	if count := roomMessageCount(t, service, "private"); count != 0 {
		t.Fatalf("stored message count = %d, want 0", count)
	}

	page, err := service.ListRoomsContext(writePairCtx, protocol.PageRequest{})
	if err != nil {
		t.Fatalf("ListRoomsContext(): %v", err)
	}
	for _, room := range page.Rooms {
		if room.ID == "private" {
			t.Fatalf("ListRoomsContext() leaked non-federated room %q to a [write pair] credential", room.ID)
		}
	}
}

// TestAdminOnlyClaimsAreNotOperatorForWrite drives the third call site,
// canWriteRoom (access_policy.go), with the mirror-image credential: `[admin]`
// and no write. An operators-write-policy room must still refuse it, because
// that policy demands the `write` scope in its own right. Under an
// `admin || write` definition the operator bypass fires first and the write
// lands.
func TestAdminOnlyClaimsAreNotOperatorForWrite(t *testing.T) {
	t.Parallel()

	service := newFederationAccessTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:          "locked",
		Visibility:  protocol.RoomVisibilityPublic,
		WritePolicy: protocol.RoomWritePolicyOperators,
	}); err != nil {
		t.Fatalf("CreateRoom(locked): %v", err)
	}

	adminOnlyCtx := authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
		ID:     "admin-only",
		Scopes: []authn.Scope{authn.ScopeAdmin},
	}))

	if _, err := service.SendMessageContext(adminOnlyCtx, roomSend("locked", "someone")); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("[admin] credential write to operators-only room error = %v, want ErrWriteForbidden", err)
	}
	if count := roomMessageCount(t, service, "locked"); count != 0 {
		t.Fatalf("stored message count = %d, want 0", count)
	}
}
