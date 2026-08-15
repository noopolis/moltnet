package nodeconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestLoadFileYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	writeNodeConfig(t, path, `
version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
attachments:
  - agent:
      id: alpha
      name: Alpha
    runtime:
      kind: openclaw
    rooms:
      - id: research
        wake: all
    dms:
      enabled: true
      wake: all
      allowed_wake_senders: [world]
`)

	config, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if config.Moltnet.BaseURL != "http://127.0.0.1:8787" {
		t.Fatalf("unexpected Moltnet config %#v", config.Moltnet)
	}
	if len(config.Attachments) != 1 || config.Attachments[0].Agent.ID != "alpha" {
		t.Fatalf("unexpected attachments %#v", config.Attachments)
	}
	bridgeConfigs := config.BridgeConfigs()
	if bridgeConfigs[0].Runtime.GatewayURL != bridgeconfig.DefaultOpenClawGatewayURL {
		t.Fatalf("expected default gateway URL, got %#v", bridgeConfigs[0].Runtime)
	}
	if got := bridgeConfigs[0].DMs.AllowedWakeSenders; len(got) != 1 || got[0] != "world" {
		t.Fatalf("allowed wake senders did not round-trip: %#v", bridgeConfigs[0].DMs)
	}
}

func TestLoadFileYAMLWithCLIBackedAttachments(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	writeNodeConfig(t, path, `
version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
attachments:
  - agent:
      id: codex_bot
      name: Codex Bot
    runtime:
      kind: codex
      command: ./bin/codex
      workspace_path: ./codex-workspace
      session_store_path: ./codex-workspace/.moltnet/sessions.json
    rooms:
      - id: research
        wake: mentions
  - agent:
      id: claude_bot
      name: Claude Bot
    runtime:
      kind: claude-code
      command: ./bin/claude
      workspace_path: ./claude-workspace
    rooms:
      - id: research
        wake: mentions
`)

	config, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	bridgeConfigs := config.BridgeConfigs()
	if len(bridgeConfigs) != 2 {
		t.Fatalf("expected two bridge configs, got %#v", bridgeConfigs)
	}
	if bridgeConfigs[0].Runtime.Kind != bridgeconfig.RuntimeCodex ||
		bridgeConfigs[0].Runtime.WorkspacePath != "./codex-workspace" ||
		bridgeConfigs[1].Runtime.Kind != bridgeconfig.RuntimeClaudeCode ||
		bridgeConfigs[1].Runtime.WorkspacePath != "./claude-workspace" {
		t.Fatalf("unexpected bridge configs %#v", bridgeConfigs)
	}
}

func TestLoadFileRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	writeNodeConfig(t, path, `
version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
wat: true
`)

	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadFileJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), defaultJSONAlt)
	writeNodeConfig(t, path, `{
  "version": "moltnet.node.v1",
  "moltnet": {
    "base_url": "http://127.0.0.1:8787",
    "network_id": "local"
  },
  "attachments": []
}`)

	config, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() json error = %v", err)
	}
	if config.Moltnet.NetworkID != "local" {
		t.Fatalf("unexpected config %#v", config)
	}
}

func TestLoadFileRejectsInsecureTokenConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	writeNodeConfig(t, path, `
version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
  token: secret
attachments:
  - agent:
      id: alpha
    runtime:
      kind: openclaw
      gateway_url: ws://127.0.0.1:9100/gateway
`)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected insecure permissions error")
	}
}

func TestValidate(t *testing.T) {
	config := Config{
		Version: VersionV1,
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://127.0.0.1:8787",
			NetworkID: "local",
		},
		Attachments: []AttachmentConfig{
			{
				Agent:   bridgeconfig.AgentConfig{ID: "alpha"},
				Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw, GatewayURL: "ws://127.0.0.1:9100/gateway"},
			},
		},
	}

	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	config.Attachments = append(config.Attachments, AttachmentConfig{
		Agent:   bridgeconfig.AgentConfig{ID: "alpha"},
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw, GatewayURL: "ws://127.0.0.1:9100/gateway"},
	})
	if err := config.Validate(); err == nil {
		t.Fatal("expected duplicate agent id error")
	}
}

func TestValidateErrors(t *testing.T) {
	testCases := []Config{
		{Version: "bad"},
		{Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"}},
		{Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://127.0.0.1:8787"}},
		{
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
			Attachments: []AttachmentConfig{
				{Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw, ControlURL: "http://127.0.0.1:9100/team/message"}},
			},
		},
	}

	for index, config := range testCases {
		if err := config.Validate(); err == nil {
			t.Fatalf("expected validation error for case %d", index)
		}
	}
}

func TestValidateRejectsInvalidAllowedWakeSenders(t *testing.T) {
	invalid := [][]string{
		{""},
		{" world "},
		{"remote:world"},
		{"molt://remote/agents/world"},
		{"world", " world "},
		make([]string, protocol.MaxMembersPerRequest+1),
	}
	for _, senders := range invalid {
		config := Config{
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
			Attachments: []AttachmentConfig{{
				Agent:   bridgeconfig.AgentConfig{ID: "alpha"},
				Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw},
				DMs:     &bridgeconfig.DMConfig{Enabled: true, AllowedWakeSenders: senders},
			}},
		}
		if err := config.Validate(); err == nil {
			t.Fatalf("expected invalid allowed wake senders error for %#v", senders)
		}
	}
}

func TestBridgeConfigs(t *testing.T) {
	config := Config{
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://127.0.0.1:8787",
			NetworkID: "local",
			Token:     "secret",
		},
		Attachments: []AttachmentConfig{
			{
				Agent: bridgeconfig.AgentConfig{ID: "alpha"},
				Runtime: bridgeconfig.RuntimeConfig{
					Kind: bridgeconfig.RuntimeOpenClaw,
				},
				DMs: &bridgeconfig.DMConfig{
					Enabled:            true,
					AllowedWakeSenders: []string{"world"},
				},
			},
		},
	}

	configs := config.BridgeConfigs()
	if len(configs) != 1 {
		t.Fatalf("expected one bridge config, got %d", len(configs))
	}
	if configs[0].Version != bridgeconfig.VersionV1 || configs[0].Moltnet.Token != "secret" {
		t.Fatalf("unexpected bridge config %#v", configs[0])
	}
	if configs[0].Runtime.GatewayURL != bridgeconfig.DefaultOpenClawGatewayURL {
		t.Fatalf("expected normalized runtime defaults, got %#v", configs[0].Runtime)
	}
	if configs[0].DMs == nil || len(configs[0].DMs.AllowedWakeSenders) != 1 || configs[0].DMs.AllowedWakeSenders[0] != "world" {
		t.Fatalf("expected allowed wake sender policy to survive bridge lowering, got %#v", configs[0].DMs)
	}
}

func TestLoadFileResolvesPerAttachmentTokenPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultPath)
	writeNodeConfig(t, path, `
version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
  auth_mode: open
attachments:
  - agent:
      id: alpha
    moltnet:
      token_path: .moltnet/alpha.token
    runtime:
      kind: openclaw
`)

	config, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	bridgeConfigs := config.BridgeConfigs()
	want := filepath.Join(dir, ".moltnet", "alpha.token")
	if bridgeConfigs[0].Moltnet.TokenPath != want {
		t.Fatalf("TokenPath = %q, want %q", bridgeConfigs[0].Moltnet.TokenPath, want)
	}
	if bridgeConfigs[0].Moltnet.Token != "" {
		t.Fatalf("generated open tokens must not be stored inline, got %#v", bridgeConfigs[0].Moltnet)
	}
}

func TestAttachmentMoltnetTokenOverridesSharedToken(t *testing.T) {
	config := Config{
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://127.0.0.1:8787",
			NetworkID: "local",
			Token:     "shared",
		},
		Attachments: []AttachmentConfig{
			{
				Agent:   bridgeconfig.AgentConfig{ID: "alpha"},
				Moltnet: bridgeconfig.MoltnetTokenConfig{TokenEnv: "ALPHA_TOKEN"},
				Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw},
			},
		},
	}

	bridgeConfigs := config.BridgeConfigs()
	if bridgeConfigs[0].Moltnet.Token != "" || bridgeConfigs[0].Moltnet.TokenEnv != "ALPHA_TOKEN" {
		t.Fatalf("attachment token source did not override shared token: %#v", bridgeConfigs[0].Moltnet)
	}
}

func TestValidateRejectsSharedOpenAgentToken(t *testing.T) {
	config := Config{
		Moltnet: bridgeconfig.MoltnetConfig{
			AuthMode:    bridgeconfig.AuthModeOpen,
			BaseURL:     "http://127.0.0.1:8787",
			NetworkID:   "local",
			StaticToken: true,
			Token:       "magt_v1_shared",
		},
		Attachments: []AttachmentConfig{
			{
				Agent:   bridgeconfig.AgentConfig{ID: "alpha"},
				Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw},
			},
		},
	}
	if err := config.Validate(); err == nil {
		t.Fatal("expected shared open agent token rejection")
	}
}

func writeNodeConfig(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write node config %q: %v", path, err)
	}
}
