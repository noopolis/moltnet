package rooms

import (
	"context"
	"errors"
	"net/http"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestWritePolicyMembersRejectsGeneratedNonMember(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	mustCreatePolicyRoom(t, service, "floor", []string{"member"}, protocol.RoomWritePolicyMembers)
	outsider := registerPolicyAgent(t, service, "outsider")

	_, err := service.SendMessageContext(bearerClaimsContext(outsider), roomSend("floor", "outsider"))
	if !errors.Is(err, ErrWriteForbidden) || statusCode(err) != http.StatusForbidden {
		t.Fatalf("expected generated non-member 403, got %v", err)
	}
	if _, err := service.SendMessageContext(context.Background(), roomSend("floor", "member")); err != nil {
		t.Fatalf("expected member send in local none mode, got %v", err)
	}
}

func TestWritePolicyRegisteredAgentsPermitsLocalRegistration(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	mustCreatePolicyRoom(t, service, "guestbook", []string{"member"}, protocol.RoomWritePolicyRegisteredAgents)
	guest := registerPolicyAgent(t, service, "guest")

	if _, err := service.SendMessageContext(bearerClaimsContext(guest), roomSend("guestbook", "guest")); err != nil {
		t.Fatalf("expected registered non-member send, got %v", err)
	}
	if _, err := service.SendMessageContext(
		bearerClaimsContext(staticClaims("writer", authn.ScopeWrite)),
		roomSend("guestbook", "unknown"),
	); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("expected unregistered non-member rejection, got %v", err)
	}
}

func TestWritePolicyAppliesToThreadSends(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	mustCreatePolicyRoom(t, service, "floor", []string{"member"}, protocol.RoomWritePolicyMembers)
	member := registerPolicyAgent(t, service, "member")
	outsider := registerPolicyAgent(t, service, "outsider")

	request := threadSend("floor", "thread_1", "outsider")
	_, err := service.SendMessageContext(bearerClaimsContext(outsider), request)
	if !errors.Is(err, ErrWriteForbidden) || statusCode(err) != http.StatusForbidden {
		t.Fatalf("expected generated non-member thread 403, got %v", err)
	}

	request.From.ID = "member"
	accepted, err := service.SendMessageContext(bearerClaimsContext(member), request)
	if err != nil {
		t.Fatalf("expected member thread send, got %v", err)
	}
	if !accepted.ThreadCreated {
		t.Fatalf("expected thread creation, got %#v", accepted)
	}
}

func TestWritePolicyOperatorsAllowsOnlyStaticWriteTokens(t *testing.T) {
	t.Parallel()

	service := newAgentRegistryTestService()
	mustCreatePolicyRoom(t, service, "ops", []string{"bot"}, protocol.RoomWritePolicyOperators)
	bot := registerPolicyAgent(t, service, "bot")

	if _, err := service.SendMessageContext(bearerClaimsContext(bot), roomSend("ops", "bot")); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("expected generated agent rejection, got %v", err)
	}
	if _, err := service.SendMessageContext(
		bearerClaimsContext(staticClaims("writer", authn.ScopeWrite)),
		roomSend("ops", "operator"),
	); err != nil {
		t.Fatalf("expected static write operator send, got %v", err)
	}
	if _, err := service.SendMessageContext(
		bearerClaimsContext(staticClaims("admin-writer", authn.ScopeAdmin, authn.ScopeWrite)),
		roomSend("ops", "admin"),
	); err != nil {
		t.Fatalf("expected admin+write operator send, got %v", err)
	}
}

func TestPairRelayDoesNotBypassMembership(t *testing.T) {
	t.Parallel()

	service := newTestService()
	mustCreatePolicyRoom(t, service, "floor", []string{"remote:member"}, protocol.RoomWritePolicyMembers)
	pairCtx := bearerClaimsContext(staticClaims("pair", authn.ScopePair))

	outsider := roomSend("floor", "outsider")
	outsider.Origin = protocol.MessageOrigin{NetworkID: "remote", MessageID: "remote_outsider"}
	outsider.From.NetworkID = "remote"
	if _, err := service.SendMessageContext(pairCtx, outsider); !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("expected remote non-member rejection, got %v", err)
	}

	member := roomSend("floor", "member")
	member.Origin = protocol.MessageOrigin{NetworkID: "remote", MessageID: "remote_member"}
	member.From.NetworkID = "remote"
	if _, err := service.SendMessageContext(pairCtx, member); err != nil {
		t.Fatalf("expected remote member relay, got %v", err)
	}
}

func mustCreatePolicyRoom(t *testing.T, service *Service, id string, members []string, policy string) {
	t.Helper()

	_, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:          id,
		Members:     members,
		WritePolicy: policy,
	})
	if err != nil {
		t.Fatalf("CreateRoom(%s) error = %v", id, err)
	}
}

func registerPolicyAgent(t *testing.T, service *Service, agentID string) authn.Claims {
	t.Helper()

	ctx := authn.WithAgentRegistration(context.Background(), authn.AgentRegistrationOpen)
	registered, err := service.RegisterAgentContext(ctx, protocol.RegisterAgentRequest{RequestedAgentID: agentID})
	if err != nil {
		t.Fatalf("RegisterAgentContext(%s) error = %v", agentID, err)
	}
	claims, ok, err := service.AuthenticateAgentTokenContext(context.Background(), registered.AgentToken)
	if err != nil || !ok {
		t.Fatalf("AuthenticateAgentTokenContext(%s) ok=%v err=%v", agentID, ok, err)
	}
	return claims
}

func bearerClaimsContext(claims authn.Claims) context.Context {
	return authn.WithMode(authn.WithClaims(context.Background(), claims), authn.ModeBearer)
}

func roomSend(roomID string, agentID string) protocol.SendMessageRequest {
	return protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: roomID},
		From:   protocol.Actor{Type: "agent", ID: agentID},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	}
}

func threadSend(roomID string, threadID string, agentID string) protocol.SendMessageRequest {
	return protocol.SendMessageRequest{
		Target: protocol.Target{
			Kind:            protocol.TargetKindThread,
			RoomID:          roomID,
			ThreadID:        threadID,
			ParentMessageID: "msg_parent",
		},
		From:  protocol.Actor{Type: "agent", ID: agentID},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	}
}

func statusCode(err error) int {
	var roomErr *Error
	if errors.As(err, &roomErr) {
		return roomErr.StatusCode()
	}
	return 0
}
