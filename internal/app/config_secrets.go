package app

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func validatePairings(pairings []protocol.Pairing) error {
	for index, pairing := range pairings {
		name := fmt.Sprintf("pairings[%d]", index)
		if strings.TrimSpace(pairing.ID) == "" {
			return fmt.Errorf("%s.id is required", name)
		}
		if strings.TrimSpace(pairing.RemoteBaseURL) == "" {
			return fmt.Errorf("%s.remote_base_url is required", name)
		}
		if err := validateRemoteURL(name+".remote_base_url", pairing.RemoteBaseURL); err != nil {
			return err
		}
	}

	return nil
}

func validateRemoteURL(name string, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}

	switch parsed.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("%s scheme %q is unsupported", name, parsed.Scheme)
	}

	if parsed.Host == "" {
		return fmt.Errorf("%s host is required", name)
	}

	return nil
}

func hasPlaintextPairingTokens(pairings []protocol.Pairing) bool {
	for _, pairing := range pairings {
		if strings.TrimSpace(pairing.Token) != "" {
			return true
		}
	}

	return false
}

func hasPlaintextAuthTokens(config rawAuthConfig) bool {
	for _, token := range config.Tokens {
		if strings.TrimSpace(token.Value) != "" {
			return true
		}
	}

	return false
}

func hasSensitivePostgresConfig(storage rawStorageConfig) bool {
	if strings.TrimSpace(storage.Kind) != storageKindPostgres {
		return false
	}
	return strings.TrimSpace(storage.Postgres.DSN) != ""
}

func validatePrivateConfigMode(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat Moltnet config %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("moltnet config %q must not be a symlink when secrets are present", path)
	}

	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("moltnet config %q must not be group/world readable when tokens are present", path)
	}

	return nil
}
