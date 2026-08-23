package rooms

import (
	"context"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSendMessageStampsCredentialBoundActorFromAuthenticatedClaims(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "pitch", Members: []string{"world"}}); err != nil {
		t.Fatal(err)
	}
	worldClaims := authn.NewStaticClaims(authn.TokenConfig{
		ID:     "world",
		Scopes: []authn.Scope{authn.ScopeAttach, authn.ScopeWrite},
		Agents: []string{"world"},
	})
	worldContext := authn.WithClaims(context.Background(), worldClaims)
	if _, err := service.RegisterAgentContext(worldContext, protocol.RegisterAgentRequest{RequestedAgentID: "world"}); err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}

	send := func(ctx context.Context, id string, credentialBound bool) {
		t.Helper()
		if _, err := service.SendMessageContext(ctx, protocol.SendMessageRequest{
			ID:     id,
			Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "pitch"},
			From: protocol.Actor{
				Type:            "agent",
				ID:              "world",
				CredentialBound: credentialBound,
			},
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "wake"}},
		}); err != nil {
			t.Fatalf("SendMessageContext(%s) error = %v", id, err)
		}
	}

	send(worldContext, "bound", false)
	attachOnlyClaims := authn.NewStaticClaims(authn.TokenConfig{
		ID:     "world",
		Scopes: []authn.Scope{authn.ScopeAttach},
		Agents: []string{"world"},
	})
	send(authn.WithClaims(context.Background(), attachOnlyClaims), "attach-only", true)
	adminClaims := authn.NewStaticClaims(authn.TokenConfig{
		ID:     "operator",
		Scopes: []authn.Scope{authn.ScopeAdmin, authn.ScopeWrite},
	})
	send(authn.WithClaims(context.Background(), adminClaims), "admin-spoof", true)

	page, err := service.ListRoomMessages("pitch", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	bound := messageByID(t, page.Messages, "bound")
	if !bound.From.CredentialBound {
		t.Fatalf("expected actor-owned credential to bind sender, got %#v", bound.From)
	}
	attachOnly := messageByID(t, page.Messages, "attach-only")
	if attachOnly.From.CredentialBound {
		t.Fatalf("attach-only credential stamped sender provenance: %#v", attachOnly.From)
	}
	spoof := messageByID(t, page.Messages, "admin-spoof")
	if spoof.From.CredentialBound {
		t.Fatalf("caller-forged provenance survived admin impersonation: %#v", spoof.From)
	}
}

func TestSendMessageClearsCredentialBindingOutsideLocalActorCredential(t *testing.T) {
	t.Parallel()

	t.Run("unauthenticated", func(t *testing.T) {
		service := newAgentRegistryTestService()
		if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "pitch", Members: []string{"world"}}); err != nil {
			t.Fatal(err)
		}
		assertUnboundStoredSender(t, service, context.Background(), protocol.SendMessageRequest{
			ID:     "anonymous",
			Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "pitch"},
			From:   protocol.Actor{Type: "agent", ID: "world", CredentialBound: true},
			Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "wake"}},
		})
	})

	t.Run("human", func(t *testing.T) {
		service := newAgentRegistryTestService()
		if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "pitch", Members: []string{"operator"}}); err != nil {
			t.Fatal(err)
		}
		claims := authn.NewStaticClaims(authn.TokenConfig{ID: "operator", Scopes: []authn.Scope{authn.ScopeWrite}})
		assertUnboundStoredSender(t, service, authn.WithClaims(context.Background(), claims), protocol.SendMessageRequest{
			ID:     "human",
			Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "pitch"},
			From:   protocol.Actor{Type: "human", ID: "operator", CredentialBound: true},
			Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "wake"}},
		})
	})

	t.Run("paired remote relay", func(t *testing.T) {
		// 7B.1: this pair-scoped send is now checked against "pitch"'s
		// federation stance, so the matching pairing is configured and the
		// room is opened to every pairing ("all") -- this subtest is about
		// credential-binding provenance, not federation.
		service := newAgentRegistryTestServiceWithPairings([]protocol.Pairing{{ID: "pair", RemoteNetworkID: "remote"}})
		if _, err := service.CreateRoom(protocol.CreateRoomRequest{
			ID: "pitch", Members: []string{"remote:world"},
			Federation: &protocol.RoomFederation{Mode: protocol.RoomFederationAll},
		}); err != nil {
			t.Fatal(err)
		}
		claims := authn.NewStaticClaims(authn.TokenConfig{
			ID:     "pair",
			Scopes: []authn.Scope{authn.ScopePair, authn.ScopeWrite},
			Agents: []string{"world"},
		})
		assertUnboundStoredSender(t, service, authn.WithClaims(context.Background(), claims), protocol.SendMessageRequest{
			ID:     "remote",
			Origin: protocol.MessageOrigin{NetworkID: "remote", MessageID: "remote"},
			Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "pitch"},
			From: protocol.Actor{
				Type:            "service",
				ID:              "world",
				NetworkID:       "remote",
				FQID:            protocol.AgentFQID("remote", "world"),
				CredentialBound: true,
			},
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "wake"}},
		})
	})
}

func assertUnboundStoredSender(t *testing.T, service *Service, ctx context.Context, request protocol.SendMessageRequest) {
	t.Helper()
	if _, err := service.SendMessageContext(ctx, request); err != nil {
		t.Fatalf("SendMessageContext() error = %v", err)
	}
	page, err := service.ListRoomMessages(request.Target.RoomID, "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	message := messageByID(t, page.Messages, request.ID)
	if message.From.CredentialBound {
		t.Fatalf("untrusted sender retained credential binding: %#v", message.From)
	}
}

func messageByID(t *testing.T, messages []protocol.Message, id string) protocol.Message {
	t.Helper()
	for _, message := range messages {
		if message.ID == id {
			return message
		}
	}
	t.Fatalf("message %q not found in %#v", id, messages)
	return protocol.Message{}
}
