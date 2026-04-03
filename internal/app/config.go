package app

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	defaultListenAddr   = ":8787"
	defaultNetworkID    = "local"
	defaultNetworkName  = "Local Moltnet"
	defaultJSONPath     = ".moltnet/state.json"
	defaultSQLitePath   = ".moltnet/moltnet.db"
	defaultConfigFile   = "Moltnet"
	defaultConfigSchema = "moltnet.v1"
	storageKindMemory   = "memory"
	storageKindJSON     = "json"
	storageKindSQLite   = "sqlite"
	storageKindPostgres = "postgres"
)

const (
	DefaultPath   = defaultConfigFile
	DefaultSchema = defaultConfigSchema
)

type Config struct {
	AllowHumanIngress bool
	Auth              authn.Config
	ListenAddr        string
	NetworkID         string
	NetworkName       string
	Pairings          []protocol.Pairing
	Rooms             []RoomConfig
	Storage           StorageConfig
	Version           string
}

type RoomConfig struct {
	ID      string   `json:"id" yaml:"id"`
	Name    string   `json:"name,omitempty" yaml:"name,omitempty"`
	Members []string `json:"members,omitempty" yaml:"members,omitempty"`
}

type StorageConfig struct {
	Kind     string                `json:"kind" yaml:"kind"`
	JSON     JSONStorageConfig     `json:"json,omitempty" yaml:"json,omitempty"`
	SQLite   SQLiteStorageConfig   `json:"sqlite,omitempty" yaml:"sqlite,omitempty"`
	Postgres PostgresStorageConfig `json:"postgres,omitempty" yaml:"postgres,omitempty"`
}

type JSONStorageConfig struct {
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
}

type SQLiteStorageConfig struct {
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
}

type PostgresStorageConfig struct {
	DSN string `json:"dsn,omitempty" yaml:"dsn,omitempty"`
}

func ConfigFromEnv(version string) (Config, error) {
	return mergeEnvConfig(defaultConfig(version))
}

func envValue(key string) (string, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", false
	}

	return value, true
}

func envBoolValue(key string) (bool, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func envPairings(key string) ([]protocol.Pairing, error) {
	pairings, ok, err := envPairingsValue(key)
	if !ok || err != nil {
		return nil, err
	}
	return pairings, nil
}

func envPairingsValue(key string) ([]protocol.Pairing, bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil, false, nil
	}

	var pairings []protocol.Pairing
	if err := json.Unmarshal([]byte(value), &pairings); err != nil {
		return nil, false, fmt.Errorf("%s must contain valid JSON: %w", key, err)
	}

	return pairings, true, nil
}
