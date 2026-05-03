package bridgeconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const AgentTokenPrefix = "magt_v1_"

func (c Config) ResolveTokenPaths(baseDir string) Config {
	c.Moltnet = c.Moltnet.ResolveTokenPath(baseDir)
	return c
}

func (m MoltnetConfig) ResolveTokenPath(baseDir string) MoltnetConfig {
	m.TokenPath = resolveTokenPath(baseDir, m.TokenPath)
	return m
}

func (m MoltnetTokenConfig) ResolveTokenPath(baseDir string) MoltnetTokenConfig {
	m.TokenPath = resolveTokenPath(baseDir, m.TokenPath)
	return m
}

func (m MoltnetConfig) EffectiveAuthMode() string {
	mode := strings.TrimSpace(m.AuthMode)
	if mode != "" {
		return mode
	}
	if m.HasTokenSource() {
		return AuthModeBearer
	}
	return AuthModeNone
}

func (m MoltnetConfig) HasTokenSource() bool {
	return strings.TrimSpace(m.Token) != "" ||
		strings.TrimSpace(m.TokenEnv) != "" ||
		strings.TrimSpace(m.TokenPath) != ""
}

func (m MoltnetTokenConfig) HasTokenSource() bool {
	return strings.TrimSpace(m.Token) != "" ||
		strings.TrimSpace(m.TokenEnv) != "" ||
		strings.TrimSpace(m.TokenPath) != ""
}

func (m MoltnetConfig) ValidateAuth() error {
	switch m.EffectiveAuthMode() {
	case AuthModeNone:
		if m.StaticToken {
			return fmt.Errorf("bridge config moltnet.static_token requires auth_mode open")
		}
		return nil
	case AuthModeBearer:
		if !m.HasTokenSource() {
			return fmt.Errorf("bridge config moltnet.token, token_env, or token_path is required for bearer auth")
		}
		return nil
	case AuthModeOpen:
		if !m.HasTokenSource() {
			return fmt.Errorf("bridge config moltnet.token, token_env, or token_path is required for open auth")
		}
		return nil
	default:
		return fmt.Errorf("bridge config moltnet.auth_mode %q is unsupported", m.AuthMode)
	}
}

func (m MoltnetConfig) ResolveToken() (string, bool, error) {
	mode := m.EffectiveAuthMode()
	if mode == AuthModeNone {
		return "", false, nil
	}

	if m.Token != "" {
		token := strings.TrimSpace(m.Token)
		if token == "" {
			return "", false, fmt.Errorf("bridge config moltnet.token is empty")
		}
		return token, true, nil
	}

	if envName := strings.TrimSpace(m.TokenEnv); envName != "" {
		token := strings.TrimSpace(os.Getenv(envName))
		if token == "" {
			return "", false, fmt.Errorf("environment variable %q is required for Moltnet %s auth", envName, mode)
		}
		return token, true, nil
	}

	if path := strings.TrimSpace(m.TokenPath); path != "" {
		token, err := ReadTokenFile(path)
		if err != nil {
			if mode == AuthModeOpen && errors.Is(err, os.ErrNotExist) {
				return "", false, nil
			}
			return "", false, err
		}
		return token, true, nil
	}

	return "", false, fmt.Errorf("bridge config moltnet.token, token_env, or token_path is required for %s auth", mode)
}

func IsAgentToken(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), AgentTokenPrefix)
}

func resolveTokenPath(baseDir string, value string) string {
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
