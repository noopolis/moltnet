package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
	"go.yaml.in/yaml/v3"
)

type rawConfigFile struct {
	Version  string             `json:"version" yaml:"version"`
	Auth     rawAuthConfig      `json:"auth" yaml:"auth"`
	Network  rawNetworkConfig   `json:"network" yaml:"network"`
	Server   rawServerConfig    `json:"server" yaml:"server"`
	Storage  rawStorageConfig   `json:"storage" yaml:"storage"`
	Rooms    []RoomConfig       `json:"rooms" yaml:"rooms"`
	Pairings []protocol.Pairing `json:"pairings" yaml:"pairings"`
}

type rawNetworkConfig struct {
	ID   string `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
}

type rawServerConfig struct {
	ListenAddr          string   `json:"listen_addr" yaml:"listen_addr"`
	DataPath            string   `json:"data_path,omitempty" yaml:"data_path,omitempty"`
	HumanIngress        *bool    `json:"human_ingress" yaml:"human_ingress"`
	AllowedOrigins      []string `json:"allowed_origins,omitempty" yaml:"allowed_origins,omitempty"`
	TrustForwardedProto bool     `json:"trust_forwarded_proto,omitempty" yaml:"trust_forwarded_proto,omitempty"`
}

type rawAuthConfig struct {
	Mode   string               `json:"mode" yaml:"mode"`
	Tokens []rawAuthTokenConfig `json:"tokens" yaml:"tokens"`
}

type rawAuthTokenConfig struct {
	ID     string   `json:"id" yaml:"id"`
	Value  string   `json:"value" yaml:"value"`
	Scopes []string `json:"scopes" yaml:"scopes"`
	Agents []string `json:"agents,omitempty" yaml:"agents,omitempty"`
}

type rawStorageConfig struct {
	Kind     string                 `json:"kind" yaml:"kind"`
	JSON     rawJSONStorageConfig   `json:"json" yaml:"json"`
	SQLite   rawSQLiteStorageConfig `json:"sqlite" yaml:"sqlite"`
	Postgres rawPostgresStorage     `json:"postgres" yaml:"postgres"`
}

type rawJSONStorageConfig struct {
	Path string `json:"path" yaml:"path"`
}

type rawSQLiteStorageConfig struct {
	Path string `json:"path" yaml:"path"`
}

type rawPostgresStorage struct {
	DSN string `json:"dsn" yaml:"dsn"`
}

func loadFileConfig(path string) (rawConfigFile, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return rawConfigFile{}, fmt.Errorf("read Moltnet config %q: %w", path, err)
	}

	var config rawConfigFile
	switch configFormat(path) {
	case "json":
		err = decodeJSONConfig(contents, &config)
	default:
		err = decodeYAMLConfig(contents, &config)
	}
	if err != nil {
		return rawConfigFile{}, fmt.Errorf("decode Moltnet config %q: %w", path, err)
	}

	if err := validateConfigFile(config); err != nil {
		return rawConfigFile{}, fmt.Errorf("validate Moltnet config %q: %w", path, err)
	}
	if hasPlaintextPairingTokens(config.Pairings) || hasPlaintextAuthTokens(config.Auth) || hasSensitivePostgresConfig(config.Storage) {
		if err := validatePrivateConfigMode(path); err != nil {
			return rawConfigFile{}, err
		}
	}

	return config, nil
}

func decodeJSONConfig(contents []byte, target *rawConfigFile) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func decodeYAMLConfig(contents []byte, target *rawConfigFile) error {
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}

func validateConfigFile(config rawConfigFile) error {
	version := strings.TrimSpace(config.Version)
	if version != "" && version != defaultConfigSchema {
		return fmt.Errorf("unsupported version %q", version)
	}
	if err := validateStorage(config.Storage); err != nil {
		return err
	}
	if err := validateAuth(config.Auth, config.Server.AllowedOrigins); err != nil {
		return err
	}
	if err := validatePairings(config.Pairings); err != nil {
		return err
	}

	return nil
}

func validateStorage(storage rawStorageConfig) error {
	switch strings.TrimSpace(storage.Kind) {
	case "", storageKindMemory:
		return nil
	case storageKindJSON:
		if strings.TrimSpace(storage.JSON.Path) == "" {
			return fmt.Errorf("storage.json.path is required for json storage")
		}
		return nil
	case storageKindSQLite:
		if strings.TrimSpace(storage.SQLite.Path) == "" {
			return fmt.Errorf("storage.sqlite.path is required for sqlite storage")
		}
		return nil
	case storageKindPostgres:
		if strings.TrimSpace(storage.Postgres.DSN) == "" {
			return fmt.Errorf("storage.postgres.dsn is required for postgres storage")
		}
		return nil
	default:
		return fmt.Errorf("unsupported storage kind %q", storage.Kind)
	}
}

func validateAuth(config rawAuthConfig, allowedOrigins []string) error {
	_, err := authn.NewPolicy(authn.Config{
		Mode:           strings.TrimSpace(config.Mode),
		AllowedOrigins: append([]string(nil), allowedOrigins...),
		Tokens:         authTokenConfigs(config.Tokens),
	})
	return err
}

func authTokenConfigs(tokens []rawAuthTokenConfig) []authn.TokenConfig {
	configs := make([]authn.TokenConfig, 0, len(tokens))
	for _, token := range tokens {
		scopes := make([]authn.Scope, 0, len(token.Scopes))
		for _, scope := range token.Scopes {
			scopes = append(scopes, authn.Scope(strings.TrimSpace(scope)))
		}
		configs = append(configs, authn.TokenConfig{
			ID:     token.ID,
			Value:  token.Value,
			Scopes: scopes,
			Agents: append([]string(nil), token.Agents...),
		})
	}
	return configs
}
