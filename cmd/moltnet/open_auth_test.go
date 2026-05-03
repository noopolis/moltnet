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

	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunRegisterAgentOpenWritesReturnedToken(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, ".moltnet", "config.json")
	if err := writeClientConfig(configPath, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "open"},
				BaseURL:   "http://placeholder",
				MemberID:  "alpha",
				NetworkID: "local",
			},
		},
	}); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authHeader = request.Header.Get("Authorization")
		_ = json.NewEncoder(response).Encode(protocol.AgentRegistration{
			NetworkID:   "local",
			AgentID:     "alpha",
			ActorUID:    "actor_1",
			ActorURI:    protocol.AgentFQID("local", "alpha"),
			AgentToken:  "magt_v1_alpha",
			DisplayName: "Alpha",
		})
	}))
	defer server.Close()

	config, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	config.Attachments[0].BaseURL = server.URL
	if err := writeClientConfig(configPath, config); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"register-agent",
			"--config", configPath,
			"--network", "local",
			"--workspace", workspace,
		}, "test")
		if err != nil {
			t.Fatalf("run register-agent: %v", err)
		}
	})
	if authHeader != "" {
		t.Fatalf("expected anonymous open registration, got auth %q", authHeader)
	}
	if !strings.Contains(output, `"agent_token": "magt_v1_alpha"`) {
		t.Fatalf("expected shown-once token in output, got %q", output)
	}

	updated, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	if updated.Attachments[0].Auth.Token != "magt_v1_alpha" {
		t.Fatalf("token was not written back: %#v", updated.Attachments[0].Auth)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".moltnet", "identity.json")); err != nil {
		t.Fatalf("identity file not written: %v", err)
	}
}

func TestRunConnectOpenRegistersAndStoresToken(t *testing.T) {
	workspace := t.TempDir()
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/agents/register" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		authHeader = request.Header.Get("Authorization")
		_ = json.NewEncoder(response).Encode(protocol.AgentRegistration{
			NetworkID:  "local",
			AgentID:    "alpha",
			ActorUID:   "actor_1",
			ActorURI:   protocol.AgentFQID("local", "alpha"),
			AgentToken: "magt_v1_alpha",
		})
	}))
	defer server.Close()

	if err := run(context.Background(), []string{
		"connect",
		"--workspace", workspace,
		"--runtime", "codex",
		"--base-url", server.URL,
		"--network-id", "local",
		"--member-id", "alpha",
		"--auth-mode", "open",
		"--rooms", "agora",
		"--install-skill=false",
	}, "test"); err != nil {
		t.Fatalf("run connect: %v", err)
	}
	if authHeader != "" {
		t.Fatalf("expected anonymous open registration, got auth %q", authHeader)
	}

	config, err := clientconfig.LoadFile(filepath.Join(workspace, ".moltnet", "config.json"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.Attachments[0].Auth.Mode != "open" || config.Attachments[0].Auth.Token != "magt_v1_alpha" {
		t.Fatalf("unexpected auth %#v", config.Attachments[0].Auth)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".moltnet", "identity.json")); err != nil {
		t.Fatalf("identity file not written: %v", err)
	}
}

func TestRunConnectOpenRollsBackConfigOnRegistrationFailure(t *testing.T) {
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	err := run(context.Background(), []string{
		"connect",
		"--workspace", workspace,
		"--runtime", "codex",
		"--base-url", server.URL,
		"--network-id", "local",
		"--member-id", "alpha",
		"--auth-mode", "open",
		"--install-skill=false",
	}, "test")
	if err == nil {
		t.Fatal("expected connect registration failure")
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".moltnet", "config.json")); !os.IsNotExist(statErr) {
		t.Fatalf("expected config rollback, stat err=%v", statErr)
	}
}

func TestRegisterAgentBearerStillSendsToken(t *testing.T) {
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

	if err := run(context.Background(), []string{
		"register-agent",
		"--base-url", server.URL,
		"--agent", "alpha",
		"--auth-mode", "bearer",
		"--token", "secret",
		"--write-identity=false",
	}, "test"); err != nil {
		t.Fatalf("run register-agent: %v", err)
	}
	if authHeader != "Bearer secret" {
		t.Fatalf("unexpected bearer auth header %q", authHeader)
	}
}
