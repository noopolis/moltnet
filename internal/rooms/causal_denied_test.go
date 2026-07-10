package rooms

import (
	"bytes"
	"context"
	"errors"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// newCausalAgentRegistryTestService is newAgentRegistryTestService (used by
// write_policy_test.go) plus an in-memory CausalWriter, so denial tests can
// both exercise real agent registration/authentication and inspect exactly
// which causal JSONL lines a denied send produced.
func newCausalAgentRegistryTestService(t *testing.T) (*Service, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
		CausalWriter:      observability.NewCausalWriter(buf),
	})
	return service, buf
}

func TestCausalPrincipalGrammar(t *testing.T) {
	t.Parallel()

	if got, want := causalPrincipal(context.Background()), "system:moltnet.anonymous"; got != want {
		t.Fatalf("causalPrincipal(no claims) = %q, want %q", got, want)
	}

	agentCtx := authn.WithClaims(context.Background(), authn.NewAgentTokenClaims("scribe", "agent-token:abc123"))
	if got, want := causalPrincipal(agentCtx), "agent:scribe"; got != want {
		t.Fatalf("causalPrincipal(agent claims) = %q, want %q", got, want)
	}

	operatorCtx := authn.WithClaims(context.Background(), staticClaims("ops-writer", authn.ScopeWrite))
	if got, want := causalPrincipal(operatorCtx), "operator:token:ops-writer"; got != want {
		t.Fatalf("causalPrincipal(static claims) = %q, want %q", got, want)
	}
}

// TestStampMessageDeniedNonMemberSendRecordsExactlyOneEvent covers
// acceptance T2/T5: a non-member send to a members-only room must both
// fail with ErrWriteForbidden and produce exactly one message.denied
// causal record, and that record's principal_id must be the authenticated
// agent principal, never a silent drop.
func TestStampMessageDeniedNonMemberSendRecordsExactlyOneEvent(t *testing.T) {
	t.Setenv(causalRunIDEnv, "run_denied_1")
	service, buf := newCausalAgentRegistryTestService(t)
	mustCreatePolicyRoom(t, service, "floor", []string{"member"}, protocol.RoomWritePolicyMembers)
	outsider := registerPolicyAgent(t, service, "outsider")

	request := roomSend("floor", "outsider")
	_, err := service.SendMessageContext(bearerClaimsContext(outsider), request)
	if !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("expected ErrWriteForbidden, got %v", err)
	}

	recorded := causalLines(t, buf)
	if len(recorded) != 1 {
		t.Fatalf("expected exactly 1 causal record for the denied send, got %d: %#v", len(recorded), recorded)
	}
	event := recorded[0]
	if err := event.Validate(); err != nil {
		t.Fatalf("recorded event failed Validate(): %v", err)
	}
	if event.Type != protocol.EventTypeMessageDenied {
		t.Fatalf("Type = %q, want %q", event.Type, protocol.EventTypeMessageDenied)
	}
	if got, want := event.PrincipalID, "agent:outsider"; got != want {
		t.Fatalf("PrincipalID = %q, want %q", got, want)
	}
	// request.From.ID happens to equal "outsider" too in this fixture (the
	// caller's real agent id); the assertion that a *forged* request.From
	// never becomes principal_id lives in
	// TestStampMessageDeniedForgedFromStampsOwnPrincipal below, which uses a
	// From identity that does not match the authenticated caller.
}

// TestStampMessageDeniedForgedFromStampsOwnPrincipal covers the spoof
// requirement in the packet: an authenticated caller whose message body
// (request.From) claims a different identity still gets its OWN
// authenticated principal stamped on the resulting message.denied event,
// never the claimed one.
func TestStampMessageDeniedForgedFromStampsOwnPrincipal(t *testing.T) {
	t.Setenv(causalRunIDEnv, "run_denied_2")
	service, buf := newCausalAgentRegistryTestService(t)
	mustCreatePolicyRoom(t, service, "floor2", []string{"someone"}, protocol.RoomWritePolicyMembers)

	// A static (operator) write token: NewStaticClaims with no Agents
	// restriction allows any actor identity through validateSenderIdentity,
	// so this exercises canWriteRoom's own-membership denial with a forged
	// From actor that was never registered as this credential's agent.
	operatorClaims := staticClaims("ops-writer", authn.ScopeWrite)
	request := roomSend("floor2", "totally-different-claimed-identity")

	_, err := service.SendMessageContext(bearerClaimsContext(operatorClaims), request)
	if !errors.Is(err, ErrWriteForbidden) {
		t.Fatalf("expected ErrWriteForbidden, got %v", err)
	}

	recorded := causalLines(t, buf)
	if len(recorded) != 1 {
		t.Fatalf("expected exactly 1 causal record for the denied send, got %d: %#v", len(recorded), recorded)
	}
	event := recorded[0]
	if got, want := event.PrincipalID, "operator:token:ops-writer"; got != want {
		t.Fatalf("PrincipalID = %q, want %q (authenticated operator claims)", got, want)
	}
	if event.PrincipalID == request.From.ID {
		t.Fatalf("PrincipalID must never equal the forged request.From identity %q", request.From.ID)
	}
}

// TestStampMessageDeniedSkipsNonWritePolicyErrors makes sure the denial
// stamp is scoped precisely to canWriteRoom rejections: an unrelated
// enforceTargetWritePolicy failure (an unknown room) must not produce a
// message.denied record.
func TestStampMessageDeniedSkipsNonWritePolicyErrors(t *testing.T) {
	t.Setenv(causalRunIDEnv, "run_denied_3")
	service, buf := newCausalAgentRegistryTestService(t)

	request := roomSend("no-such-room", "someone")
	if _, err := service.SendMessage(request); !errors.Is(err, ErrUnknownRoom) {
		t.Fatalf("expected ErrUnknownRoom, got %v", err)
	}

	if buf.Len() != 0 {
		t.Fatalf("expected no message.denied record for an unknown-room error, got %q", buf.String())
	}
}
