package app

import (
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/configfile"
	"github.com/noopolis/moltnet/pkg/protocol"
)

var DefaultDiscoveryOrder = []string{
	defaultConfigFile,
	"moltnet.yaml",
	"moltnet.yml",
	"moltnet.json",
}

func LoadConfig(version string) (Config, error) {
	config := defaultConfig(version)

	path, ok, err := DiscoverPath("")
	if err != nil {
		return Config{}, err
	}
	if ok {
		fileConfig, err := loadFileConfig(path)
		if err != nil {
			return Config{}, err
		}

		config = mergeFileConfig(config, fileConfig)
	}

	return mergeEnvConfig(config)
}

func LoadFile(path string, version string) (Config, error) {
	fileConfig, err := loadFileConfig(path)
	if err != nil {
		return Config{}, err
	}

	return mergeFileConfig(defaultConfig(version), fileConfig), nil
}

func defaultConfig(version string) Config {
	return Config{
		AllowHumanIngress: true,
		Auth: authn.Config{
			Mode:                authn.ModeNone,
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
	if fileConfig.Server.AllowedOrigins != nil {
		config.Auth.AllowedOrigins = append([]string(nil), fileConfig.Server.AllowedOrigins...)
	}
	config.Auth.TrustForwardedProto = fileConfig.Server.TrustForwardedProto
	if strings.TrimSpace(fileConfig.Auth.Mode) != "" {
		config.Auth.Mode = strings.TrimSpace(fileConfig.Auth.Mode)
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
	if pairings, ok, err := envPairingsValue("MOLTNET_PAIRINGS_JSON"); err != nil {
		return Config{}, err
	} else if ok {
		config.Pairings = pairings
	}
	config.Storage = mergeEnvStorage(config.Storage)

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
	return configfile.DiscoverPath(explicit, "MOLTNET_CONFIG", DefaultDiscoveryOrder, "Moltnet config")
}
