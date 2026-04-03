package app

import (
	authn "github.com/noopolis/moltnet/internal/auth"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaultsWithoutFile(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	config, err := LoadConfig("1.2.3")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.ListenAddr != defaultListenAddr {
		t.Fatalf("unexpected listen addr %q", config.ListenAddr)
	}
	if config.Storage.Kind != storageKindSQLite || config.Storage.SQLite.Path != defaultSQLitePath {
		t.Fatalf("unexpected storage %#v", config.Storage)
	}
	if config.NetworkID != defaultNetworkID || config.NetworkName != defaultNetworkName {
		t.Fatalf("unexpected network %#v", config)
	}
	if config.Version != "1.2.3" {
		t.Fatalf("unexpected version %q", config.Version)
	}
	if config.Auth.Mode != authn.ModeNone || config.Auth.ListenAddr != defaultListenAddr {
		t.Fatalf("unexpected auth defaults %#v", config.Auth)
	}
}

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
  human_ingress: false
  allowed_origins:
    - http://localhost:8787
auth:
  mode: bearer
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
	if config.NetworkID != "local_lab" || config.NetworkName != "Local Lab" {
		t.Fatalf("unexpected network %#v", config)
	}
	if len(config.Rooms) != 1 || config.Rooms[0].ID != "research" {
		t.Fatalf("unexpected rooms %#v", config.Rooms)
	}
	if len(config.Pairings) != 1 || config.Pairings[0].ID != "pair_1" {
		t.Fatalf("unexpected pairings %#v", config.Pairings)
	}
	if config.Auth.Mode != authn.ModeBearer || len(config.Auth.Tokens) != 1 || len(config.Auth.AllowedOrigins) != 1 {
		t.Fatalf("unexpected auth %#v", config.Auth)
	}
}

func TestLoadConfigEnvOverridesFile(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	writeConfigFile(t, filepath.Join(directory, defaultConfigFile), `
version: moltnet.v1
network:
  id: from_file
  name: From File
server:
  listen_addr: :8787
  human_ingress: true
`)

	t.Setenv("MOLTNET_LISTEN_ADDR", ":9999")
	t.Setenv("MOLTNET_SQLITE_PATH", "/env/moltnet.db")
	t.Setenv("MOLTNET_NETWORK_ID", "from_env")
	t.Setenv("MOLTNET_NETWORK_NAME", "From Env")
	t.Setenv("MOLTNET_ALLOW_HUMAN_INGRESS", "false")

	config, err := LoadConfig("1.2.3")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.ListenAddr != ":9999" || config.Storage.SQLite.Path != "/env/moltnet.db" || config.NetworkID != "from_env" || config.NetworkName != "From Env" {
		t.Fatalf("unexpected env override result %#v", config)
	}
	if config.AllowHumanIngress {
		t.Fatalf("expected env bool override, got %#v", config)
	}
}

func TestLoadConfigUsesExplicitPathFromEnv(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	explicitPath := filepath.Join(directory, "custom.yaml")
	writeConfigFile(t, explicitPath, `
version: moltnet.v1
network:
  id: explicit
`)

	t.Setenv("MOLTNET_CONFIG", explicitPath)

	config, err := LoadConfig("1.2.3")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.NetworkID != "explicit" {
		t.Fatalf("unexpected explicit config %#v", config)
	}
}

func TestLoadConfigRejectsUnknownFields(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	writeConfigFile(t, filepath.Join(directory, defaultConfigFile), `
version: moltnet.v1
network:
  id: local
unknown: true
`)

	if _, err := LoadConfig("1.2.3"); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadConfigSupportsJSONFile(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	explicitPath := filepath.Join(directory, "moltnet.json")
	writeConfigFile(t, explicitPath, `{
  "version": "moltnet.v1",
  "network": {
    "id": "json-net",
    "name": "JSON Net"
  },
  "server": {
    "listen_addr": ":9000",
    "human_ingress": false
  },
  "storage": {
    "kind": "memory"
  }
}`)

	t.Setenv("MOLTNET_CONFIG", explicitPath)

	config, err := LoadConfig("1.2.3")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.NetworkID != "json-net" || config.ListenAddr != ":9000" || config.AllowHumanIngress {
		t.Fatalf("unexpected json config %#v", config)
	}
	if config.Storage.Kind != storageKindMemory {
		t.Fatalf("unexpected storage %#v", config.Storage)
	}
}

func TestLoadConfigSupportsLegacyDataPath(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	writeConfigFile(t, filepath.Join(directory, defaultConfigFile), `
version: moltnet.v1
network:
  id: legacy
server:
  data_path: /tmp/legacy-state.json
`)

	config, err := LoadConfig("1.2.3")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config.Storage.Kind != storageKindJSON || config.Storage.JSON.Path != "/tmp/legacy-state.json" {
		t.Fatalf("unexpected legacy storage %#v", config.Storage)
	}
}

func TestLoadConfigSupportsStorageEnvOverrides(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	t.Setenv("MOLTNET_STORAGE_KIND", storageKindPostgres)
	t.Setenv("MOLTNET_POSTGRES_DSN", "postgres://moltnet:test@localhost:5432/moltnet?sslmode=disable")

	config, err := LoadConfig("1.2.3")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config.Storage.Kind != storageKindPostgres || config.Storage.Postgres.DSN == "" {
		t.Fatalf("unexpected env storage %#v", config.Storage)
	}
}

func TestLoadConfigRejectsInvalidPairingsEnvJSON(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	t.Setenv("MOLTNET_PAIRINGS_JSON", `not-json`)

	if _, err := LoadConfig("1.2.3"); err == nil {
		t.Fatal("expected invalid pairings env error")
	}
}

func TestLoadConfigSupportsJSONPathEnvAliases(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	t.Setenv("MOLTNET_JSON_PATH", "/tmp/from-json-path.json")
	config, err := LoadConfig("1.2.3")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config.Storage.Kind != storageKindJSON || config.Storage.JSON.Path != "/tmp/from-json-path.json" {
		t.Fatalf("unexpected json-path storage %#v", config.Storage)
	}

	t.Setenv("MOLTNET_JSON_PATH", "")
	t.Setenv("MOLTNET_DATA_PATH", "/tmp/from-data-path.json")
	config, err = LoadConfig("1.2.3")
	if err != nil {
		t.Fatalf("LoadConfig() second error = %v", err)
	}
	if config.Storage.Kind != storageKindJSON || config.Storage.JSON.Path != "/tmp/from-data-path.json" {
		t.Fatalf("unexpected data-path storage %#v", config.Storage)
	}
}

func TestLoadFileAppliesDefaultsAndVersion(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	writeConfigFile(t, path, `
version: moltnet.v1
network:
  id: explicit
`)

	config, err := LoadFile(path, "9.9.9")
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if config.NetworkID != "explicit" {
		t.Fatalf("unexpected network id %q", config.NetworkID)
	}
	if config.ListenAddr != defaultListenAddr {
		t.Fatalf("unexpected default listen addr %q", config.ListenAddr)
	}
	if config.Storage.Kind != storageKindSQLite || config.Storage.SQLite.Path != defaultSQLitePath {
		t.Fatalf("unexpected default storage %#v", config.Storage)
	}
	if config.Version != "9.9.9" {
		t.Fatalf("unexpected version %q", config.Version)
	}
}

func TestDiscoverPathSupportsExplicitAndEnvPaths(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	explicit := filepath.Join(directory, "custom.yaml")
	writeConfigFile(t, explicit, `
version: moltnet.v1
network:
  id: explicit
`)

	path, ok, err := DiscoverPath(explicit)
	if err != nil {
		t.Fatalf("DiscoverPath(explicit) error = %v", err)
	}
	if !ok || path != explicit {
		t.Fatalf("unexpected explicit discovery result ok=%v path=%q", ok, path)
	}

	t.Setenv("MOLTNET_CONFIG", explicit)
	path, ok, err = DiscoverPath("")
	if err != nil {
		t.Fatalf("DiscoverPath(env) error = %v", err)
	}
	if !ok || path != explicit {
		t.Fatalf("unexpected env discovery result ok=%v path=%q", ok, path)
	}
}

func TestDiscoverPathFindsFallbackCandidates(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	writeConfigFile(t, filepath.Join(directory, "moltnet.json"), `{
  "version": "moltnet.v1",
  "network": { "id": "json-net" }
}`)

	path, ok, err := DiscoverPath("")
	if err != nil {
		t.Fatalf("DiscoverPath() error = %v", err)
	}
	if !ok || path != "moltnet.json" {
		t.Fatalf("unexpected discovered fallback ok=%v path=%q", ok, path)
	}
}

func TestDiscoverPathRejectsMissingOrDirectoryPaths(t *testing.T) {
	directory := t.TempDir()
	restore := chdirForTest(t, directory)
	defer restore()

	if _, _, err := DiscoverPath(filepath.Join(directory, "missing.yaml")); err == nil {
		t.Fatal("expected missing explicit path error")
	}

	if _, _, err := DiscoverPath(directory); err == nil {
		t.Fatal("expected directory explicit path error")
	}
}

func chdirForTest(t *testing.T, directory string) func() {
	t.Helper()

	t.Chdir(directory)
	return func() {}
}

func writeConfigFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config file %q: %v", path, err)
	}
}
