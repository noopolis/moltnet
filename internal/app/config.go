package app

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	defaultListenAddr  = ":8787"
	defaultNetworkID   = "local"
	defaultNetworkName = "Local Moltnet"
)

type Config struct {
	AllowHumanIngress bool
	ListenAddr        string
	NetworkID         string
	NetworkName       string
	Pairings          []protocol.Pairing
	Version           string
}

func ConfigFromEnv(version string) Config {
	return Config{
		AllowHumanIngress: envBoolOrDefault("MOLTNET_ALLOW_HUMAN_INGRESS", true),
		ListenAddr:        envOrDefault("MOLTNET_LISTEN_ADDR", defaultListenAddr),
		NetworkID:         envOrDefault("MOLTNET_NETWORK_ID", defaultNetworkID),
		NetworkName:       envOrDefault("MOLTNET_NETWORK_NAME", defaultNetworkName),
		Pairings:          envPairings("MOLTNET_PAIRINGS_JSON"),
		Version:           version,
	}
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}

func envBoolOrDefault(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envPairings(key string) []protocol.Pairing {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}

	var pairings []protocol.Pairing
	if err := json.Unmarshal([]byte(value), &pairings); err != nil {
		return nil
	}

	return pairings
}
