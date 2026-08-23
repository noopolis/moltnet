package rooms

import (
	"context"
	"errors"
	"net/http"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/store"
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

// TestPairRelayDoesNotBypassMembership exercises the write-policy check
// underneath the federation gates, using a fully bound pair credential on a
// service that explicitly opts into require_pair_network_binding
// (newBoundPairTestService, below). auth.require_pair_network_binding
// itself defaults to false (F3's revert -- see config_load.go's
// defaultConfig doc comment), not true; it is this test's service, not the
// global default, that makes an unbound credential like
// TestUnboundPairCredentialIsRefused's get refused before reaching
// membership at all. The pairing and the room's federation list both name "pair_remote"
// so 7B.1's identity-based enforcement (federation_access.go) also passes
// cleanly, isolating this test to what it actually means to check: a
// relayed message still has to satisfy the room's own write policy.
func TestPairRelayDoesNotBypassMembership(t *testing.T) {
	t.Parallel()

	service := newBoundPairTestService(t)
	mustCreatePolicyRoomWithFederation(t, service, "floor", []string{"remote:member"}, protocol.RoomWritePolicyMembers,
		&protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"pair_remote"}})
	pairCtx := bearerClaimsContext(boundPairClaims())

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

// TestUnboundPairCredentialIsRefused is 7B.2's required regression: with
// require_pair_network_binding explicitly enabled on this service (the
// default reverted to false under F3 -- see config_load.go's defaultConfig
// doc comment -- but the underlying enforcement this test exercises is
// unchanged and still opt-in via auth.require_pair_network_binding: true), a
// pair-scoped credential with no bound network claiming a remote origin must
// be refused, not merely warned about. The rejection surfaces from
// validateSenderIdentity's remote-origin actor check (messaging.go), not the
// room's write policy, so it is asserted on ErrWriteForbidden's sibling
// ErrAgentForbidden rather than ErrWriteForbidden.
//
// F3 (confirmed live): this used to assert ErrAgentConflict, because the
// unbound-credential path fell through into agentCollisionID's generic
// "agent already registered with different credentials" 409 -- a real, but
// wildly misleading, error for what is actually a pair-credential binding
// refusal. remoteOriginPairStatus (messaging.go) now gives this its own
// accurate ErrAgentForbidden instead of falling through.
func TestUnboundPairCredentialIsRefused(t *testing.T) {
	t.Parallel()

	service := newBoundPairTestService(t)
	mustCreatePolicyRoomWithFederation(t, service, "floor", []string{"remote:member"}, protocol.RoomWritePolicyMembers,
		&protocol.RoomFederation{Mode: protocol.RoomFederationList, Pairings: []string{"pair_remote"}})
	unboundCtx := bearerClaimsContext(staticClaims("pair_remote", authn.ScopePair))

	member := roomSend("floor", "member")
	member.Origin = protocol.MessageOrigin{NetworkID: "remote", MessageID: "remote_member_unbound"}
	member.From.NetworkID = "remote"
	if _, err := service.SendMessageContext(unboundCtx, member); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("expected unbound pair credential to be refused with ErrAgentForbidden, got %v", err)
	}
}

// newBoundPairTestService builds a Service with require_pair_network_binding
// explicitly enabled -- opt-in only, not the default (F3's revert; see
// config_load.go's defaultConfig doc comment) -- and a single pairing,
// "pair_remote", bound to remote network id "remote".
func newBoundPairTestService(t *testing.T) *Service {
	t.Helper()
	memory := store.NewMemoryStore()
	return NewService(ServiceConfig{
		NetworkID:                 "local",
		NetworkName:               "Local",
		Version:                   "test",
		RequirePairNetworkBinding: true,
		Pairings:                  []protocol.Pairing{{ID: "pair_remote", RemoteNetworkID: "remote"}},
		Store:                     memory,
		Messages:                  memory,
		Broker:                    events.NewBroker(),
	})
}

// boundPairClaims returns a pair-scoped credential bound to "remote" and
// carrying TokenID "pair_remote" -- the shape a join-side pairing (or an
// inviter-side pairing once its peer's real network id is confirmed) mints.
func boundPairClaims() authn.Claims {
	return authn.NewStaticClaims(authn.TokenConfig{
		ID:      "pair_remote",
		Network: "remote",
		Scopes:  []authn.Scope{authn.ScopePair},
	})
}

// mustCreatePolicyRoomWithFederation is mustCreatePolicyRoom plus an explicit
// federation stance, for the pair-credential tests above that need the room
// to actually allow the pairing under test.
func mustCreatePolicyRoomWithFederation(t *testing.T, service *Service, id string, members []string, policy string, federation *protocol.RoomFederation) {
	t.Helper()

	_, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:          id,
		Members:     members,
		WritePolicy: policy,
		Federation:  federation,
	})
	if err != nil {
		t.Fatalf("CreateRoom(%s) error = %v", id, err)
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
