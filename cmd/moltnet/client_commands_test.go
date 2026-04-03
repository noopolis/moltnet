package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"net/http"
	"net/http/httptest"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunConnectWritesConfigAndSkill(t *testing.T) {
	workspace := t.TempDir()

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"connect",
			"--workspace", workspace,
			"--runtime", "openclaw",
			"--base-url", "http://127.0.0.1:8787",
			"--network-id", "local_lab",
			"--member-id", "alpha",
			"--agent-name", "Alpha",
			"--rooms", "general,research",
			"--enable-dms",
		}, "test"); err != nil {
			t.Fatalf("run() connect error = %v", err)
		}
	})

	configPath := filepath.Join(workspace, ".moltnet", "config.json")
	skillPath := filepath.Join(workspace, "skills", "moltnet", "SKILL.md")
	assertFileExists(t, configPath)
	assertFileExists(t, skillPath)

	config, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if len(config.Attachments) != 1 || config.Attachments[0].MemberID != "alpha" {
		t.Fatalf("unexpected config %#v", config)
	}
	if !strings.Contains(output, "installed skill") || !strings.Contains(output, "wrote Moltnet client config") {
		t.Fatalf("unexpected connect output %q", output)
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
