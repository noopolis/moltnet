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
	mustCreatePolicyRoom(t, service, "floor", []string{"network-c:member"}, protocol.RoomWritePolicyMembers)
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
	mustCreatePolicyRoom(t, service, "floor", []string{"remote-b:member"}, protocol.RoomWritePolicyMembers)
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
	mustCreatePolicyRoom(t, service, "floor", []string{"network-c:member"}, protocol.RoomWritePolicyMembers)
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
	mustCreatePolicyRoom(t, service, "floor", []string{"remote-b:member"}, protocol.RoomWritePolicyMembers)
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

func newPairingCredentialBindingTestService() *Service {
	memory := store.NewMemoryStore()
	return NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Pairings: []protocol.Pairing{
			{ID: "pair-b", RemoteNetworkID: "remote-b", Token: "pair-b-secret"},
			{ID: "pair-c", RemoteNetworkID: "network-c", Token: "pair-c-secret"},
		},
		Store:    memory,
		Messages: memory,
		Broker:   events.NewBroker(),
	})
}

func roomMessageCount(t *testing.T, service *Service, roomID string) int {
	t.Helper()

	page, err := service.ListRoomMessages(roomID, "", 100)
	if err != nil && !errors.Is(err, ErrUnknownRoom) {
		t.Fatalf("ListRoomMessages(%q) error = %v", roomID, err)
	}
	return len(page.Messages)
}
