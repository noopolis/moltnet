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
			name: "openclaw requires control url",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:       RuntimeOpenClaw,
					ControlURL: "http://127.0.0.1:9100/team/message",
				},
			},
			ok: true,
		},
		{
			name: "picoclaw requires control url",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:       RuntimePicoClaw,
					ControlURL: "http://127.0.0.1:9100/team/message",
				},
			},
			ok: true,
		},
		{
			name: "tinyclaw missing ack url",
			config: Config{
				Version: VersionV1,
				Agent:   AgentConfig{ID: "researcher"},
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
				Runtime: RuntimeConfig{
					Kind:        RuntimeTinyClaw,
					InboundURL:  "http://127.0.0.1:3777/api/message",
					OutboundURL: "http://127.0.0.1:3777/api/responses/pending?channel=moltnet",
				},
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
    "kind":"tinyclaw",
    "inbound_url":"http://127.0.0.1:3777/api/message",
    "outbound_url":"http://127.0.0.1:3777/api/responses/pending?channel=moltnet",
    "ack_url":"http://127.0.0.1:3777/api/responses"
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
  "runtime":{"kind":"tinyclaw"}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(invalidFromFile); err == nil {
		t.Fatal("expected validation failure from loaded file")
	}
}
