package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
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

func TestOpenAttachmentClaimsAndRequiresAgentToken(t *testing.T) {
	t.Parallel()

	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		NetworkID:   "local",
		NetworkName: "Local",
		Version:     "test",
		Store:       memory,
		Messages:    memory,
		Broker:      events.NewBroker(),
	})
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen, ListenAddr: ":8787"})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	server := httptest.NewServer(NewHTTPHandler(service, policy))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	first := dialIdentifyReadReady(t, endpoint, nil, "local", "luna")
	defer first.connection.Close()
	if !strings.HasPrefix(first.ready.AgentToken, authn.AgentTokenPrefix) {
		t.Fatalf("expected READY agent_token, got %#v", first.ready)
	}

	anonymousAgain := dialIdentifyReadError(t, endpoint, nil, "local", "luna")
	if !strings.Contains(anonymousAgain.Error, "requires its agent token") {
		t.Fatalf("unexpected anonymous existing-id error %#v", anonymousAgain)
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+first.ready.AgentToken)
	reconnect := dialIdentifyReadReady(t, endpoint, headers, "local", "luna")
	defer reconnect.connection.Close()
	if reconnect.ready.AgentToken != "" {
		t.Fatalf("reconnect should not return token again %#v", reconnect.ready)
	}

	mismatch := dialIdentifyReadError(t, endpoint, headers, "local", "atlas")
	if !strings.Contains(mismatch.Error, "not allowed") {
		t.Fatalf("unexpected mismatched token error %#v", mismatch)
	}
}

func TestOpenAgentTokenAttachmentFiltersUnrelatedPrivateAndAdminEvents(t *testing.T) {
	t.Parallel()

	token, err := authn.GenerateAgentToken()
	if err != nil {
		t.Fatalf("GenerateAgentToken() error = %v", err)
	}
	stream := make(chan protocol.Event, 5)
	service := &agentTokenAttachmentService{
		fakeService: &fakeService{
			network: protocol.Network{ID: "local"},
			stream:  stream,
		},
		token:  token,
		claims: authn.NewAgentTokenClaims("luna", authn.AgentTokenCredentialKey(token)),
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen, ListenAddr: ":8787"})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	server := httptest.NewServer(NewHTTPHandler(service, policy))
	defer server.Close()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	attachment := dialIdentifyReadReady(t, endpoint, headers, "local", "luna")
	defer attachment.connection.Close()

	stream <- protocol.Event{
		ID:   "evt_dm_unrelated",
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			ID: "msg_dm_unrelated",
			Target: protocol.Target{
				Kind:           protocol.TargetKindDM,
				DMID:           "dm_private",
				ParticipantIDs: []string{"atlas", "nova"},
			},
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "private dm"}},
		},
	}
	stream <- protocol.Event{
		ID:      "evt_pair",
		Type:    protocol.EventTypePairingUpdated,
		Pairing: &protocol.Pairing{ID: "pair_private"},
	}
	stream <- protocol.Event{
		ID:   "evt_members",
		Type: protocol.EventTypeRoomMembersUpdated,
		Room: &protocol.Room{ID: "agora", Members: []string{"luna", "atlas"}},
	}
	stream <- protocol.Event{
		ID:   "evt_room",
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			ID:     "msg_room",
			Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "agora"},
			Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "public room"}},
		},
	}
	stream <- protocol.Event{
		ID:   "evt_dm_luna",
		Type: protocol.EventTypeMessageCreated,
		Message: &protocol.Message{
			ID: "msg_dm_luna",
			Target: protocol.Target{
				Kind:           protocol.TargetKindDM,
				DMID:           "dm_luna",
				ParticipantIDs: []string{"luna", "atlas"},
			},
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "luna dm"}},
		},
	}
	close(stream)

	ids := strings.Join(readAttachmentEventIDsUntilClose(t, attachment.connection), ",")
	if ids != "evt_room,evt_dm_luna" {
		t.Fatalf("unexpected attachment event ids %q", ids)
	}
	for _, blocked := range []string{"evt_dm_unrelated", "evt_pair", "evt_members"} {
		if strings.Contains(ids, blocked) {
			t.Fatalf("attachment leaked %q in ids %q", blocked, ids)
		}
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

type readyAttachment struct {
	connection *websocket.Conn
	ready      protocol.AttachmentFrame
}

type agentTokenAttachmentService struct {
	*fakeService
	token  string
	claims authn.Claims
}

func (s *agentTokenAttachmentService) AuthenticateAgentTokenContext(
	_ context.Context,
	stringToken string,
) (authn.Claims, bool, error) {
	if stringToken != s.token {
		return authn.Claims{}, false, nil
	}
	return s.claims, true, nil
}

func dialIdentifyReadReady(
	t *testing.T,
	endpoint string,
	headers http.Header,
	networkID string,
	agentID string,
) readyAttachment {
	t.Helper()

	connection := dialAttachmentForAuthTest(t, endpoint, headers, networkID, agentID)
	var ready protocol.AttachmentFrame
	if err := connection.ReadJSON(&ready); err != nil {
		connection.Close()
		t.Fatalf("read ready: %v", err)
	}
	if ready.Op != protocol.AttachmentOpReady {
		connection.Close()
		t.Fatalf("unexpected ready frame %#v", ready)
	}
	return readyAttachment{connection: connection, ready: ready}
}

func dialIdentifyReadError(
	t *testing.T,
	endpoint string,
	headers http.Header,
	networkID string,
	agentID string,
) protocol.AttachmentFrame {
	t.Helper()

	connection := dialAttachmentForAuthTest(t, endpoint, headers, networkID, agentID)
	defer connection.Close()
	var errorFrame protocol.AttachmentFrame
	if err := connection.ReadJSON(&errorFrame); err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	if errorFrame.Op != protocol.AttachmentOpError {
		t.Fatalf("unexpected error frame %#v", errorFrame)
	}
	return errorFrame
}

func dialAttachmentForAuthTest(
	t *testing.T,
	endpoint string,
	headers http.Header,
	networkID string,
	agentID string,
) *websocket.Conn {
	t.Helper()

	connection, _, err := websocket.DefaultDialer.Dial(endpoint, headers)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	var hello protocol.AttachmentFrame
	if err := connection.ReadJSON(&hello); err != nil {
		connection.Close()
		t.Fatalf("read hello: %v", err)
	}
	if err := connection.WriteJSON(protocol.AttachmentFrame{
		Op:        protocol.AttachmentOpIdentify,
		Version:   protocol.AttachmentProtocolV1,
		NetworkID: networkID,
		Agent:     &protocol.Actor{ID: agentID},
	}); err != nil {
		connection.Close()
		t.Fatalf("write identify: %v", err)
	}
	return connection
}

func readAttachmentEventIDsUntilClose(t *testing.T, connection *websocket.Conn) []string {
	t.Helper()

	var ids []string
	for {
		if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		var frame protocol.AttachmentFrame
		if err := connection.ReadJSON(&frame); err != nil {
			return ids
		}
		switch frame.Op {
		case protocol.AttachmentOpEvent:
			if frame.Event == nil {
				t.Fatalf("event frame missing event %#v", frame)
			}
			ids = append(ids, frame.Event.ID)
		case protocol.AttachmentOpPing:
			continue
		case protocol.AttachmentOpError:
			t.Fatalf("unexpected error frame %#v", frame)
		}
	}
}
