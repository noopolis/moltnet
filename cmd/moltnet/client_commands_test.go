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

func TestRunRemoveAgentAndRoomUseAdminToken(t *testing.T) {
	var requests []string
	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		authHeaders = append(authHeaders, request.Header.Get("Authorization"))
		switch request.URL.Path {
		case "/v1/agents/stale-agent":
			_ = json.NewEncoder(response).Encode(protocol.RemoveResult{
				Removed: true,
				Kind:    "agent",
				ID:      "stale-agent",
				Mode:    "soft",
			})
		case "/v1/rooms/stale-room":
			_ = json.NewEncoder(response).Encode(protocol.RemoveResult{
				Removed: true,
				Kind:    "room",
				ID:      "stale-room",
				Mode:    "soft",
			})
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("MOLTNET_ADMIN_TOKEN", "admin-secret")
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"remove-agent",
			"--base-url", server.URL,
			"--agent", "stale-agent",
			"--token-env", "MOLTNET_ADMIN_TOKEN",
		}, "test"); err != nil {
			t.Fatalf("run() remove-agent error = %v", err)
		}
	})
	if !strings.Contains(output, `"kind": "agent"`) {
		t.Fatalf("unexpected remove-agent output %q", output)
	}

	output = captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"remove-room",
			"--base-url", server.URL,
			"--room", "stale-room",
			"--token-env", "MOLTNET_ADMIN_TOKEN",
		}, "test"); err != nil {
			t.Fatalf("run() remove-room error = %v", err)
		}
	})
	if !strings.Contains(output, `"kind": "room"`) {
		t.Fatalf("unexpected remove-room output %q", output)
	}
	if len(requests) != 2 ||
		requests[0] != "DELETE /v1/agents/stale-agent" ||
		requests[1] != "DELETE /v1/rooms/stale-room" {
		t.Fatalf("unexpected requests %#v", requests)
	}
	if authHeaders[0] != "Bearer admin-secret" || authHeaders[1] != "Bearer admin-secret" {
		t.Fatalf("unexpected auth headers %#v", authHeaders)
	}
}

func TestRunSendPostsRoomMessage(t *testing.T) {
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
