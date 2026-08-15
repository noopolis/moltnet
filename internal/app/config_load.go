package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

var DefaultDiscoveryOrder = []string{
	defaultConfigFile,
	"moltnet.yaml",
	"moltnet.yml",
	"moltnet.json",
}

func LoadFile(path string, version string) (Config, error) {
	fileConfig, err := loadFileConfig(path)
	if err != nil {
		return Config{}, err
	}

	config := mergeFileConfig(defaultConfig(version), fileConfig)
	config.roomCredentialBaseDir = filepath.Dir(path)
	config.Storage = anchorStoragePaths(config.Storage, config.roomCredentialBaseDir)
	if err := validateRoomFederationStances(config.Rooms, config.Pairings); err != nil {
		return Config{}, err
	}
	return config, nil
}

// LoadConfigForPath loads and validates an explicit Moltnet config path
// through the same full load path the server uses at startup, including
// environment overrides. Unlike LoadFile (used by `moltnet validate`), it
// merges environment configuration, so it is the right check to run after a
// config writeback: LoadFile alone can pass a config that would fail once
// the server actually starts.
func LoadConfigForPath(path string, version string) (Config, error) {
	fileConfig, err := loadFileConfig(path)
	if err != nil {
		return Config{}, err
	}

	config := mergeFileConfig(defaultConfig(version), fileConfig)
	config.roomCredentialBaseDir = filepath.Dir(path)
	config.Storage = anchorStoragePaths(config.Storage, config.roomCredentialBaseDir)

	config, err = mergeEnvConfig(config)
	if err != nil {
		return Config{}, err
	}
	if err := validateRoomFederationStances(config.Rooms, config.Pairings); err != nil {
		return Config{}, err
	}
	return config, nil
}

// anchorStoragePaths resolves relative storage.json.path and
// storage.sqlite.path values (whether set explicitly in the config file or
// left at their defaults) against baseDir, the directory containing the
// config file that was loaded. Without this, a relative default like
// ".moltnet/moltnet.db" resolves against the process's current working
// directory instead of the network directory the config lives in, which
// breaks `moltnet start` when it locates a config via the ~/.moltnet/<id>/
// fallback from an unrelated cwd. Absolute paths are left untouched, and
// callers that have no config path (env-only / zero-config loads) never call
// this, so that behavior stays cwd-relative exactly as before.
func anchorStoragePaths(storage StorageConfig, baseDir string) StorageConfig {
	storage.JSON.Path = resolveConfigRelativePath(baseDir, storage.JSON.Path)
	storage.SQLite.Path = resolveConfigRelativePath(baseDir, storage.SQLite.Path)
	return storage
}

func resolveConfigRelativePath(baseDir string, value string) string {
	path := strings.TrimSpace(value)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	root := strings.TrimSpace(baseDir)
	if root == "" {
		root = "."
	}
	return filepath.Clean(filepath.Join(root, path))
}

func defaultConfig(version string) Config {
	return Config{
		AllowHumanIngress: true,
		Auth: authn.Config{
			Mode:                authn.ModeNone,
			AgentRegistration:   authn.AgentRegistrationDisabled,
			ListenAddr:          defaultListenAddr,
			TrustForwardedProto: false,
		},
		ListenAddr:  defaultListenAddr,
		NetworkID:   defaultNetworkID,
		NetworkName: defaultNetworkName,
		Storage: StorageConfig{
			Kind: storageKindSQLite,
			SQLite: SQLiteStorageConfig{
				Path: defaultSQLitePath,
			},
		},
		Version: version,
	}
}

func mergeFileConfig(config Config, fileConfig rawConfigFile) Config {
	if fileConfig.Network.ID != "" {
		config.NetworkID = fileConfig.Network.ID
	}
	if fileConfig.Network.Name != "" {
		config.NetworkName = fileConfig.Network.Name
	}
	if fileConfig.Server.ListenAddr != "" {
		config.ListenAddr = fileConfig.Server.ListenAddr
	}
	config.Auth.ListenAddr = config.ListenAddr
	if fileConfig.Server.HumanIngress != nil {
		config.AllowHumanIngress = *fileConfig.Server.HumanIngress
	}
	if fileConfig.Server.Console.Analytics.Provider != "" || fileConfig.Server.Console.Analytics.MeasurementID != "" {
		config.Console.Analytics = ConsoleAnalyticsConfig{
			Provider:      strings.TrimSpace(fileConfig.Server.Console.Analytics.Provider),
			MeasurementID: strings.TrimSpace(fileConfig.Server.Console.Analytics.MeasurementID),
		}
	}
	if fileConfig.Server.DebugEvents != nil {
		config.DebugEvents = *fileConfig.Server.DebugEvents
	}
	if fileConfig.Server.DirectMessages != nil {
		config.DisableDirectMessages = !*fileConfig.Server.DirectMessages
	}
	if fileConfig.Server.AllowedOrigins != nil {
		config.Auth.AllowedOrigins = append([]string(nil), fileConfig.Server.AllowedOrigins...)
	}
	config.Auth.TrustForwardedProto = fileConfig.Server.TrustForwardedProto
	if strings.TrimSpace(fileConfig.Auth.Mode) != "" {
		config.Auth.Mode = strings.TrimSpace(fileConfig.Auth.Mode)
	}
	if fileConfig.Auth.PublicRead != nil {
		config.Auth.PublicRead = *fileConfig.Auth.PublicRead
	}
	if fileConfig.Auth.RequirePairNetworkBinding != nil {
		config.Auth.RequirePairNetworkBinding = *fileConfig.Auth.RequirePairNetworkBinding
	}
	if strings.TrimSpace(fileConfig.Auth.AgentRegistration) != "" {
		config.Auth.AgentRegistration = strings.TrimSpace(fileConfig.Auth.AgentRegistration)
	}
	if config.Auth.Mode == authn.ModeOpen {
		config.Auth.PublicRead = true
		config.Auth.AgentRegistration = authn.AgentRegistrationOpen
	}
	if fileConfig.Auth.Tokens != nil {
		config.Auth.Tokens = authTokenConfigs(fileConfig.Auth.Tokens)
	}
	config.Storage = mergeFileStorage(config.Storage, fileConfig)
	if fileConfig.Pairings != nil {
		config.Pairings = append([]protocol.Pairing(nil), fileConfig.Pairings...)
	}
	if fileConfig.Rooms != nil {
		config.Rooms = append([]RoomConfig(nil), fileConfig.Rooms...)
	}

	return config
}

func mergeEnvConfig(config Config) (Config, error) {
	if value, ok := envValue("MOLTNET_LISTEN_ADDR"); ok {
		config.ListenAddr = value
	}
	config.Auth.ListenAddr = config.ListenAddr
	if value, ok := envValue("MOLTNET_NETWORK_ID"); ok {
		config.NetworkID = value
	}
	if value, ok := envValue("MOLTNET_NETWORK_NAME"); ok {
		config.NetworkName = value
	}
	if value, ok := envBoolValue("MOLTNET_ALLOW_HUMAN_INGRESS"); ok {
		config.AllowHumanIngress = value
	}
	if value, ok := envBoolValue("MOLTNET_REQUIRE_PAIR_NETWORK_BINDING"); ok {
		config.Auth.RequirePairNetworkBinding = value
	}
	if value, ok := envValue("MOLTNET_CAUSAL_EVENTS_PATH"); ok {
		config.CausalEventsPath = value
	}
	if value, ok := envBoolValue("MOLTNET_DEBUG_EVENTS"); ok {
		config.DebugEvents = value
	}
	if value, ok := envBoolValue("MOLTNET_ALLOW_DIRECT_MESSAGES"); ok {
		config.DisableDirectMessages = !value
	}
	if value, ok := envValue("MOLTNET_CONSOLE_ANALYTICS_PROVIDER"); ok {
		config.Console.Analytics.Provider = value
	}
	if value, ok := envValue("MOLTNET_CONSOLE_ANALYTICS_MEASUREMENT_ID"); ok {
		config.Console.Analytics.MeasurementID = value
	}
	if pairings, ok, err := envPairingsValue("MOLTNET_PAIRINGS_JSON"); err != nil {
		return Config{}, err
	} else if ok {
		config.Pairings = pairings
	}
	config.Storage = mergeEnvStorage(config.Storage)
	if err := validateConsoleAnalytics(config.Console.Analytics.Provider, config.Console.Analytics.MeasurementID); err != nil {
		return Config{}, err
	}

	return config, nil
}

func mergeFileStorage(storage StorageConfig, fileConfig rawConfigFile) StorageConfig {
	if fileConfig.Storage.Kind != "" {
		storage.Kind = fileConfig.Storage.Kind
	}
	if fileConfig.Storage.JSON.Path != "" {
		storage.JSON.Path = fileConfig.Storage.JSON.Path
	}
	if fileConfig.Storage.SQLite.Path != "" {
		storage.SQLite.Path = fileConfig.Storage.SQLite.Path
	}
	if fileConfig.Storage.Postgres.DSN != "" {
		storage.Postgres.DSN = fileConfig.Storage.Postgres.DSN
	}

	if fileConfig.Server.DataPath != "" && strings.TrimSpace(fileConfig.Storage.Kind) == "" {
		storage.Kind = storageKindJSON
		storage.JSON.Path = fileConfig.Server.DataPath
	}

	return storage
}

func mergeEnvStorage(storage StorageConfig) StorageConfig {
	explicitKind, hasExplicitKind := envValue("MOLTNET_STORAGE_KIND")
	if hasExplicitKind {
		storage.Kind = explicitKind
	}
	if value, ok := envValue("MOLTNET_JSON_PATH"); ok {
		storage.JSON.Path = value
		if !hasExplicitKind {
			storage.Kind = storageKindJSON
		}
	}
	if value, ok := envValue("MOLTNET_DATA_PATH"); ok {
		storage.JSON.Path = value
		if !hasExplicitKind {
			storage.Kind = storageKindJSON
		}
	}
	if value, ok := envValue("MOLTNET_SQLITE_PATH"); ok {
		storage.SQLite.Path = value
		if !hasExplicitKind {
			storage.Kind = storageKindSQLite
		}
	}
	if value, ok := envValue("MOLTNET_POSTGRES_DSN"); ok {
		storage.Postgres.DSN = value
		if !hasExplicitKind {
			storage.Kind = storageKindPostgres
		}
	}

	return storage
}

func DiscoverPath(explicit string) (string, bool, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return explicitConfigPath(value)
	}
	if value, ok := envValue("MOLTNET_CONFIG"); ok {
		return explicitConfigPath(value)
	}

	for _, candidate := range DefaultDiscoveryOrder {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", false, fmt.Errorf("inspect Moltnet config %q: %w", candidate, err)
		}
	}

	return "", false, nil
}

func explicitConfigPath(value string) (string, bool, error) {
	path := strings.TrimSpace(value)
	if path == "" {
		return "", false, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", false, fmt.Errorf("stat Moltnet config %q: %w", path, err)
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("moltnet config %q is a directory", path)
	}

	return path, true, nil
}

func configFormat(path string) string {
	extension := strings.ToLower(filepath.Ext(path))
	if extension == ".json" {
		return "json"
	}

	return "yaml"
}
