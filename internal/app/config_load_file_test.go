package app

import (
	"path/filepath"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
)

func TestLoadConfigFromMoltnetFile(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	writeConfigFile(t, filepath.Join(directory, defaultConfigFile), `
version: moltnet.v1
network:
  id: local_lab
  name: Local Lab
server:
  listen_addr: 127.0.0.1:8787
  console:
    analytics:
      provider: google
      measurement_id: G-FILE123
  human_ingress: false
  debug_events: true
  direct_messages: false
  allowed_origins:
    - http://localhost:8787
auth:
  mode: bearer
  public_read: true
  require_pair_network_binding: true
  agent_registration: token
  tokens:
    - id: operator
      value: secret
      scopes: [observe, write]
      agents: [researcher]
storage:
  kind: sqlite
  sqlite:
    path: /tmp/moltnet.db
rooms:
  - id: research
    name: Research
    federation: all
    visibility: public
    write_policy: members
    members:
      - orchestrator
      - researcher
pairings:
  - id: pair_1
    remote_network_id: remote
    remote_network_name: Remote
    remote_base_url: http://remote:8787
    status: connected
`)

	config, err := LoadConfig("1.2.3")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.ListenAddr != "127.0.0.1:8787" {
		t.Fatalf("unexpected listen addr %q", config.ListenAddr)
	}
	if config.Storage.Kind != storageKindSQLite || config.Storage.SQLite.Path != "/tmp/moltnet.db" {
		t.Fatalf("unexpected storage %#v", config.Storage)
	}
	if config.AllowHumanIngress {
		t.Fatalf("expected human ingress disabled, got %#v", config)
	}
	if !config.DebugEvents {
		t.Fatalf("expected debug events enabled, got %#v", config)
	}
	if !config.DisableDirectMessages {
		t.Fatalf("expected direct messages disabled, got %#v", config)
	}
	if config.Console.Analytics.Provider != "google" || config.Console.Analytics.MeasurementID != "G-FILE123" {
		t.Fatalf("unexpected console analytics %#v", config.Console.Analytics)
	}
	if config.NetworkID != "local_lab" || config.NetworkName != "Local Lab" {
		t.Fatalf("unexpected network %#v", config)
	}
	if len(config.Rooms) != 1 || config.Rooms[0].ID != "research" {
		t.Fatalf("unexpected rooms %#v", config.Rooms)
	}
	if config.Rooms[0].Visibility != "public" || config.Rooms[0].WritePolicy != "members" {
		t.Fatalf("unexpected room policy %#v", config.Rooms[0])
	}
	if len(config.Pairings) != 1 || config.Pairings[0].ID != "pair_1" {
		t.Fatalf("unexpected pairings %#v", config.Pairings)
	}
	if config.Auth.Mode != authn.ModeBearer || len(config.Auth.Tokens) != 1 || len(config.Auth.AllowedOrigins) != 1 {
		t.Fatalf("unexpected auth %#v", config.Auth)
	}
	if !config.Auth.PublicRead || !config.Auth.RequirePairNetworkBinding || config.Auth.AgentRegistration != authn.AgentRegistrationToken {
		t.Fatalf("unexpected auth policy %#v", config.Auth)
	}
}
