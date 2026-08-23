package transport

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// TestJoinRoomScopesAdvertiseWriteOnly is PLAN.md 7A.3's "advertised-scope
// fix" gate: the route used to advertise [write, pair], which was
// misleading — handleJoinRoom's own authorization (join.go's
// joinActorAuthorized) requires a local agent-restricted agent token no
// matter what the route advertises, and a pair-scoped token (minted for a
// peer network) is never agent-restricted, so it could never actually join
// anything. This asserts the route's declared contract directly, the same
// var-literal pattern readScopes/roomListScopes/networkScopes already use.
func TestJoinRoomScopesAdvertiseWriteOnly(t *testing.T) {
	if len(joinRoomScopes) != 1 || joinRoomScopes[0] != authn.ScopeWrite {
		t.Fatalf("joinRoomScopes = %v, want exactly [write] (never pair, which handleJoinRoom can never satisfy)", joinRoomScopes)
	}
}

// TestJoinRoomRefusesPairScopedToken is the behavioral half of the same
// fix: a pair-scoped token — the credential a peer network is handed at
// `pair invite`/`pair <code>` time, never agent-restricted — must never be
// able to join a room, whether or not the route itself would have let the
// request through under the old, broader advertised scope set.
func TestJoinRoomRefusesPairScopedToken(t *testing.T) {
	service := newCredentialHTTPService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:         "research",
		Credential: protocol.NewSecretString(transportRoomCredential),
	}); err != nil {
		t.Fatal(err)
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, Tokens: []authn.TokenConfig{{
		ID:     "friend-net",
		Value:  "friend-net-secret",
		Scopes: []authn.Scope{authn.ScopePair},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPHandler(service, policy)

	request := httptest.NewRequest(http.MethodPost, "/v1/rooms/research/join",
		bytes.NewBufferString(`{"from":{"type":"agent","id":"writer"},"credential":"transport-room-join-secret"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer friend-net-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code == http.StatusOK {
		t.Fatalf("expected a pair-scoped token to be refused, got 200: %s", response.Body.String())
	}
}
