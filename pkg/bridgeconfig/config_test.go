package bridgeconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	validTinyClaw := Config{
		Version: VersionV1,
		Agent:   AgentConfig{ID: "researcher"},
		Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
		Runtime: RuntimeConfig{
			Kind:        RuntimeTinyClaw,
			InboundURL:  "http://127.0.0.1:3777/api/message",
			OutboundURL: "http://127.0.0.1:3777/api/responses/pending?channel=moltnet",
			AckURL:      "http://127.0.0.1:3777/api/responses",
		},
	}

	tests := []struct {
		name   string
		config Config
		ok     bool
	}{
		{name: "valid tinyclaw", config: validTinyClaw, ok: true},
		{
			name: "tinyclaw accepts default local api urls",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{Kind: RuntimeTinyClaw},
			},
			ok: true,
		},
		{name: "missing version", config: Config{}, ok: false},
		{
			name: "unsupported runtime",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{Kind: "unknown"},
			},
		},
		{
			name: "openclaw accepts gateway url",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:       RuntimeOpenClaw,
					GatewayURL: "ws://127.0.0.1:18789",
				},
			},
			ok: true,
		},
		{
			name: "openclaw accepts default local gateway url",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind: RuntimeOpenClaw,
				},
			},
			ok: true,
		},
		{
			name: "openclaw rejects legacy control url",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:       RuntimeOpenClaw,
					ControlURL: "http://127.0.0.1:9100/team/message",
				},
			},
		},
		{
			name: "openclaw rejects conflicting control url even with gateway",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:       RuntimeOpenClaw,
					GatewayURL: "ws://127.0.0.1:18789",
					ControlURL: "http://127.0.0.1:9100/team/message",
				},
			},
		},
		{
			name: "picoclaw accepts direct command config",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:       RuntimePicoClaw,
					Command:    "/usr/local/bin/picoclaw",
					ConfigPath: "/var/lib/spawnfile/instances/picoclaw/agent-researcher/picoclaw/config.json",
				},
			},
			ok: true,
		},
		{
			name: "picoclaw accepts default local event socket",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind: RuntimePicoClaw,
				},
			},
			ok: true,
		},
		{
			name: "picoclaw defaults command when config path is present",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:       RuntimePicoClaw,
					ConfigPath: "/var/lib/spawnfile/instances/picoclaw/agent-researcher/picoclaw/config.json",
				},
			},
			ok: true,
		},
		{
			name: "picoclaw command requires config path",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:    RuntimePicoClaw,
					Command: "/usr/local/bin/picoclaw",
				},
			},
		},
		{
			name: "claude code accepts workspace path",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:          RuntimeClaudeCode,
					Command:       "/usr/local/bin/claude",
					WorkspacePath: "/tmp/claude-workspace",
				},
			},
			ok: true,
		},
		{
			name: "codex accepts workspace path",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:             RuntimeCodex,
					Command:          "/usr/local/bin/codex",
					WorkspacePath:    "/tmp/codex-workspace",
					SessionStorePath: "/tmp/codex-workspace/.moltnet/sessions.json",
				},
			},
			ok: true,
		},
		{
			name: "codex requires workspace path",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind: RuntimeCodex,
				},
			},
		},
		{
			name: "claude code rejects control url",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:          RuntimeClaudeCode,
					WorkspacePath: "/tmp/claude-workspace",
					ControlURL:    "http://127.0.0.1:9000/control",
				},
			},
		},
		{
			name: "picoclaw rejects invalid events url scheme",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:      RuntimePicoClaw,
					EventsURL: "http://127.0.0.1:18990/pico/ws",
				},
			},
		},
		{
			name: "tinyclaw rejects invalid ack url",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:        RuntimeTinyClaw,
					InboundURL:  "http://127.0.0.1:3777/api/message",
					OutboundURL: "http://127.0.0.1:3777/api/responses/pending?channel=moltnet",
					AckURL:      "unix:///tmp/tinyclaw.sock",
				},
			},
		},
		{
			name: "invalid read config",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{Kind: RuntimeOpenClaw, ControlURL: "http://127.0.0.1:18789/hooks/agent"},
				Rooms:   []RoomBinding{{ID: "research", Read: ReadConfig("whenever")}},
			},
		},
		{
			name: "invalid reply config",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{Kind: RuntimeOpenClaw, ControlURL: "http://127.0.0.1:18789/hooks/agent"},
				Rooms:   []RoomBinding{{ID: "research", Reply: ReplyConfig("whenever")}},
			},
		},
		{
			name: "invalid control url scheme",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{Kind: RuntimeOpenClaw, ControlURL: "unix:///tmp/control.sock"},
			},
		},
		{
			name: "invalid gateway url scheme",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{Kind: RuntimeOpenClaw, GatewayURL: "http://127.0.0.1:18789"},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.config.Validate()
			if test.ok && err != nil {
				t.Fatalf("expected success, got %v", err)
			}

			if !test.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLoadFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	validPath := filepath.Join(dir, "bridge.json")
	if err := os.WriteFile(validPath, []byte(`{
  "version":"moltnet.bridge.v1",
  "agent":{"id":"researcher"},
  "moltnet":{"base_url":"http://127.0.0.1:8787","network_id":"local"},
  "runtime":{
    "kind":"tinyclaw"
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := LoadFile(validPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if config.Agent.ID != "researcher" {
		t.Fatalf("unexpected agent id %q", config.Agent.ID)
	}
	if config.Runtime.InboundURL != "http://127.0.0.1:3777/api/message" ||
		config.Runtime.OutboundURL != "http://127.0.0.1:3777/api/responses/pending?channel=moltnet" ||
		config.Runtime.AckURL != "http://127.0.0.1:3777/api/responses" {
		t.Fatalf("runtime defaults were not applied: %#v", config.Runtime)
	}

	invalidPath := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalidPath, []byte(`{"version":`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFile(invalidPath); err == nil {
		t.Fatal("expected invalid json error")
	}

	if _, err := LoadFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("expected missing file error")
	}

	insecureTokenPath := filepath.Join(dir, "bridge-insecure.json")
	if err := os.WriteFile(insecureTokenPath, []byte(`{
  "version":"moltnet.bridge.v1",
  "agent":{"id":"researcher"},
  "moltnet":{"base_url":"http://127.0.0.1:8787","network_id":"local","token":"secret"},
  "runtime":{"kind":"openclaw","gateway_url":"ws://127.0.0.1:18789"}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(insecureTokenPath); err == nil {
		t.Fatal("expected insecure token file permissions error")
	}

	insecureRuntimeTokenPath := filepath.Join(dir, "bridge-runtime-token-insecure.json")
	if err := os.WriteFile(insecureRuntimeTokenPath, []byte(`{
  "version":"moltnet.bridge.v1",
  "agent":{"id":"researcher"},
  "moltnet":{"base_url":"http://127.0.0.1:8787","network_id":"local"},
  "runtime":{"kind":"openclaw","gateway_url":"ws://127.0.0.1:18789","token":"secret"}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(insecureRuntimeTokenPath); err == nil {
		t.Fatal("expected insecure runtime token file permissions error")
	}
}

func TestConfigValidateBaseFieldErrors(t *testing.T) {
	t.Parallel()

	tests := []Config{
		{},
		{Version: VersionV1},
		{Version: VersionV1, Agent: AgentConfig{ID: "researcher"}},
		{Version: VersionV1, Agent: AgentConfig{ID: "researcher"}, Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787"}},
	}

	for _, config := range tests {
		config := config
		t.Run(config.Version+"-"+config.Agent.ID, func(t *testing.T) {
			t.Parallel()
			if err := config.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	invalidFromFile := filepath.Join(t.TempDir(), "invalid-validated.json")
	if err := os.WriteFile(invalidFromFile, []byte(`{
  "version":"moltnet.bridge.v1",
  "agent":{"id":"researcher"},
  "moltnet":{"base_url":"http://127.0.0.1:8787","network_id":"local"},
  "runtime":{"kind":"unknown"}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(invalidFromFile); err == nil {
		t.Fatal("expected validation failure from loaded file")
	}
}
