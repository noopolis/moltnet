package rooms

import (
	"context"
	"errors"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestOpenRegistrationMintsShownOnceAgentToken(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	openCtx := authn.WithMode(context.Background(), authn.ModeOpen)
	registered, err := service.RegisterAgentContext(openCtx, protocol.RegisterAgentRequest{
		RequestedAgentID: "luna",
		Name:             "Luna",
	})
	if err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if !strings.HasPrefix(registered.AgentToken, authn.AgentTokenPrefix) {
		t.Fatalf("expected shown-once agent token, got %#v", registered)
	}
	if !strings.HasPrefix(registered.CredentialKey, "agent-token:") || registered.CredentialKey == "anonymous" {
		t.Fatalf("unexpected credential key %q", registered.CredentialKey)
	}

	stored, ok, err := service.registeredAgent(context.Background(), "luna")
	if err != nil || !ok {
		t.Fatalf("registeredAgent() ok=%v err=%v", ok, err)
	}
	if stored.AgentToken != "" || stored.CredentialKey != authn.AgentTokenCredentialKey(registered.AgentToken) {
		t.Fatalf("unexpected stored registration %#v", stored)
	}

	claims, ok, err := service.AuthenticateAgentTokenContext(context.Background(), registered.AgentToken)
	if err != nil || !ok {
		t.Fatalf("AuthenticateAgentTokenContext() ok=%v err=%v", ok, err)
	}
	if !claims.AllowsAgent("luna") || !claims.Allows(authn.ScopeWrite) || claims.Allows(authn.ScopeAdmin) {
		t.Fatalf("unexpected agent-token claims %#v", claims)
	}

	again, err := service.RegisterAgentContext(openContextWithClaims(claims), protocol.RegisterAgentRequest{
		RequestedAgentID: "luna",
		Name:             "Luna Prime",
	})
	if err != nil {
		t.Fatalf("idempotent RegisterAgentContext() error = %v", err)
	}
	if again.AgentToken != "" || again.ActorUID != registered.ActorUID || again.DisplayName != "Luna Prime" {
		t.Fatalf("unexpected idempotent registration %#v", again)
	}

	if _, err := service.RegisterAgentContext(openCtx, protocol.RegisterAgentRequest{
		RequestedAgentID: "luna",
	}); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected anonymous duplicate conflict, got %v", err)
	}
}

func TestOpenModeSendOwnership(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "agora"}); err != nil {
		t.Fatal(err)
	}
	luna := registerOpenAgentForTest(t, service, "luna")
	atlas := registerOpenAgentForTest(t, service, "atlas")

	send := protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "agora"},
		From:   protocol.Actor{Type: "agent", ID: "luna"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	}
	if _, err := service.SendMessageContext(authn.WithMode(context.Background(), authn.ModeOpen), send); !errors.Is(err, ErrAgentUnauthorized) {
		t.Fatalf("expected missing-token rejection, got %v", err)
	}
	if _, err := service.SendMessageContext(openContextWithClaims(luna), send); err != nil {
		t.Fatalf("matching agent token send error = %v", err)
	}
	if _, err := service.SendMessageContext(openContextWithClaims(atlas), send); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected wrong-token conflict, got %v", err)
	}

	unregistered := send
	unregistered.From.ID = "newcomer"
	if _, err := service.SendMessageContext(openContextWithClaims(staticClaims("writer", authn.ScopeWrite)), unregistered); !errors.Is(err, ErrAgentUnauthorized) {
		t.Fatalf("expected unregistered local-agent rejection, got %v", err)
	}
}

func TestOpenModeStaticOwnershipAndAdminOverride(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "agora"}); err != nil {
		t.Fatal(err)
	}
	registerOpenAgentForTest(t, service, "luna")
	send := protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "agora"},
		From:   protocol.Actor{Type: "agent", ID: "luna"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	}

	if _, err := service.SendMessageContext(openContextWithClaims(staticClaims("writer", authn.ScopeWrite)), send); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected write-only non-owner conflict, got %v", err)
	}
	adminWriter := staticClaims("admin-writer", authn.ScopeAdmin, authn.ScopeWrite)
	if _, err := service.SendMessageContext(openContextWithClaims(adminWriter), send); err != nil {
		t.Fatalf("expected admin+write override, got %v", err)
	}
}

func TestNoneModeAnonymousRegistrationCanSend(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "agora"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterAgentContext(context.Background(), protocol.RegisterAgentRequest{
		RequestedAgentID: "luna",
	}); err != nil {
		t.Fatalf("RegisterAgentContext() error = %v", err)
	}
	if _, err := service.SendMessageContext(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "agora"},
		From:   protocol.Actor{Type: "agent", ID: "luna"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	}); err != nil {
		t.Fatalf("none-mode anonymous registered send error = %v", err)
	}
}

func registerOpenAgentForTest(t *testing.T, service *Service, agentID string) authn.Claims {
	t.Helper()

	registered, err := service.RegisterAgentContext(
		authn.WithMode(context.Background(), authn.ModeOpen),
		protocol.RegisterAgentRequest{RequestedAgentID: agentID},
	)
	if err != nil {
		t.Fatalf("RegisterAgentContext(%s) error = %v", agentID, err)
	}
	claims, ok, err := service.AuthenticateAgentTokenContext(context.Background(), registered.AgentToken)
	if err != nil || !ok {
		t.Fatalf("AuthenticateAgentTokenContext(%s) ok=%v err=%v", agentID, ok, err)
	}
	return claims
}

func openContextWithClaims(claims authn.Claims) context.Context {
	return authn.WithMode(authn.WithClaims(context.Background(), claims), authn.ModeOpen)
}

func staticClaims(tokenID string, scopes ...authn.Scope) authn.Claims {
	return authn.NewStaticClaims(authn.TokenConfig{
		ID:     tokenID,
		Scopes: scopes,
	})
}
