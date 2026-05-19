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
	ListenAddr          string           `json:"listen_addr" yaml:"listen_addr"`
	DataPath            string           `json:"data_path,omitempty" yaml:"data_path,omitempty"`
	Console             rawConsoleConfig `json:"console,omitempty" yaml:"console,omitempty"`
	HumanIngress        *bool            `json:"human_ingress" yaml:"human_ingress"`
	DebugEvents         *bool            `json:"debug_events" yaml:"debug_events"`
	DirectMessages      *bool            `json:"direct_messages" yaml:"direct_messages"`
	AllowedOrigins      []string         `json:"allowed_origins,omitempty" yaml:"allowed_origins,omitempty"`
	TrustForwardedProto bool             `json:"trust_forwarded_proto,omitempty" yaml:"trust_forwarded_proto,omitempty"`
}

type rawConsoleConfig struct {
	Analytics rawConsoleAnalyticsConfig `json:"analytics,omitempty" yaml:"analytics,omitempty"`
}

type rawConsoleAnalyticsConfig struct {
	Provider      string `json:"provider" yaml:"provider"`
	MeasurementID string `json:"measurement_id" yaml:"measurement_id"`
}

type rawAuthConfig struct {
	Mode              string               `json:"mode" yaml:"mode"`
	PublicRead        *bool                `json:"public_read,omitempty" yaml:"public_read,omitempty"`
	AgentRegistration string               `json:"agent_registration,omitempty" yaml:"agent_registration,omitempty"`
	Tokens            []rawAuthTokenConfig `json:"tokens" yaml:"tokens"`
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
	contents, err := readConfigFile(path)
	if err != nil {
		return rawConfigFile{}, err
	}

	var config rawConfigFile
	if err := decodeConfigBytes(path, contents, &config); err != nil {
		return rawConfigFile{}, err
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

func readConfigFile(path string) ([]byte, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Moltnet config %q: %w", path, err)
	}
	return contents, nil
}

func decodeConfigBytes(path string, contents []byte, config *rawConfigFile) error {
	var err error
	switch configFormat(path) {
	case "json":
		err = decodeJSONConfig(contents, config)
	default:
		err = decodeYAMLConfig(contents, config)
	}
	if err != nil {
		return fmt.Errorf("decode Moltnet config %q: %w", path, err)
	}
	return nil
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
	if err := validateConsole(config.Server.Console); err != nil {
		return err
	}
	if err := validatePairings(config.Pairings); err != nil {
		return err
	}
	if err := validateRooms(config.Rooms); err != nil {
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

func validateConsole(config rawConsoleConfig) error {
	analytics := config.Analytics
	return validateConsoleAnalytics(analytics.Provider, analytics.MeasurementID)
}

func validateConsoleAnalytics(providerValue string, measurementIDValue string) error {
	provider := strings.TrimSpace(providerValue)
	measurementID := strings.TrimSpace(measurementIDValue)
	if provider == "" && measurementID == "" {
		return nil
	}
	if provider == "" {
		return fmt.Errorf("server.console.analytics.provider is required")
	}
	if provider != "google" {
		return fmt.Errorf("unsupported server.console.analytics.provider %q", provider)
	}
	if measurementID == "" {
		return fmt.Errorf("server.console.analytics.measurement_id is required")
	}
	if !strings.HasPrefix(measurementID, "G-") {
		return fmt.Errorf("server.console.analytics.measurement_id must be a Google Analytics measurement ID")
	}

	return nil
}

func validateAuth(config rawAuthConfig, allowedOrigins []string) error {
	publicRead := false
	if config.PublicRead != nil {
		publicRead = *config.PublicRead
	}
	_, err := authn.NewPolicy(authn.Config{
		Mode:              strings.TrimSpace(config.Mode),
		PublicRead:        publicRead,
		AgentRegistration: strings.TrimSpace(config.AgentRegistration),
		AllowedOrigins:    append([]string(nil), allowedOrigins...),
		Tokens:            authTokenConfigs(config.Tokens),
	})
	return err
}

func validateRooms(rooms []RoomConfig) error {
	for index, room := range rooms {
		if err := protocol.ValidateRoomVisibility(room.Visibility); err != nil {
			return fmt.Errorf("rooms[%d].visibility: %w", index, err)
		}
		if err := protocol.ValidateRoomWritePolicy(room.WritePolicy); err != nil {
			return fmt.Errorf("rooms[%d].write_policy: %w", index, err)
		}
	}
	return nil
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
