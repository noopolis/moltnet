package app

import (
	"path/filepath"
	"testing"
)

func TestLoadConfigForPathEnvOverridesFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, defaultConfigFile)
	writeConfigFile(t, path, `
version: moltnet.v1
network:
  id: from_file
  name: From File
server:
  listen_addr: :8787
  human_ingress: true
  direct_messages: true
`)

	t.Setenv("MOLTNET_LISTEN_ADDR", ":9999")
	t.Setenv("MOLTNET_SQLITE_PATH", "/env/moltnet.db")
	t.Setenv("MOLTNET_NETWORK_ID", "from_env")
	t.Setenv("MOLTNET_NETWORK_NAME", "From Env")
	t.Setenv("MOLTNET_ALLOW_HUMAN_INGRESS", "false")
	t.Setenv("MOLTNET_DEBUG_EVENTS", "true")
	t.Setenv("MOLTNET_ALLOW_DIRECT_MESSAGES", "false")
	t.Setenv("MOLTNET_CONSOLE_ANALYTICS_PROVIDER", "google")
	t.Setenv("MOLTNET_CONSOLE_ANALYTICS_MEASUREMENT_ID", "G-ENV123")

	config, err := LoadConfigForPath(path, "1.2.3")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	if config.ListenAddr != ":9999" || config.Storage.SQLite.Path != "/env/moltnet.db" || config.NetworkID != "from_env" || config.NetworkName != "From Env" {
		t.Fatalf("unexpected env override result %#v", config)
	}
	if config.AllowHumanIngress {
		t.Fatalf("expected env bool override, got %#v", config)
	}
	if !config.DebugEvents {
		t.Fatalf("expected debug events env override, got %#v", config)
	}
	if !config.DisableDirectMessages {
		t.Fatalf("expected direct messages env override, got %#v", config)
	}
	if config.Console.Analytics.Provider != "google" || config.Console.Analytics.MeasurementID != "G-ENV123" {
		t.Fatalf("unexpected console analytics env override %#v", config.Console.Analytics)
	}
}

func TestLoadConfigForPathSupportsJSONFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "moltnet.json")
	writeConfigFile(t, path, `{
  "version": "moltnet.v1",
  "network": {
    "id": "json-net",
    "name": "JSON Net"
  },
  "server": {
    "listen_addr": ":9000",
    "human_ingress": false,
    "direct_messages": false
  },
  "storage": {
    "kind": "memory"
  }
}`)

	config, err := LoadConfigForPath(path, "1.2.3")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	if config.NetworkID != "json-net" || config.ListenAddr != ":9000" || config.AllowHumanIngress {
		t.Fatalf("unexpected json config %#v", config)
	}
	if !config.DisableDirectMessages {
		t.Fatalf("expected json direct messages setting, got %#v", config)
	}
	if config.Storage.Kind != storageKindMemory {
		t.Fatalf("unexpected storage %#v", config.Storage)
	}
}

// TestLoadConfigForPathAnchorsRelativeStorageToConfigDir is the P0 regression
// test: a config discovered away from the process cwd (e.g. under
// ~/.moltnet/<id>/ via the phase-4 fallback) must anchor its default,
// relative storage.sqlite.path against the config file's own directory, not
// whatever directory the process happens to be running from.
func TestLoadConfigForPathAnchorsRelativeStorageToConfigDir(t *testing.T) {
	networkDir := t.TempDir()
	path := filepath.Join(networkDir, defaultConfigFile)
	writeConfigFile(t, path, `
version: moltnet.v1
network:
  id: acme
`)

	unrelatedCwd := t.TempDir()
	t.Chdir(unrelatedCwd)

	config, err := LoadConfigForPath(path, "1.2.3")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	want := filepath.Join(networkDir, defaultSQLitePath)
	if config.Storage.SQLite.Path != want {
		t.Fatalf("Storage.SQLite.Path = %q, want %q (anchored to config dir, not cwd %q)", config.Storage.SQLite.Path, want, unrelatedCwd)
	}
}

// TestLoadConfigForPathAnchorsExplicitRelativeStoragePath covers the same
// anchoring when storage.sqlite.path is set explicitly (rather than left at
// its default) in the config file.
func TestLoadConfigForPathAnchorsExplicitRelativeStoragePath(t *testing.T) {
	networkDir := t.TempDir()
	path := filepath.Join(networkDir, defaultConfigFile)
	writeConfigFile(t, path, `
version: moltnet.v1
network:
  id: acme
storage:
  kind: sqlite
  sqlite:
    path: data/moltnet.db
`)

	unrelatedCwd := t.TempDir()
	t.Chdir(unrelatedCwd)

	config, err := LoadConfigForPath(path, "1.2.3")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	want := filepath.Join(networkDir, "data", "moltnet.db")
	if config.Storage.SQLite.Path != want {
		t.Fatalf("Storage.SQLite.Path = %q, want %q", config.Storage.SQLite.Path, want)
	}
}

// TestLoadConfigForPathKeepsAbsoluteStoragePathsUnchanged ensures the
// anchoring never touches an already-absolute path.
func TestLoadConfigForPathKeepsAbsoluteStoragePathsUnchanged(t *testing.T) {
	networkDir := t.TempDir()
	path := filepath.Join(networkDir, defaultConfigFile)
	writeConfigFile(t, path, `
version: moltnet.v1
network:
  id: acme
storage:
  kind: json
  json:
    path: /var/lib/moltnet/state.json
`)

	config, err := LoadConfigForPath(path, "1.2.3")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	if config.Storage.JSON.Path != "/var/lib/moltnet/state.json" {
		t.Fatalf("Storage.JSON.Path = %q, want unchanged absolute path", config.Storage.JSON.Path)
	}
}

// TestLoadConfigForPathCwdConfigMatchesPreFixBehavior covers the case where
// the config file lives in the process cwd (config dir == cwd): the anchored
// path must resolve to the exact same location the pre-fix, cwd-relative
// behavior produced.
func TestLoadConfigForPathCwdConfigMatchesPreFixBehavior(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	path := filepath.Join(directory, defaultConfigFile)
	writeConfigFile(t, path, `
version: moltnet.v1
network:
  id: acme
`)

	config, err := LoadConfigForPath(path, "1.2.3")
	if err != nil {
		t.Fatalf("LoadConfigForPath() error = %v", err)
	}

	want := filepath.Join(directory, defaultSQLitePath)
	if config.Storage.SQLite.Path != want {
		t.Fatalf("Storage.SQLite.Path = %q, want %q", config.Storage.SQLite.Path, want)
	}
}
