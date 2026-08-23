package rooms

import (
	"errors"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestPairTokenCannotImpersonateAnotherPairingsNetwork(t *testing.T) {
	t.Parallel()

	service := newPairingCredentialBindingTestService()
	mustCreateFederatedPolicyRoom(t, service, "floor", []string{"network-c:member"})
	claims := authn.NewStaticClaims(authn.TokenConfig{
		ID:      "pair-b",
		Value:   "pair-b-secret",
		Network: "remote-b",
		Scopes:  []authn.Scope{authn.ScopePair},
	})
	request := roomSend("floor", "member")
	request.Origin = protocol.MessageOrigin{NetworkID: "network-c", MessageID: "network-c-message"}
	request.From.NetworkID = "network-c"

	before := roomMessageCount(t, service, "floor")
	// Regression: before pair credentials were bound to a remote network, this
	// correctly shaped member message was accepted using the remote-b token.
	if _, err := service.SendMessageContext(bearerClaimsContext(claims), request); err == nil {
		t.Fatal("expected cross-pairing impersonation attempt to be rejected")
	}
	if after := roomMessageCount(t, service, "floor"); after != before {
		t.Fatalf("stored message count = %d, want %d after rejected forgery", after, before)
	}
}

func TestPairTokenAcceptsBoundPairingNetwork(t *testing.T) {
	t.Parallel()

	service := newPairingCredentialBindingTestService()
	mustCreateFederatedPolicyRoom(t, service, "floor", []string{"remote-b:member"})
	claims := authn.NewStaticClaims(authn.TokenConfig{
		ID:      "pair-b",
		Value:   "pair-b-secret",
		Network: "remote-b",
		Scopes:  []authn.Scope{authn.ScopePair},
	})
	request := roomSend("floor", "member")
	request.Origin = protocol.MessageOrigin{NetworkID: "remote-b", MessageID: "remote-b-message"}
	request.From.NetworkID = "remote-b"

	before := roomMessageCount(t, service, "floor")
	if _, err := service.SendMessageContext(bearerClaimsContext(claims), request); err != nil {
		t.Fatalf("expected message from the bound pairing network to be accepted, got %v", err)
	}
	if after := roomMessageCount(t, service, "floor"); after != before+1 {
		t.Fatalf("stored message count = %d, want %d", after, before+1)
	}
}

func TestPairTokenCannotImpersonateAnotherPairingsNetworkAsHuman(t *testing.T) {
	t.Parallel()

	service := newPairingCredentialBindingTestService()
	mustCreateFederatedPolicyRoom(t, service, "floor", []string{"network-c:member"})
	claims := authn.NewStaticClaims(authn.TokenConfig{
		ID:      "pair-b",
		Value:   "pair-b-secret",
		Network: "remote-b",
		Scopes:  []authn.Scope{authn.ScopePair},
	})
	request := roomSend("floor", "member")
	request.From.Type = "human"
	request.Origin = protocol.MessageOrigin{NetworkID: "network-c", MessageID: "network-c-human-message"}
	request.From.NetworkID = "network-c"

	before := roomMessageCount(t, service, "floor")
	if _, err := service.SendMessageContext(bearerClaimsContext(claims), request); err == nil {
		t.Fatal("expected cross-pairing human impersonation attempt to be rejected")
	}
	if after := roomMessageCount(t, service, "floor"); after != before {
		t.Fatalf("stored message count = %d, want %d after rejected forgery", after, before)
	}
}

func TestPairTokenAcceptsBoundPairingNetworkAsHuman(t *testing.T) {
	t.Parallel()

	service := newPairingCredentialBindingTestService()
	mustCreateFederatedPolicyRoom(t, service, "floor", []string{"remote-b:member"})
	claims := authn.NewStaticClaims(authn.TokenConfig{
		ID:      "pair-b",
		Value:   "pair-b-secret",
		Network: "remote-b",
		Scopes:  []authn.Scope{authn.ScopePair},
	})
	request := roomSend("floor", "member")
	request.From.Type = "human"
	request.Origin = protocol.MessageOrigin{NetworkID: "remote-b", MessageID: "remote-b-human-message"}
	request.From.NetworkID = "remote-b"

	before := roomMessageCount(t, service, "floor")
	if _, err := service.SendMessageContext(bearerClaimsContext(claims), request); err != nil {
		t.Fatalf("expected human message from the bound pairing network to be accepted, got %v", err)
	}
	if after := roomMessageCount(t, service, "floor"); after != before+1 {
		t.Fatalf("stored message count = %d, want %d", after, before+1)
	}
}

func TestStrictPairNetworkBindingRejectsUnboundRemoteOriginSenders(t *testing.T) {
	t.Parallel()

	for _, senderType := range []string{"agent", "human"} {
		t.Run(senderType, func(t *testing.T) {
			service := newPairingCredentialBindingTestServiceWithStrictBinding(true)
			mustCreateFederatedPolicyRoom(t, service, "floor", []string{"remote-b:member"})
			// TokenID "pair-b" matches a real configured pairing (unlike a
			// fictitious id, which 7B.1's federation_access.go would now
			// refuse outright regardless of RequirePairNetworkBinding,
			// making this no longer isolate the strict-binding check this
			// test exists to exercise). Network is left unset -- that is
			// exactly the "unbound" shape under test here.
			claims := authn.NewStaticClaims(authn.TokenConfig{
				ID:     "pair-b",
				Value:  "pair-b-secret",
				Scopes: []authn.Scope{authn.ScopePair},
			})
			request := roomSend("floor", "member")
			request.From.Type = senderType
			request.From.NetworkID = "remote-b"
			request.Origin = protocol.MessageOrigin{NetworkID: "remote-b", MessageID: "remote-b-unbound-" + senderType}

			before := roomMessageCount(t, service, "floor")
			if _, err := service.SendMessageContext(bearerClaimsContext(claims), request); err == nil {
				t.Fatal("expected unbound pair credential to be rejected when strict binding is enabled")
			}
			if after := roomMessageCount(t, service, "floor"); after != before {
				t.Fatalf("stored message count = %d, want %d after strict rejection", after, before)
			}
		})
	}
}

func TestStrictPairNetworkBindingAcceptsBoundRemoteOriginCredential(t *testing.T) {
	t.Parallel()

	service := newPairingCredentialBindingTestServiceWithStrictBinding(true)
	mustCreateFederatedPolicyRoom(t, service, "floor", []string{"remote-b:member"})
	claims := authn.NewStaticClaims(authn.TokenConfig{
		ID:      "pair-b",
		Value:   "pair-b-secret",
		Network: "remote-b",
		Scopes:  []authn.Scope{authn.ScopePair},
	})
	request := roomSend("floor", "member")
	request.From.NetworkID = "remote-b"
	request.Origin = protocol.MessageOrigin{NetworkID: "remote-b", MessageID: "remote-b-strict-bound"}

	before := roomMessageCount(t, service, "floor")
	if _, err := service.SendMessageContext(bearerClaimsContext(claims), request); err != nil {
		t.Fatalf("expected bound pair credential to be accepted in strict mode, got %v", err)
	}
	if after := roomMessageCount(t, service, "floor"); after != before+1 {
		t.Fatalf("stored message count = %d, want %d", after, before+1)
	}
}

func TestDefaultPairNetworkBindingAcceptsUnboundRemoteOriginCredential(t *testing.T) {
	t.Parallel()

	service := newPairingCredentialBindingTestService()
	mustCreateFederatedPolicyRoom(t, service, "floor", []string{"remote-b:member"})
	// TokenID "pair-b" matches a real configured pairing -- see
	// TestStrictPairNetworkBindingRejectsUnboundRemoteOriginSenders's comment
	// above for why a fictitious id no longer works here under 7B.1. Network
	// is left unset: that is the "unbound" shape this test is about.
	claims := authn.NewStaticClaims(authn.TokenConfig{
		ID:     "pair-b",
		Value:  "pair-b-secret",
		Scopes: []authn.Scope{authn.ScopePair},
	})
	request := roomSend("floor", "member")
	request.From.NetworkID = "remote-b"
	request.Origin = protocol.MessageOrigin{NetworkID: "remote-b", MessageID: "remote-b-default-unbound"}

	before := roomMessageCount(t, service, "floor")
	if _, err := service.SendMessageContext(bearerClaimsContext(claims), request); err != nil {
		t.Fatalf("expected unbound pair credential to be accepted by default, got %v", err)
	}
	if after := roomMessageCount(t, service, "floor"); after != before+1 {
		t.Fatalf("stored message count = %d, want %d", after, before+1)
	}
}

func newPairingCredentialBindingTestService() *Service {
	return newPairingCredentialBindingTestServiceWithStrictBinding(false)
}

func newPairingCredentialBindingTestServiceWithStrictBinding(requirePairNetworkBinding bool) *Service {
	memory := store.NewMemoryStore()
	return NewService(ServiceConfig{
		AllowHumanIngress:         true,
		RequirePairNetworkBinding: requirePairNetworkBinding,
		NetworkID:                 "local",
		NetworkName:               "Local",
		Version:                   "test",
		Pairings: []protocol.Pairing{
			{ID: "pair-b", RemoteNetworkID: "remote-b", Token: "pair-b-secret"},
			{ID: "pair-c", RemoteNetworkID: "network-c", Token: "pair-c-secret"},
			// The inviting side's own shape: `pair invite` mints the
			// credential before the peer has ever spoken, so RemoteNetworkID
			// is empty and bindPairingNetworks has nothing to bind from.
			{ID: "pair-unbound", Token: "pair-unbound-secret"},
		},
		Store:    memory,
		Messages: memory,
		Broker:   events.NewBroker(),
	})
}

func mustCreateFederatedPolicyRoom(t *testing.T, service *Service, id string, members []string) {
	t.Helper()

	_, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:          id,
		Members:     members,
		WritePolicy: protocol.RoomWritePolicyMembers,
		Federation:  &protocol.RoomFederation{Mode: protocol.RoomFederationAll},
	})
	if err != nil {
		t.Fatalf("CreateRoom(%s) error = %v", id, err)
	}
}

func roomMessageCount(t *testing.T, service *Service, roomID string) int {
	t.Helper()

	page, err := service.ListRoomMessages(roomID, "", 100)
	if err != nil && !errors.Is(err, ErrUnknownRoom) {
		t.Fatalf("ListRoomMessages(%q) error = %v", roomID, err)
	}
	return len(page.Messages)
}

// An UNBOUND pair credential — the shape `moltnet pair invite` always mints,
// because the inviting side never learns its peer's real network id — used to
// be allowed to assert any origin network on every message, forever. That is
// the cross-pairing impersonation the code logged about but permitted.
//
// Trust-on-first-use closes it: the first origin such a credential asserts is
// the one it is held to for the rest of the process.
func TestUnboundPairTokenIsPinnedToTheOriginItFirstAsserts(t *testing.T) {
	t.Parallel()

	service := newPairingCredentialBindingTestService()
	mustCreateFederatedPolicyRoom(t, service, "floor", []string{"remote-b:member", "network-c:member"})
	// Network deliberately empty: this is the inviter-side credential shape.
	claims := authn.NewStaticClaims(authn.TokenConfig{
		ID:     "pair-unbound",
		Value:  "pair-unbound-secret",
		Scopes: []authn.Scope{authn.ScopePair},
	})

	first := roomSend("floor", "member")
	first.Origin = protocol.MessageOrigin{NetworkID: "remote-b", MessageID: "remote-b-1"}
	first.From.NetworkID = "remote-b"
	if _, err := service.SendMessageContext(bearerClaimsContext(claims), first); err != nil {
		t.Fatalf("first contact from an unbound pair credential should be accepted: %v", err)
	}

	// Same credential, different claimed origin. Before the learned binding
	// this was accepted and stored as if it came from network-c.
	forged := roomSend("floor", "member")
	forged.Origin = protocol.MessageOrigin{NetworkID: "network-c", MessageID: "network-c-1"}
	forged.From.NetworkID = "network-c"

	before := roomMessageCount(t, service, "floor")
	if _, err := service.SendMessageContext(bearerClaimsContext(claims), forged); err == nil {
		t.Fatal("expected a second, different origin from the same credential to be rejected")
	}
	if after := roomMessageCount(t, service, "floor"); after != before {
		t.Fatalf("stored message count = %d, want %d after rejected forgery", after, before)
	}

	// The pinned origin must keep working.
	again := roomSend("floor", "member")
	again.Origin = protocol.MessageOrigin{NetworkID: "remote-b", MessageID: "remote-b-2"}
	again.From.NetworkID = "remote-b"
	if _, err := service.SendMessageContext(bearerClaimsContext(claims), again); err != nil {
		t.Fatalf("the origin this credential was pinned to must still be accepted: %v", err)
	}
}

// Provenance: the receiving network records WHICH pairing an inbound relayed
// message actually arrived through, resolved from the presenting credential.
// This is the only field on a relayed message the local operator authored --
// every other identity field is the peer's own claim -- so it must be stamped
// by the server, never read from the wire, and never sent onward.
func TestInboundRelayedMessageRecordsThePairingItArrivedThrough(t *testing.T) {
	t.Parallel()

	service := newPairingCredentialBindingTestService()
	mustCreateFederatedPolicyRoom(t, service, "floor", []string{"remote-b:member"})
	claims := authn.NewStaticClaims(authn.TokenConfig{
		ID:      "pair-b",
		Value:   "pair-b-secret",
		Network: "remote-b",
		Scopes:  []authn.Scope{authn.ScopePair},
	})

	request := roomSend("floor", "member")
	request.Origin = protocol.MessageOrigin{
		NetworkID: "remote-b",
		MessageID: "remote-b-message",
		// A hostile peer naming someone else's pairing. Must be discarded.
		ReceivedVia: "pair-c",
	}
	request.From.NetworkID = "remote-b"

	if _, err := service.SendMessageContext(bearerClaimsContext(claims), request); err != nil {
		t.Fatalf("SendMessageContext() error = %v", err)
	}

	page, err := service.ListRoomMessages("floor", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 1 {
		t.Fatalf("stored %d messages, want 1", len(page.Messages))
	}
	if got := page.Messages[0].Origin.ReceivedVia; got != "pair-b" {
		t.Fatalf("Origin.ReceivedVia = %q, want the pairing the credential actually resolves to (%q)", got, "pair-b")
	}
}

// A locally-originated message has no pairing provenance: it did not arrive
// through one. An empty value is what distinguishes "mine" from "a peer's".
func TestLocalMessageRecordsNoPairingProvenance(t *testing.T) {
	t.Parallel()

	service := newPairingCredentialBindingTestService()
	mustCreateFederatedPolicyRoom(t, service, "floor", []string{"member"})

	request := roomSend("floor", "member")
	if _, err := service.SendMessageContext(t.Context(), request); err != nil {
		t.Fatalf("SendMessageContext() error = %v", err)
	}

	page, err := service.ListRoomMessages("floor", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if got := page.Messages[0].Origin.ReceivedVia; got != "" {
		t.Fatalf("Origin.ReceivedVia = %q, want empty for a local message", got)
	}
}

// The stamp is this network's own bookkeeping. Relaying it onward would hand
// the far side a field it must never see, and it would arrive there as
// testimony rather than as that network's own record.
func TestRelayedRequestDoesNotCarryPairingProvenanceOnward(t *testing.T) {
	t.Parallel()

	service := newPairingCredentialBindingTestService()
	message := protocol.Message{
		ID:        "msg_1",
		NetworkID: "local",
		Origin:    protocol.MessageOrigin{NetworkID: "local", MessageID: "msg_1", ReceivedVia: "pair-b"},
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "floor"},
		From:      protocol.Actor{Type: "agent", ID: "member", NetworkID: "local"},
		Parts:     []protocol.Part{{Kind: "text", Text: "hi"}},
	}

	relayed, ok := service.relayRequest(protocol.Pairing{ID: "pair-b"}, message)
	if !ok {
		t.Fatal("relayRequest() refused a room message")
	}
	if relayed.Origin.ReceivedVia != "" {
		t.Fatalf("relayed Origin.ReceivedVia = %q, want it stripped", relayed.Origin.ReceivedVia)
	}
	if relayed.Origin.NetworkID != "local" {
		t.Fatalf("stripping provenance must not disturb the rest of the origin: %+v", relayed.Origin)
	}
}
