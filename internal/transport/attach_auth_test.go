package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAttachmentEndpointRequiresAttachScope(t *testing.T) {
	t.Parallel()

	policy := mustBearerPolicy(t, authn.TokenConfig{
		ID:     "node",
		Value:  "attach-secret",
		Scopes: []authn.Scope{authn.ScopeAttach},
	})
	server := httptest.NewServer(NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}, policy))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	_, response, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err == nil {
		t.Fatal("expected unauthorized attach dial to fail")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized attach status, got %#v err=%v", response, err)
	}
}

func TestAttachmentEndpointEnforcesAllowedAgentIDs(t *testing.T) {
	t.Parallel()

	policy := mustBearerPolicy(t, authn.TokenConfig{
		ID:     "node",
		Value:  "attach-secret",
		Scopes: []authn.Scope{authn.ScopeAttach},
		Agents: []string{"researcher"},
	})
	server := httptest.NewServer(NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}, policy))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	headers := http.Header{}
	headers.Set("Authorization", "Bearer attach-secret")
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, headers)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: "local",
		Agent:     &protocol.Actor{ID: "writer"},
	}); err != nil {
		t.Fatalf("write identify: %v", err)
	}

	var errorFrame protocol.AttachmentFrame
	if err := connection.ReadJSON(&errorFrame); err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	if errorFrame.Op != protocol.AttachmentOpError {
		t.Fatalf("unexpected frame %#v", errorFrame)
	}
}

func TestAttachmentEndpointRejectsUnexpectedOrigin(t *testing.T) {
	t.Parallel()

	policy := mustBearerPolicy(t, authn.TokenConfig{
		ID:     "node",
		Value:  "attach-secret",
		Scopes: []authn.Scope{authn.ScopeAttach},
	})
	server := httptest.NewServer(NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local"},
		stream:  make(chan protocol.Event),
	}, policy))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	headers := http.Header{}
	headers.Set("Authorization", "Bearer attach-secret")
	headers.Set("Origin", "https://evil.example")

	_, response, err := websocket.DefaultDialer.Dial(endpoint, headers)
	if err == nil {
		t.Fatal("expected origin check failure")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden origin status, got %#v err=%v", response, err)
	}
}

func mustBearerPolicy(t *testing.T, token authn.TokenConfig) *authn.Policy {
	t.Helper()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeBearer,
		ListenAddr: ":8787",
		Tokens:     []authn.TokenConfig{token},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	return policy
}
