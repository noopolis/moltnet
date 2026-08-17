package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// pairingsRoutes are the four surfaces authorizedOperatorOrObserver (auth.go)
// gates: remote network ids, names, and base URLs, which a self-registered
// agent should never be able to enumerate even though NewAgentTokenClaims
// now grants `observe` (P2-2) so that agent can read its own rooms.
var pairingsRoutes = []string{
	"/v1/pairings",
	"/v1/pairings/pair-1/network",
	"/v1/pairings/pair-1/rooms",
	"/v1/pairings/pair-1/agents",
}

// TestPairingsRoutesRejectAgentTokens is the P2-1 regression test (PLAN.md
// phase 6c review): before this fix, every /v1/pairings* route was gated on
// readScopes alone, and P2-2 (same review) added `observe` to
// NewAgentTokenClaims — so a self-registered agent's own token, without
// admin or pair, could list every remote network this operator is paired
// with (ids, names, base URLs). Tokens themselves stay redacted (no
// credential leak), but it is real topology disclosure the stated fix
// criterion forbids. This drives a real self-registered agent token against
// the genuine rooms.Service (rather than a hand-built Claims value), so the
// exclusion is proven against production credential issuance, not the
// transport package's own idea of what an agent token looks like.
// authorizedOperatorOrObserver rejects agent tokens before the handler ever
// calls the service, so no pairing needs to actually exist for this half of
// the test.
func TestPairingsRoutesRejectAgentTokens(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "public",
		NetworkName:       "Public",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})

	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	handler := NewHTTPHandler(service, policy)

	response := httptest.NewRecorder()
	registerRequest := httptest.NewRequest(http.MethodPost, "/v1/agents/register", strings.NewReader(`{"requested_agent_id":"guest"}`))
	registerRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, registerRequest)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected agent registration to succeed, got %d: %s", response.Code, response.Body.String())
	}
	var registration protocol.AgentRegistration
	if err := json.Unmarshal(response.Body.Bytes(), &registration); err != nil {
		t.Fatalf("decode registration response: %v", err)
	}
	if registration.AgentToken == "" {
		t.Fatalf("expected a generated agent_token in the registration response: %s", response.Body.String())
	}

	for _, path := range pairingsRoutes {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Authorization", "Bearer "+registration.AgentToken)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("expected agent token to get 403 from %s, got %d: %s", path, response.Code, response.Body.String())
			}
		})
	}
}

// TestPairingsRoutesAllowConsoleObserveToken is the other half of the P2-1
// fix criterion: the exclusion above must not also catch the console's own
// session credential, an ordinary static token scoped to exactly [observe]
// (cmd/moltnet's consoleTokenScopes) with no agent restriction at all — the
// PAIRINGS sidebar tab (web/src/hooks/usePairings.ts, PairingView.tsx) calls
// all four of these routes and must keep working. This uses fakeService,
// like the rest of this package's scope tests, since it succeeds for any
// pairingID regardless of whether a pairing actually exists — the point
// here is purely the authorization decision, not pairing lookup behavior.
func TestPairingsRoutesAllowConsoleObserveToken(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode: authn.ModeBearer,
		Tokens: []authn.TokenConfig{
			{ID: "console", Value: "console-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local", Name: "Local"},
		pairings: []protocol.Pairing{
			{ID: "pair-1", RemoteNetworkID: "remote"},
		},
	}, policy)

	for _, path := range pairingsRoutes {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Authorization", "Bearer console-secret")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("expected the console's observe-only token to get 200 from %s, got %d: %s", path, response.Code, response.Body.String())
			}
		})
	}
}
