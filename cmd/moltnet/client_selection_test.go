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

func TestRunSendRequiresMemberForSameNetworkAttachments(t *testing.T) {
	workspace := writeSameNetworkClientWorkspace(t)
	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		called = true
		_ = json.NewEncoder(response).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer server.Close()
	rewriteAllClientConfigBaseURLs(t, workspace, server.URL)

	err := run(context.Background(), []string{
		"send",
		"--network", "local",
		"--target", "room:general",
		"--text", "hello",
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "--member") {
		t.Fatalf("expected member selector error, got %v", err)
	}
	if called {
		t.Fatal("server was contacted despite ambiguous member selection")
	}
}

func TestRunSendUsesSelectedMemberCredentials(t *testing.T) {
	workspace := writeSameNetworkClientWorkspace(t)
	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var authHeader string
	var received protocol.SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authHeader = request.Header.Get("Authorization")
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(response).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer server.Close()
	rewriteAllClientConfigBaseURLs(t, workspace, server.URL)

	if err := run(context.Background(), []string{
		"send",
		"--network", "local",
		"--member", "beta",
		"--target", "room:general",
		"--text", "hello",
	}, "test"); err != nil {
		t.Fatalf("run send: %v", err)
	}
	if authHeader != "Bearer beta-token" {
		t.Fatalf("unexpected auth header %q", authHeader)
	}
	if received.From.ID != "beta" {
		t.Fatalf("unexpected sender %#v", received.From)
	}
}

func TestClientReadCommandsRequireMemberForSameNetworkAttachments(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{
			name: "read",
			args: []string{"read", "--network", "local", "--target", "room:general"},
		},
		{
			name: "conversations",
			args: []string{"conversations", "--network", "local"},
		},
		{
			name: "participants",
			args: []string{"participants", "--network", "local", "--target", "room:general"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := writeSameNetworkClientWorkspace(t)
			cwd := mustGetwd(t)
			defer func() { _ = os.Chdir(cwd) }()
			if err := os.Chdir(workspace); err != nil {
				t.Fatalf("chdir: %v", err)
			}

			called := false
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				called = true
				_ = json.NewEncoder(response).Encode(map[string]any{})
			}))
			defer server.Close()
			rewriteAllClientConfigBaseURLs(t, workspace, server.URL)

			err := run(context.Background(), test.args, "test")
			if err == nil || !strings.Contains(err.Error(), "--member") {
				t.Fatalf("expected member selector error, got %v", err)
			}
			if called {
				t.Fatal("server was contacted despite ambiguous member selection")
			}
		})
	}
}

func TestClientReadCommandsUseSelectedMemberCredentials(t *testing.T) {
	for _, test := range []struct {
		name     string
		args     []string
		response any
	}{
		{
			name: "read",
			args: []string{"read", "--network", "local", "--member", "beta", "--target", "room:general"},
			response: protocol.MessagePage{
				Messages: []protocol.Message{{
					ID: "msg_1",
					From: protocol.Actor{
						Type:      "agent",
						ID:        "alpha",
						NetworkID: "local",
					},
					Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
				}},
			},
		},
		{
			name: "conversations",
			args: []string{"conversations", "--network", "local", "--member", "beta"},
			response: protocol.RoomPage{
				Rooms: []protocol.Room{{ID: "general", NetworkID: "local"}},
			},
		},
		{
			name: "participants",
			args: []string{"participants", "--network", "local", "--member", "beta", "--target", "room:general"},
			response: protocol.Room{
				ID:        "general",
				NetworkID: "local",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := writeSameNetworkClientWorkspace(t)
			cwd := mustGetwd(t)
			defer func() { _ = os.Chdir(cwd) }()
			if err := os.Chdir(workspace); err != nil {
				t.Fatalf("chdir: %v", err)
			}

			var authHeader string
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				authHeader = request.Header.Get("Authorization")
				_ = json.NewEncoder(response).Encode(test.response)
			}))
			defer server.Close()
			rewriteAllClientConfigBaseURLs(t, workspace, server.URL)

			if err := run(context.Background(), test.args, "test"); err != nil {
				t.Fatalf("run %s: %v", test.name, err)
			}
			if authHeader != "Bearer beta-token" {
				t.Fatalf("unexpected auth header %q", authHeader)
			}
		})
	}
}

func writeSameNetworkClientWorkspace(t *testing.T) string {
	t.Helper()

	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: "moltnet.client.v1",
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "open", Token: "alpha-token"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
			{
				Auth:      clientconfig.AuthConfig{Mode: "open", Token: "beta-token"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "beta",
				NetworkID: "local",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})
	return workspace
}

func rewriteAllClientConfigBaseURLs(t *testing.T, workspace string, baseURL string) {
	t.Helper()

	path := filepath.Join(workspace, ".moltnet", "config.json")
	config, err := clientconfig.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	for index := range config.Attachments {
		config.Attachments[index].BaseURL = baseURL
	}
	if err := writeClientConfig(path, config); err != nil {
		t.Fatalf("writeClientConfig() error = %v", err)
	}
}
