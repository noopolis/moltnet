package transport

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const transportRoomCredential = "transport-room-join-secret"

func TestRoomCredentialNeverLeaksFromRoomsHTTPOrSSE(t *testing.T) {
	service := newCredentialHTTPService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:          "research",
		Visibility:  protocol.RoomVisibilityPublic,
		Credential:  protocol.NewSecretString(transportRoomCredential),
		WritePolicy: protocol.RoomWritePolicyMembers,
	}); err != nil {
		t.Fatal(err)
	}
	policy, err := authn.NewPolicy(authn.Config{
		Mode: authn.ModeBearer,
		Tokens: []authn.TokenConfig{{
			ID:     "observer",
			Value:  "observer-secret",
			Scopes: []authn.Scope{authn.ScopeObserve},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPHandler(service, policy)
	for _, path := range []string{"/v1/rooms", "/v1/rooms/research"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer observer-secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
		assertCredentialBytesAbsent(t, response.Body.Bytes())
	}

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer observer-secret")
	response := newSignalRecorder(": stream-open")
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(response, request)
		close(done)
	}()
	select {
	case <-response.signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for SSE room event: %q", response.String())
	}
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:         "stream-room",
		Visibility: protocol.RoomVisibilityPublic,
		Credential: protocol.NewSecretString(transportRoomCredential),
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(response.String(), "data: ") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(response.String(), "data: ") {
		t.Fatalf("timed out waiting for SSE room payload: %q", response.String())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out closing SSE stream")
	}
	assertCredentialBytesAbsent(t, []byte(response.String()))
}

func TestJoinRoomEndpointAcceptsWriteScopedAgentToken(t *testing.T) {
	service := newCredentialHTTPService()
	if _, err := service.CreateRoomContext(credentialModeContext(), protocol.CreateRoomRequest{
		ID:         "research",
		Credential: protocol.NewSecretString(transportRoomCredential),
	}); err != nil {
		t.Fatal(err)
	}
	registered, err := service.RegisterAgentContext(
		authn.WithAgentRegistration(context.Background(), authn.AgentRegistrationOpen),
		protocol.RegisterAgentRequest{RequestedAgentID: "writer"},
	)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeBearer, Tokens: []authn.TokenConfig{{
		ID:     "observer",
		Value:  "observer-secret",
		Scopes: []authn.Scope{authn.ScopeObserve},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPHandler(service, policy)
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/rooms/research/join", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous join status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/rooms/research/join", bytes.NewBufferString(`{"from":{"type":"agent","id":"writer"},"credential":"transport-room-join-secret"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+registered.AgentToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("join status=%d body=%s", response.Code, response.Body.String())
	}
	assertCredentialBytesAbsent(t, response.Body.Bytes())
}

func newCredentialHTTPService() *rooms.Service {
	memory := store.NewMemoryStore()
	return rooms.NewService(rooms.ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})
}

func credentialModeContext() context.Context {
	return authn.WithMode(context.Background(), authn.ModeBearer)
}

func assertCredentialBytesAbsent(t *testing.T, payload []byte) {
	t.Helper()
	if bytes.Contains(payload, []byte(transportRoomCredential)) {
		t.Fatalf("credential leaked in serialized bytes %s", payload)
	}
}
