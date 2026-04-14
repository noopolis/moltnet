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

func TestRegisterAgentCredentialOwnership(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	firstCtx := authn.WithClaims(context.Background(), authn.Claims{TokenID: "one"})
	secondCtx := authn.WithClaims(context.Background(), authn.Claims{TokenID: "two"})

	registered, err := service.RegisterAgentContext(firstCtx, protocol.RegisterAgentRequest{
		RequestedAgentID: "director",
		Name:             "Director",
	})
	if err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if registered.AgentID != "director" ||
		registered.ActorURI != protocol.AgentFQID("local", "director") ||
		registered.DisplayName != "Director" {
		t.Fatalf("unexpected registration %#v", registered)
	}

	again, err := service.RegisterAgentContext(firstCtx, protocol.RegisterAgentRequest{
		RequestedAgentID: "director",
		Name:             "Director Prime",
	})
	if err != nil {
		t.Fatalf("idempotent RegisterAgentContext() error = %v", err)
	}
	if again.ActorUID != registered.ActorUID || again.DisplayName != "Director Prime" {
		t.Fatalf("unexpected idempotent registration %#v", again)
	}

	if _, err := service.RegisterAgentContext(secondCtx, protocol.RegisterAgentRequest{
		RequestedAgentID: "director",
		Name:             "Impostor",
	}); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected agent conflict, got %v", err)
	}
}

func TestRegisteredSenderCredentialIsEnforced(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	firstCtx := authn.WithClaims(context.Background(), authn.Claims{TokenID: "one"})
	secondCtx := authn.WithClaims(context.Background(), authn.Claims{TokenID: "two"})

	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research", Members: []string{"director"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterAgentContext(firstCtx, protocol.RegisterAgentRequest{
		RequestedAgentID: "director",
		Name:             "Director",
	}); err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}

	if _, err := service.SendMessageContext(firstCtx, protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "director"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	}); err != nil {
		t.Fatalf("SendMessageContext() owner error = %v", err)
	}

	if _, err := service.SendMessageContext(secondCtx, protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "director"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	}); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected sender identity conflict, got %v", err)
	}
}

func TestRegisterAgentGeneratesHandle(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	registered, err := service.RegisterAgentContext(context.Background(), protocol.RegisterAgentRequest{
		Name: "Director Prime",
	})
	if err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if registered.AgentID != "director-prime" {
		t.Fatalf("unexpected generated agent id %#v", registered)
	}
}

func TestRegisterAgentGeneratesASCIIHandle(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	registered, err := service.RegisterAgentContext(context.Background(), protocol.RegisterAgentRequest{
		Name: "Díréctor Prime",
	})
	if err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if registered.AgentID != "d-r-ctor-prime" {
		t.Fatalf("unexpected generated agent id %#v", registered)
	}
	if err := protocol.ValidateMemberID(registered.AgentID); err != nil {
		t.Fatalf("generated agent id should be valid, got %v", err)
	}
}

func newAgentRegistryTestService() *Service {
	memory := store.NewMemoryStore()
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
