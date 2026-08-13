package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestApplyConfigSendsRoomCredentialPlaintextOnWire(t *testing.T) {
	const credential = "outbound-room-credential"
	var body []byte
	client, err := New(clientconfig.AttachmentConfig{
		Auth:      clientconfig.AuthConfig{Mode: "none"},
		BaseURL:   "http://moltnet.test",
		MemberID:  "alpha",
		NetworkID: "local",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	client.httpClient = &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/apply" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		var readErr error
		body, readErr = io.ReadAll(request.Body)
		if readErr != nil {
			t.Fatalf("read request body: %v", readErr)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"applied":true}`)),
			Header:     make(http.Header),
		}, nil
	})}
	if _, err := client.ApplyConfig(context.Background(), protocol.ApplyConfigRequest{Rooms: []protocol.CreateRoomRequest{{
		ID:         "research",
		Credential: protocol.NewSecretString(credential),
	}}}); err != nil {
		t.Fatalf("ApplyConfig() error = %v", err)
	}
	if !bytes.Contains(body, []byte(`"credential":"`+credential+`"`)) {
		t.Fatalf("request body did not contain plaintext room credential: %s", body)
	}
	if bytes.Contains(body, []byte(`"credential":"[REDACTED]"`)) {
		t.Fatalf("request body sent redacted room credential: %s", body)
	}
}

func TestSendMessage(t *testing.T) {
	t.Parallel()

	var received protocol.SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(response).Encode(protocol.MessageAccepted{
			Accepted:  true,
			EventID:   "evt_1",
			MessageID: "msg_1",
		})
	}))
	defer server.Close()

	client, err := New(clientconfig.AttachmentConfig{
		Auth:      clientconfig.AuthConfig{Mode: "none"},
		BaseURL:   server.URL,
		MemberID:  "alpha",
		NetworkID: "local",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	accepted, err := client.SendMessage(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
		From:   protocol.Actor{Type: "agent", ID: "alpha", NetworkID: "local"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if !accepted.Accepted || received.Target.RoomID != "general" {
		t.Fatalf("unexpected send accepted=%#v received=%#v", accepted, received)
	}
}

func TestListRoomMessages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/rooms/general/messages" || request.URL.RawQuery != "limit=5" {
			t.Fatalf("unexpected path %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		_ = json.NewEncoder(response).Encode(protocol.MessagePage{
			Messages: []protocol.Message{{ID: "msg_1"}},
		})
	}))
	defer server.Close()

	client, err := New(clientconfig.AttachmentConfig{
		Auth:      clientconfig.AuthConfig{Mode: "none"},
		BaseURL:   server.URL,
		MemberID:  "alpha",
		NetworkID: "local",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	page, err := client.ListRoomMessages(context.Background(), "general", protocol.PageRequest{Limit: 5})
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "msg_1" {
		t.Fatalf("unexpected page %#v", page)
	}
}

func TestListRoomMessagesPreservesLargeJSONNumber(t *testing.T) {
	t.Parallel()
	const large = "900719925474099312345678901234567890"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/rooms/general/messages" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		_, _ = fmt.Fprintf(response, `{"messages":[{"id":"msg_1","network_id":"local","target":{"kind":"room","room_id":"general"},"from":{"type":"agent","id":"alpha"},"parts":[{"kind":"text","data":{"integer":%s}}],"created_at":"2026-07-22T10:00:00Z"}]}`, large)
	}))
	defer server.Close()

	client, err := New(clientconfig.AttachmentConfig{Auth: clientconfig.AuthConfig{Mode: "none"}, BaseURL: server.URL, MemberID: "alpha", NetworkID: "local"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	page, err := client.ListRoomMessages(context.Background(), "general", protocol.PageRequest{})
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	got, ok := page.Messages[0].Parts[0].Data["integer"].(json.Number)
	if !ok || got.String() != large {
		t.Fatalf("large integer changed: %#v", page.Messages[0].Parts[0].Data)
	}
}

func TestRegisterAgentUsesOpenToken(t *testing.T) {
	t.Parallel()

	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authHeader = request.Header.Get("Authorization")
		_ = json.NewEncoder(response).Encode(protocol.AgentRegistration{
			NetworkID: "local",
			AgentID:   "alpha",
			ActorUID:  "actor_1",
			ActorURI:  protocol.AgentFQID("local", "alpha"),
		})
	}))
	defer server.Close()

	client, err := New(clientconfig.AttachmentConfig{
		Auth:      clientconfig.AuthConfig{Mode: "open", Token: "magt_v1_alpha"},
		BaseURL:   server.URL,
		MemberID:  "alpha",
		NetworkID: "local",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := client.RegisterAgent(context.Background(), protocol.RegisterAgentRequest{RequestedAgentID: "alpha"}); err != nil {
		t.Fatalf("RegisterAgent() error = %v", err)
	}
	if authHeader != "Bearer magt_v1_alpha" {
		t.Fatalf("unexpected auth header %q", authHeader)
	}
}
