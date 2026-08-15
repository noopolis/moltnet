package main

import (
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
)

// resolveAdminFromServerConfig is admin's config-resolution fallback
// (PLAN.md phase 4 item 1): when neither --base-url nor a client config
// (.moltnet/config.json) is available, derive an attachment from the local
// Moltnet *server* config instead — the same order start/pair/relay use
// (app.ResolveConfigPath): explicit wins outright; with --network given,
// ~/.moltnet/<id>/Moltnet is resolved first, falling back to cwd only when
// its config self-identifies as network id id; with neither, ./Moltnet in
// cwd, then the sole network under ~/.moltnet/. ok is false, with a nil
// error, only when no server config exists anywhere in that order either;
// admin commands then fall through to their normal "--base-url or --config"
// error.
func resolveAdminFromServerConfig(networkID string) (clientconfig.AttachmentConfig, bool, error) {
	path, found, err := app.ResolveConfigPath("", strings.TrimSpace(networkID))
	if err != nil {
		return clientconfig.AttachmentConfig{}, false, err
	}
	if !found {
		return clientconfig.AttachmentConfig{}, false, nil
	}

	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		return clientconfig.AttachmentConfig{}, false, err
	}

	return clientconfig.AttachmentConfig{
		BaseURL:   localBaseURLHint(config),
		NetworkID: config.NetworkID,
		Auth:      adminAuthFromServerConfig(config),
	}, true, nil
}

// adminAuthFromServerConfig picks the first admin-scoped token with a
// resolvable plaintext value out of config.Auth.Tokens. It is safe to hand
// this value to moltnetclient: the config file on disk already holds it in
// plaintext (the same source `pair invite`'s membership-command hint reads).
func adminAuthFromServerConfig(config app.Config) clientconfig.AuthConfig {
	for _, token := range config.Auth.Tokens {
		value := strings.TrimSpace(token.Value)
		if value == "" {
			continue
		}
		for _, scope := range token.Scopes {
			if string(scope) == "admin" {
				return clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeBearer, Token: value}
			}
		}
	}
	return clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeNone}
}
