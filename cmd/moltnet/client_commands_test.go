package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunRegisterAgentWritesIdentity(t *testing.T) {
	workspace := t.TempDir()
	var received protocol.RegisterAgentRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/agents/register" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(response).Encode(protocol.AgentRegistration{
			NetworkID:   "local",
			AgentID:     "director",
			ActorUID:    "actor_1",
			ActorURI:    protocol.AgentFQID("local", "director"),
			DisplayName: "Director",
		})
	}))
	defer server.Close()

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"register-agent",
			"--base-url", server.URL,
			"--agent", "director",
			"--name", "Director",
			"--workspace", workspace,
		}, "test"); err != nil {
			t.Fatalf("run() register-agent error = %v", err)
		}
	})

	if received.RequestedAgentID != "director" || received.Name != "Director" {
		t.Fatalf("unexpected register request %#v", received)
	}
	if !strings.Contains(output, `"actor_uri": "molt://local/agents/director"`) {
		t.Fatalf("unexpected register output %q", output)
	}

	identityBytes, err := os.ReadFile(filepath.Join(workspace, ".moltnet", "identity.json"))
	if err != nil {
		t.Fatalf("read identity: %v", err)
	}
	if !strings.Contains(string(identityBytes), `"actor_uri": "molt://local/agents/director"`) {
		t.Fatalf("unexpected identity file %s", identityBytes)
	}
}

func TestRunSendPostsRoomMessage(t *testing.T) {
	t.Setenv(bridgeutil.DaimonWakeIDEnv, "moltnet:msg_wake_1")
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: "moltnet.client.v1",
		Agent:   clientconfig.AgentConfig{Name: "Alpha", Runtime: "openclaw"},
		Attachments: []clientconfig.AttachmentConfig{
			{
				AgentName: "Alpha",
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	var received protocol.SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(response).Encode(protocol.MessageAccepted{
			Accepted:  true,
			EventID:   "evt_1",
			MessageID: "msg_1",
		})
	}))
	defer server.Close()

	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"send",
			"--target", "room:general",
			"--text", "hello world",
		}, "test"); err != nil {
			t.Fatalf("run() send error = %v", err)
		}
	})

	if received.Target.RoomID != "general" || received.From.ID != "alpha" || received.Parts[0].Text != "hello world" {
		t.Fatalf("unexpected send request %#v", received)
	}
	wantID := bridgeutil.DeliveryReplyMessageID("alpha", "moltnet:msg_wake_1", received.Target)
	if received.ID != wantID || len(received.CauseEventIDs) != 1 || received.CauseEventIDs[0] != "moltnet:msg_wake_1" {
		t.Fatalf("send request did not claim the Daimon delivery publication slot: %#v", received)
	}
	if !strings.Contains(output, `"accepted": true`) {
		t.Fatalf("unexpected send output %q", output)
	}
}

func TestRunSendFailsLocallyForReadOnlyRoom(t *testing.T) {
	workspace := t.TempDir()
	canWrite := false
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: "moltnet.client.v1",
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "guest",
				NetworkID: "public",
				Rooms: []bridgeconfig.RoomBinding{
					{
						ID:          "episode-floor",
						Visibility:  "public",
						WritePolicy: "members",
						CanWrite:    &canWrite,
					},
				},
			},
		},
	})

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	err := run(context.Background(), []string{
		"send",
		"--target", "room:episode-floor",
		"--text", "hello",
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only error, got %v", err)
	}
}

func TestRunReadRoomMessages(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: "moltnet.client.v1",
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/rooms/general/messages" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		_ = json.NewEncoder(response).Encode(protocol.MessagePage{
			Messages: []protocol.Message{{ID: "msg_1"}},
		})
	}))
	defer server.Close()

	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"read",
			"--target", "room:general",
			"--limit", "5",
		}, "test"); err != nil {
			t.Fatalf("run() read error = %v", err)
		}
	})

	if !strings.Contains(output, `"msg_1"`) {
		t.Fatalf("unexpected read output %q", output)
	}
}

// TestRunReadThreadMessages is the PLAN 7C.3 happy-path integration test for
// `read --target thread:<id>`: the CLI resolves the thread's room via
// GetThread, finds it in the attached rooms list, and lists the thread's
// messages via ListThreadMessages.
func TestRunReadThreadMessages(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: "moltnet.client.v1",
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/threads/thr_1":
			_ = json.NewEncoder(response).Encode(protocol.Thread{ID: "thr_1", RoomID: "general"})
		case "/v1/threads/thr_1/messages":
			_ = json.NewEncoder(response).Encode(protocol.MessagePage{
				Messages: []protocol.Message{{ID: "msg_1"}},
			})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()

	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"read",
			"--target", "thread:thr_1",
		}, "test"); err != nil {
			t.Fatalf("run() read error = %v", err)
		}
	})

	if !strings.Contains(output, `"msg_1"`) {
		t.Fatalf("unexpected read output %q", output)
	}
}

// TestRunReadThreadRefusedForUnattachedRoom is the PLAN 7C.3 authorization
// test: a thread whose room ("secret-room") is not in the caller's attached
// rooms list must be refused locally, without ever reaching
// ListThreadMessages.
func TestRunReadThreadRefusedForUnattachedRoom(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: "moltnet.client.v1",
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/threads/thr_secret":
			_ = json.NewEncoder(response).Encode(protocol.Thread{ID: "thr_secret", RoomID: "secret-room"})
		default:
			t.Fatalf("unexpected path %s: local authorization should have refused before any other request", request.URL.Path)
		}
	}))
	defer server.Close()

	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	err := run(context.Background(), []string{
		"read",
		"--target", "thread:thr_secret",
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("expected a \"not attached\" refusal, got %v", err)
	}
}

// TestRunSendThreadMessage is the PLAN 7C.3 happy-path integration test for
// `send --target thread:<id>`: the wire SendMessageRequest.Target must carry
// Kind=thread, the thread id, and the room id resolved via GetThread —
// protocol.ValidateTarget requires room_id for a thread target server-side.
func TestRunSendThreadMessage(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: "moltnet.client.v1",
		Agent:   clientconfig.AgentConfig{Name: "Alpha", Runtime: "openclaw"},
		Attachments: []clientconfig.AttachmentConfig{
			{
				AgentName: "Alpha",
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	var received protocol.SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/v1/threads/thr_1":
			_ = json.NewEncoder(response).Encode(protocol.Thread{ID: "thr_1", RoomID: "general"})
		case request.Method == http.MethodPost && request.URL.Path == "/v1/messages":
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_ = json.NewEncoder(response).Encode(protocol.MessageAccepted{Accepted: true, EventID: "evt_1", MessageID: "msg_1"})
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"send",
			"--target", "thread:thr_1",
			"--text", "hello thread",
		}, "test"); err != nil {
			t.Fatalf("run() send error = %v", err)
		}
	})

	if received.Target.Kind != protocol.TargetKindThread || received.Target.ThreadID != "thr_1" || received.Target.RoomID != "general" {
		t.Fatalf("unexpected send request target %#v", received.Target)
	}
	if !strings.Contains(output, `"accepted": true`) {
		t.Fatalf("unexpected send output %q", output)
	}
}

// TestRunSendThreadRefusedForUnattachedRoom mirrors
// TestRunReadThreadRefusedForUnattachedRoom for the write path: a thread
// resolving into a room the caller has no local attachment for must be
// refused before any /v1/messages POST is ever made.
func TestRunSendThreadRefusedForUnattachedRoom(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: "moltnet.client.v1",
		Agent:   clientconfig.AgentConfig{Name: "Alpha", Runtime: "openclaw"},
		Attachments: []clientconfig.AttachmentConfig{
			{
				AgentName: "Alpha",
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/threads/thr_secret":
			_ = json.NewEncoder(response).Encode(protocol.Thread{ID: "thr_secret", RoomID: "secret-room"})
		default:
			t.Fatalf("unexpected request %s %s: local authorization should have refused before any other request", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	err := run(context.Background(), []string{
		"send",
		"--target", "thread:thr_secret",
		"--text", "hello",
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("expected a \"not attached\" refusal, got %v", err)
	}
}

// TestRunParticipantsThreadReturnsParentRoom is the PLAN 7C.3 deliberate
// choice for `participants --target thread:<id>`: threads have no
// membership of their own, so this resolves the thread's room and returns
// that room's participants, rather than refusing the target outright (see
// runParticipants' TargetKindThread case doc comment).
func TestRunParticipantsThreadReturnsParentRoom(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: "moltnet.client.v1",
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/threads/thr_1":
			_ = json.NewEncoder(response).Encode(protocol.Thread{ID: "thr_1", RoomID: "general"})
		case "/v1/rooms/general":
			_ = json.NewEncoder(response).Encode(protocol.Room{ID: "general", Members: []string{"alpha", "beta"}})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()

	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"participants",
			"--target", "thread:thr_1",
		}, "test"); err != nil {
			t.Fatalf("run() participants error = %v", err)
		}
	})

	if !strings.Contains(output, `"general"`) || !strings.Contains(output, `"beta"`) {
		t.Fatalf("unexpected participants output %q", output)
	}
}

func TestRunConversationsFiltersToAttachedRooms(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: "moltnet.client.v1",
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/rooms":
			_ = json.NewEncoder(response).Encode(protocol.RoomPage{
				Rooms: []protocol.Room{
					{ID: "general"},
					{ID: "random"},
				},
			})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()

	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"conversations"}, "test"); err != nil {
			t.Fatalf("run() conversations error = %v", err)
		}
	})

	if !strings.Contains(output, `"general"`) || strings.Contains(output, `"random"`) {
		t.Fatalf("unexpected conversations output %q", output)
	}
}

func writeClientConfigFixture(t *testing.T, workspace string, config clientconfig.Config) {
	t.Helper()

	path := filepath.Join(workspace, ".moltnet", "config.json")
	if err := writeClientConfig(path, config); err != nil {
		t.Fatalf("writeClientConfig() error = %v", err)
	}
}

func rewriteClientConfigBaseURL(t *testing.T, workspace string, baseURL string) {
	t.Helper()

	path := filepath.Join(workspace, ".moltnet", "config.json")
	config, err := clientconfig.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	config.Attachments[0].BaseURL = baseURL
	if err := writeClientConfig(path, config); err != nil {
		t.Fatalf("writeClientConfig() error = %v", err)
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return cwd
}
