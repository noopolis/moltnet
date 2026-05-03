package nodeconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func TestValidateRejectsSharedOpenAgentTokenFromEnv(t *testing.T) {
	t.Setenv("SHARED_MOLTNET_TOKEN", "magt_v1_shared")
	config := Config{
		Moltnet: bridgeconfig.MoltnetConfig{
			AuthMode:    bridgeconfig.AuthModeOpen,
			BaseURL:     "http://127.0.0.1:8787",
			NetworkID:   "local",
			StaticToken: true,
			TokenEnv:    "SHARED_MOLTNET_TOKEN",
		},
		Attachments: []AttachmentConfig{
			{
				Agent:   bridgeconfig.AgentConfig{ID: "alpha"},
				Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw},
			},
		},
	}
	if err := config.Validate(); err == nil {
		t.Fatal("expected shared open env agent token rejection")
	}
}

func TestLoadFileRejectsSharedOpenAgentTokenFromTokenPath(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "shared.token")
	if err := os.WriteFile(tokenPath, []byte("magt_v1_shared\n"), 0o600); err != nil {
		t.Fatalf("write token path: %v", err)
	}
	path := filepath.Join(dir, DefaultPath)
	writeNodeConfig(t, path, `
version: moltnet.node.v1
moltnet:
  auth_mode: open
  base_url: http://127.0.0.1:8787
  network_id: local
  static_token: true
  token_path: shared.token
attachments:
  - agent:
      id: alpha
    runtime:
      kind: openclaw
`)

	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected shared open token_path agent token rejection")
	}
}
