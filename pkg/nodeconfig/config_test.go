package nodeconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
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
      gateway_url: ws://127.0.0.1:9100/gateway
    rooms:
      - id: research
        read: all
        reply: auto
    dms:
      enabled: true
      read: all
      reply: auto
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
					Kind:       bridgeconfig.RuntimeOpenClaw,
					GatewayURL: "ws://127.0.0.1:9100/gateway",
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
}

func TestDiscoverPath(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	writeNodeConfig(t, filepath.Join(directory, DefaultPath), "moltnet:\n  base_url: http://127.0.0.1:8787\n  network_id: local\n")

	path, ok, err := DiscoverPath("")
	if err != nil {
		t.Fatalf("DiscoverPath() error = %v", err)
	}
	if !ok || path != DefaultPath {
		t.Fatalf("unexpected discovery result path=%q ok=%v", path, ok)
	}
}

func TestDiscoverPathExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	writeNodeConfig(t, path, "moltnet:\n  base_url: http://127.0.0.1:8787\n  network_id: local\n")

	discovered, ok, err := DiscoverPath(path)
	if err != nil {
		t.Fatalf("DiscoverPath() explicit error = %v", err)
	}
	if !ok || discovered != path {
		t.Fatalf("unexpected explicit discovery path=%q ok=%v", discovered, ok)
	}
}

func TestDiscoverPathSupportsFallbackNamesAndMissingConfig(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	if path, ok, err := DiscoverPath(""); err != nil || ok || path != "" {
		t.Fatalf("expected no config, got path=%q ok=%v err=%v", path, ok, err)
	}

	writeNodeConfig(t, filepath.Join(directory, defaultYAMLAlt), "moltnet:\n  base_url: http://127.0.0.1:8787\n  network_id: local\n")
	path, ok, err := DiscoverPath("")
	if err != nil {
		t.Fatalf("DiscoverPath() fallback error = %v", err)
	}
	if !ok || path != defaultYAMLAlt {
		t.Fatalf("unexpected fallback discovery path=%q ok=%v", path, ok)
	}
}

func TestDiscoverPathDirectoryError(t *testing.T) {
	directory := t.TempDir()
	if _, _, err := DiscoverPath(directory); err == nil {
		t.Fatal("expected directory error")
	}
}

func writeNodeConfig(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write node config %q: %v", path, err)
	}
}
