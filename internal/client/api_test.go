package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// TestGetThread covers the PLAN 7C.3 client leg: GetThread must hit GET
// /v1/threads/{threadID} and decode the thread's RoomID, since that field is
// exactly what cmd/moltnet's resolveThreadTarget needs to authorize (and,
// for send, to fill in) a thread:<id> CLI target.
func TestGetThread(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/threads/thr_1" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		_ = json.NewEncoder(response).Encode(protocol.Thread{ID: "thr_1", RoomID: "general"})
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

	thread, err := client.GetThread(context.Background(), "thr_1")
	if err != nil {
		t.Fatalf("GetThread() error = %v", err)
	}
	if thread.ID != "thr_1" || thread.RoomID != "general" {
		t.Fatalf("unexpected thread %#v", thread)
	}
}

// TestListThreadMessages mirrors TestListRoomMessages for the thread
// endpoint (GET /v1/threads/{threadID}/messages), including the page-limit
// query parameter.
func TestListThreadMessages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/threads/thr_1/messages" || request.URL.RawQuery != "limit=5" {
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

	page, err := client.ListThreadMessages(context.Background(), "thr_1", protocol.PageRequest{Limit: 5})
	if err != nil {
		t.Fatalf("ListThreadMessages() error = %v", err)
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

// TestNewLoopbackOnlyRefusesCrossHostRedirect is the P2 fix for the false
// "never leaves the machine" claim: the operator fallback (moltnetclient.
// NewLoopbackOnly, used by cmd/moltnet's resolveOperatorClient) dials only a
// loopback address it derived itself, but the standard library's default
// redirect policy still follows a same-process 307/308 to any other host —
// Go strips the Authorization header cross-host, but the request body (a
// message's text, for a send) still goes wherever the redirect points. This
// proves NewLoopbackOnly's client refuses that hop instead of silently
// completing it.
func TestNewLoopbackOnlyRefusesCrossHostRedirect(t *testing.T) {
	t.Parallel()

	var offHostHits int32
	offHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&offHostHits, 1)
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer offHost.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, offHost.URL+"/v1/messages", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	client, err := NewLoopbackOnly(clientconfig.AttachmentConfig{
		Auth:      clientconfig.AuthConfig{Mode: "none"},
		BaseURL:   origin.URL,
		MemberID:  "operator",
		NetworkID: "local",
	})
	if err != nil {
		t.Fatalf("NewLoopbackOnly() error = %v", err)
	}

	_, err = client.SendMessage(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
		From:   protocol.Actor{Type: "human", ID: "operator"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hola"}},
	})
	if err == nil {
		t.Fatal("expected SendMessage to refuse the cross-host redirect")
	}
	if !strings.Contains(err.Error(), "different host") {
		t.Fatalf("expected the error to explain the cross-host refusal, got %v", err)
	}
	if atomic.LoadInt32(&offHostHits) != 0 {
		t.Fatal("request body must never reach the off-host redirect target")
	}
}

// TestNewLoopbackOnlyStillFollowsSameHostRedirect confirms the fix is
// specific to cross-host hops, not a blanket "never follow any redirect":
// a same-host 307 (e.g. a trailing-slash normalization) still completes.
func TestNewLoopbackOnlyStillFollowsSameHostRedirect(t *testing.T) {
	t.Parallel()

	var finalPath string
	var mux http.ServeMux
	server := httptest.NewServer(&mux)
	defer server.Close()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/v1/messages/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/v1/messages/", func(w http.ResponseWriter, r *http.Request) {
		finalPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true})
	})

	client, err := NewLoopbackOnly(clientconfig.AttachmentConfig{
		Auth:      clientconfig.AuthConfig{Mode: "none"},
		BaseURL:   server.URL,
		MemberID:  "operator",
		NetworkID: "local",
	})
	if err != nil {
		t.Fatalf("NewLoopbackOnly() error = %v", err)
	}

	if _, err := client.SendMessage(context.Background(), protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
		From:   protocol.Actor{Type: "human", ID: "operator"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hola"}},
	}); err != nil {
		t.Fatalf("SendMessage() error = %v, want the same-host redirect to be followed", err)
	}
	if finalPath != "/v1/messages/" {
		t.Fatalf("unexpected final path %q", finalPath)
	}
}
