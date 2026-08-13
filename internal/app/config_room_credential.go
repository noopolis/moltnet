package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func (c RoomCredentialConfig) HasTokenSource() bool {
	return strings.TrimSpace(c.Token.Reveal()) != "" ||
		strings.TrimSpace(c.TokenEnv) != "" ||
		strings.TrimSpace(c.TokenPath) != ""
}

func (c RoomCredentialConfig) ResolveTokenPath(baseDir string) RoomCredentialConfig {
	c.TokenPath = resolveRoomCredentialTokenPath(baseDir, c.TokenPath)
	return c
}

func (c RoomCredentialConfig) ResolveToken() (string, bool, error) {
	if !c.HasTokenSource() {
		return "", false, nil
	}
	if c.Token.Reveal() != "" {
		token := strings.TrimSpace(c.Token.Reveal())
		if token == "" {
			return "", false, fmt.Errorf("room credential.token is empty")
		}
		return token, true, nil
	}
	if envName := strings.TrimSpace(c.TokenEnv); envName != "" {
		token := strings.TrimSpace(os.Getenv(envName))
		if token == "" {
			return "", false, fmt.Errorf("environment variable %q is required for room credential", envName)
		}
		return token, true, nil
	}
	if path := strings.TrimSpace(c.TokenPath); path != "" {
		token, err := bridgeconfig.ReadTokenFile(path)
		if err != nil {
			return "", false, err
		}
		return token, true, nil
	}
	return "", false, nil
}

func resolveRoomCredentialTokenPath(baseDir string, value string) string {
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
