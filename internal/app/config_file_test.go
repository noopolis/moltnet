package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestValidateStorage(t *testing.T) {
	t.Parallel()

	valid := []rawStorageConfig{
		{},
		{Kind: storageKindMemory},
		{Kind: storageKindJSON, JSON: rawJSONStorageConfig{Path: "/tmp/state.json"}},
		{Kind: storageKindSQLite, SQLite: rawSQLiteStorageConfig{Path: "/tmp/moltnet.db"}},
		{Kind: storageKindPostgres, Postgres: rawPostgresStorage{DSN: "postgres://moltnet:test@localhost:5432/moltnet?sslmode=disable"}},
	}
	for _, config := range valid {
		if err := validateStorage(config); err != nil {
			t.Fatalf("validateStorage(%#v) error = %v", config, err)
		}
	}

	invalid := []rawStorageConfig{
		{Kind: "wat"},
		{Kind: storageKindJSON},
		{Kind: storageKindSQLite},
		{Kind: storageKindPostgres},
	}
	for _, config := range invalid {
		if err := validateStorage(config); err == nil {
			t.Fatalf("expected validation error for %#v", config)
		}
	}
}

func TestValidateConfigFileVersion(t *testing.T) {
	t.Parallel()

	if err := validateConfigFile(rawConfigFile{Version: defaultConfigSchema}); err != nil {
		t.Fatalf("validateConfigFile() error = %v", err)
	}
	if err := validateConfigFile(rawConfigFile{Version: "moltnet.v9"}); err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestValidatePairingsAndConfigPermissions(t *testing.T) {
	t.Parallel()

	if err := validatePairings([]protocol.Pairing{{ID: "pair_1", RemoteBaseURL: "http://remote:8787"}}); err != nil {
		t.Fatalf("validatePairings() error = %v", err)
	}
	if err := validatePairings([]protocol.Pairing{{ID: "pair_1"}}); err == nil {
		t.Fatal("expected missing remote base url error")
	}
	if err := validatePairings([]protocol.Pairing{{ID: "pair_1", RemoteBaseURL: "ftp://remote"}}); err == nil {
		t.Fatal("expected invalid remote base url scheme error")
	}

	path := filepath.Join(t.TempDir(), "Moltnet")
	writeConfigFile(t, path, `
version: moltnet.v1
network:
  id: local
pairings:
  - id: pair_1
    remote_network_id: remote
    remote_base_url: http://remote:8787
    token: secret
`)
	if _, err := loadFileConfig(path); err != nil {
		t.Fatalf("loadFileConfig() private file error = %v", err)
	}

	insecureAuthPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConfigFile(t, insecureAuthPath, `
version: moltnet.v1
network:
  id: local
auth:
  mode: bearer
  tokens:
    - id: operator
      value: secret
      scopes: [observe]
`)
	if err := os.Chmod(insecureAuthPath, 0o644); err != nil {
		t.Fatalf("chmod insecure auth file: %v", err)
	}
	if _, err := loadFileConfig(insecureAuthPath); err == nil {
		t.Fatal("expected insecure auth token file permission error")
	}

	insecurePostgresPath := filepath.Join(t.TempDir(), "Moltnet")
	writeConfigFile(t, insecurePostgresPath, `
version: moltnet.v1
network:
  id: local
storage:
  kind: postgres
  postgres:
    dsn: postgres://moltnet:secret@localhost:5432/moltnet?sslmode=disable
`)
	if err := os.Chmod(insecurePostgresPath, 0o644); err != nil {
		t.Fatalf("chmod insecure postgres file: %v", err)
	}
	if _, err := loadFileConfig(insecurePostgresPath); err == nil {
		t.Fatal("expected insecure postgres dsn file permission error")
	}
}

func TestValidateAuthConfig(t *testing.T) {
	t.Parallel()

	if err := validateAuth(rawAuthConfig{}, nil); err != nil {
		t.Fatalf("validateAuth() default error = %v", err)
	}
	if err := validateAuth(rawAuthConfig{
		Mode: "bearer",
		Tokens: []rawAuthTokenConfig{
			{ID: "operator", Value: "secret", Scopes: []string{"observe", "write"}, Agents: []string{"researcher"}},
		},
	}, []string{"http://localhost:8787"}); err != nil {
		t.Fatalf("validateAuth() bearer error = %v", err)
	}
	if err := validateAuth(rawAuthConfig{Mode: "wat"}, nil); err == nil {
		t.Fatal("expected unsupported auth mode error")
	}
	if err := validateAuth(rawAuthConfig{
		Mode: "bearer",
		Tokens: []rawAuthTokenConfig{
			{ID: "operator", Value: "secret", Scopes: []string{"wat"}},
		},
	}, nil); err == nil {
		t.Fatal("expected unsupported auth scope error")
	}
}

func TestLoadFileConfigSupportsTrustedForwardedProto(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Moltnet")
	writeConfigFile(t, path, `
version: moltnet.v1
network:
  id: local
server:
  trust_forwarded_proto: true
`)

	config, err := loadFileConfig(path)
	if err != nil {
		t.Fatalf("loadFileConfig() error = %v", err)
	}
	if !config.Server.TrustForwardedProto {
		t.Fatal("expected trust_forwarded_proto to load")
	}
}
