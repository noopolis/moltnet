package clisession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
)

func TestEnsureWorkspaceClientConfigWritesOpenToken(t *testing.T) {
	workspace := t.TempDir()
	tokenPath := filepath.Join(t.TempDir(), "alpha.token")
	if err := os.WriteFile(tokenPath, []byte("magt_v1_alpha\n"), 0o600); err != nil {
		t.Fatalf("write token path: %v", err)
	}

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "alpha", Name: "Alpha"},
		Moltnet: bridgeconfig.MoltnetConfig{
			AuthMode:  bridgeconfig.AuthModeOpen,
			BaseURL:   "http://moltnet",
			NetworkID: "local",
			TokenPath: tokenPath,
		},
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeCodex, WorkspacePath: workspace},
		Rooms:   []bridgeconfig.RoomBinding{{ID: "agora", Read: bridgeconfig.ReadAll, Reply: bridgeconfig.ReplyAuto}},
	}

	if err := EnsureWorkspaceClientConfig(config); err != nil {
		t.Fatalf("EnsureWorkspaceClientConfig() error = %v", err)
	}
	clientConfig, err := clientconfig.LoadFile(filepath.Join(workspace, ".moltnet", "config.json"))
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	attachment := clientConfig.Attachments[0]
	if attachment.Auth.Mode != bridgeconfig.AuthModeOpen || attachment.Auth.Token != "magt_v1_alpha" {
		t.Fatalf("unexpected auth %#v", attachment.Auth)
	}
	if attachment.MemberID != "alpha" || attachment.NetworkID != "local" {
		t.Fatalf("unexpected attachment %#v", attachment)
	}
}

func TestEnsureWorkspaceClientConfigPreservesOtherAttachments(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, ".moltnet", "config.json")
	existing := `{
  "version": "moltnet.client.v1",
  "attachments": [
    {
      "auth": {"mode": "none"},
      "base_url": "http://other",
      "member_id": "beta",
      "network_id": "remote"
    }
  ]
}`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	config := bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "alpha"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://moltnet",
			NetworkID: "local",
			Token:     "bearer-secret",
		},
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeClaudeCode, WorkspacePath: workspace},
	}
	if err := EnsureWorkspaceClientConfig(config); err != nil {
		t.Fatalf("EnsureWorkspaceClientConfig() error = %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(contents)
	for _, want := range []string{`"member_id": "beta"`, `"member_id": "alpha"`, `"token": "bearer-secret"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q:\n%s", want, text)
		}
	}
}
