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

func TestRunRegisterAgentOpenHardensPublicConfigWriteback(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, ".moltnet", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{
  "version": "moltnet.client.v1",
  "attachments": [
    {
      "auth": {"mode": "open"},
      "base_url": "http://placeholder",
      "member_id": "alpha",
      "network_id": "local"
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write public config: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(response).Encode(protocol.AgentRegistration{
			NetworkID:  "local",
			AgentID:    "alpha",
			ActorUID:   "actor_1",
			ActorURI:   protocol.AgentFQID("local", "alpha"),
			AgentToken: "magt_v1_alpha",
		})
	}))
	defer server.Close()
	config, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	config.Attachments[0].BaseURL = server.URL
	if err := os.WriteFile(configPath, mustMarshalClientConfig(t, config), 0o644); err != nil {
		t.Fatalf("rewrite public config: %v", err)
	}

	if err := run(context.Background(), []string{
		"register-agent",
		"--config", configPath,
		"--network", "local",
		"--workspace", workspace,
		"--write-identity=false",
	}, "test"); err != nil {
		t.Fatalf("run register-agent: %v", err)
	}

	info, err := os.Lstat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", info.Mode().Perm())
	}
	updated, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	if updated.Attachments[0].Auth.Token != "magt_v1_alpha" {
		t.Fatalf("token was not written back: %#v", updated.Attachments[0].Auth)
	}
}

func TestRunRegisterAgentOpenRejectsSymlinkConfigWriteback(t *testing.T) {
	workspace := t.TempDir()
	targetPath := filepath.Join(workspace, "target-config.json")
	configPath := filepath.Join(workspace, ".moltnet", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte(`{
  "version": "moltnet.client.v1",
  "attachments": [
    {
      "auth": {"mode": "open"},
      "base_url": "http://placeholder",
      "member_id": "alpha",
      "network_id": "local"
    }
  ]
}`), 0o600); err != nil {
		t.Fatalf("write target config: %v", err)
	}
	if err := os.Symlink(targetPath, configPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		called = true
		_ = json.NewEncoder(response).Encode(protocol.AgentRegistration{AgentToken: "magt_v1_alpha"})
	}))
	defer server.Close()
	config, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	config.Attachments[0].BaseURL = server.URL
	if err := os.WriteFile(targetPath, mustMarshalClientConfig(t, config), 0o600); err != nil {
		t.Fatalf("rewrite target config: %v", err)
	}

	err = run(context.Background(), []string{
		"register-agent",
		"--config", configPath,
		"--network", "local",
		"--workspace", workspace,
		"--write-identity=false",
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink writeback error, got %v", err)
	}
	if called {
		t.Fatal("server was contacted before symlink writeback rejection")
	}
}

func TestRunRegisterAgentOpenInheritsConfiguredTokenEnv(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, ".moltnet", "config.json")
	t.Setenv("ALPHA_MOLTNET_TOKEN", "magt_v1_existing")
	if err := writeClientConfig(configPath, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth: clientconfig.AuthConfig{
					Mode:     "open",
					TokenEnv: "ALPHA_MOLTNET_TOKEN",
				},
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
			NetworkID: "local",
			AgentID:   "alpha",
			ActorUID:  "actor_1",
			ActorURI:  protocol.AgentFQID("local", "alpha"),
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

	if err := run(context.Background(), []string{
		"register-agent",
		"--config", configPath,
		"--network", "local",
		"--auth-mode", "open",
		"--workspace", workspace,
		"--write-identity=false",
	}, "test"); err != nil {
		t.Fatalf("run register-agent: %v", err)
	}
	if authHeader != "Bearer magt_v1_existing" {
		t.Fatalf("unexpected auth header %q", authHeader)
	}
}

func TestRunRegisterAgentOpenFailsOnMissingConfiguredTokenEnv(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, ".moltnet", "config.json")
	if err := writeClientConfig(configPath, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth: clientconfig.AuthConfig{
					Mode:     "open",
					TokenEnv: "MISSING_MOLTNET_TOKEN",
				},
				BaseURL:   "http://placeholder",
				MemberID:  "alpha",
				NetworkID: "local",
			},
		},
	}); err != nil {
		t.Fatalf("write config: %v", err)
	}

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		called = true
		_ = json.NewEncoder(response).Encode(protocol.AgentRegistration{AgentToken: "magt_v1_alpha"})
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

	err = run(context.Background(), []string{
		"register-agent",
		"--config", configPath,
		"--network", "local",
		"--auth-mode", "open",
		"--workspace", workspace,
		"--write-identity=false",
	}, "test")
	if err == nil || !strings.Contains(err.Error(), "MISSING_MOLTNET_TOKEN") {
		t.Fatalf("expected missing token env error, got %v", err)
	}
	if called {
		t.Fatal("server was contacted before token env validation")
	}
}

func TestRunRegisterAgentOpenRespectsConfiguredTokenPath(t *testing.T) {
	workspace := t.TempDir()
	tokenPath := filepath.Join(workspace, ".moltnet", "alpha.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		t.Fatalf("mkdir token dir: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("magt_v1_file\n"), 0o600); err != nil {
		t.Fatalf("write token path: %v", err)
	}
	configPath := filepath.Join(workspace, ".moltnet", "config.json")
	if err := writeClientConfig(configPath, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth: clientconfig.AuthConfig{
					Mode:      "open",
					TokenPath: tokenPath,
				},
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
			NetworkID: "local",
			AgentID:   "alpha",
			ActorUID:  "actor_1",
			ActorURI:  protocol.AgentFQID("local", "alpha"),
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

	if err := run(context.Background(), []string{
		"register-agent",
		"--config", configPath,
		"--network", "local",
		"--auth-mode", "open",
		"--workspace", workspace,
		"--write-identity=false",
	}, "test"); err != nil {
		t.Fatalf("run register-agent: %v", err)
	}
	if authHeader != "Bearer magt_v1_file" {
		t.Fatalf("unexpected auth header %q", authHeader)
	}
}

func TestRunConnectOpenReusesExistingConfigTokenSource(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("ALPHA_MOLTNET_TOKEN", "magt_v1_existing")
	configPath := filepath.Join(workspace, ".moltnet", "config.json")
	if err := writeClientConfig(configPath, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth: clientconfig.AuthConfig{
					Mode:     "open",
					TokenEnv: "ALPHA_MOLTNET_TOKEN",
				},
				BaseURL:   "http://old",
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
			NetworkID: "local",
			AgentID:   "alpha",
			ActorUID:  "actor_1",
			ActorURI:  protocol.AgentFQID("local", "alpha"),
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
	if authHeader != "Bearer magt_v1_existing" {
		t.Fatalf("unexpected auth header %q", authHeader)
	}

	config, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	auth := config.Attachments[0].Auth
	if auth.TokenEnv != "ALPHA_MOLTNET_TOKEN" || auth.Token != "" {
		t.Fatalf("token source was not preserved: %#v", auth)
	}
	if config.Attachments[0].Rooms[0].ID != "agora" {
		t.Fatalf("rooms were not updated: %#v", config.Attachments[0].Rooms)
	}
}

func mustMarshalClientConfig(t *testing.T, config clientconfig.Config) []byte {
	t.Helper()

	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return append(payload, '\n')
}
