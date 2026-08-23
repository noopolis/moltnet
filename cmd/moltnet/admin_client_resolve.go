package main

import (
	"flag"
	"fmt"
	"strings"

	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
)

// adminClientOptions holds the flag-bound values every admin-style command
// (admin, pair invite/revoke/list/show, room list) resolves a client from:
// see bindAdminClientResolverFlags.
type adminClientOptions struct {
	authMode   string
	baseURL    string
	configPath string
	networkID  string
	memberID   string
	token      string
	tokenEnv   string
	tokenPath  string
}

// adminClientResolution records how resolveAdminClient actually reached its
// target, so a caller performing a local-config writeback afterward (like
// admin_room_members_writeback.go) knows whether the live request it just
// made is guaranteed to have landed on the same server that writeback is
// about to open and edit.
//
// F4 (confirmed live, two-server reproduction): a follow-up writeback used
// to gate only on "the operator did not pass --base-url/--config" and then
// independently re-resolve a local server config by --network alone. That
// silently lands on the wrong file whenever a client config
// (.moltnet/config.json, MOLTNET_CLIENT_CONFIG, or the legacy
// .spawnfile/moltnet.json) exists and resolves to a different — possibly
// remote — server: the live request goes there, but the writeback would
// still guess at whatever local server config --network happened to name.
type adminClientResolution struct {
	// LocalServerConfigPath is non-empty only when resolveAdminClient
	// resolved entirely through the local-server-config fallback
	// (resolveAdminFromServerConfig): no --base-url, and no client config
	// found on disk at all. That is the only resolution path guaranteed to
	// have targeted the local server at this exact path -- an explicit
	// --base-url, or a resolved client config (which can itself point
	// anywhere), leaves this empty.
	LocalServerConfigPath string
}

func bindAdminOnlyFlags(flags *flag.FlagSet) *adminClientOptions {
	return bindAdminClientResolverFlags(flags, true)
}

func bindAdminClientResolverFlags(flags *flag.FlagSet, includeMemberFlag bool) *adminClientOptions {
	options := &adminClientOptions{}
	flags.StringVar(&options.authMode, "auth-mode", "none", "client auth mode: none, bearer, or open")
	flags.StringVar(&options.baseURL, "base-url", "", "Moltnet base URL")
	flags.StringVar(&options.configPath, "config", "", "existing Moltnet client config path")
	if includeMemberFlag {
		flags.StringVar(&options.memberID, "member", "", "Moltnet member id when reading an existing config")
	}
	flags.StringVar(&options.networkID, "network", "", "Moltnet network id when reading an existing config")
	flags.StringVar(&options.token, "token", "", "plain bearer token")
	flags.StringVar(&options.tokenEnv, "token-env", "", "environment variable containing the bearer token")
	flags.StringVar(&options.tokenPath, "token-path", "", "file containing the bearer token")
	return options
}

func bindAdminClientFlags(flags *flag.FlagSet, targetName string, targetUsage string) (*adminClientOptions, *string) {
	options := bindAdminOnlyFlags(flags)
	target := flags.String(targetName, "", targetUsage)
	return options, target
}

// resolveAdminClient is resolveAdminClientResolution with the resolution
// provenance dropped, for every admin-style caller that only needs the
// client itself (admin dm/agent, pair invite/revoke/list/show, room
// list/remove). Only runAdminRoomMembers (remove.go) needs the provenance,
// to gate its local-config writeback (F4) -- see
// resolveAdminClientResolution's doc comment.
func resolveAdminClient(flags *flag.FlagSet, options *adminClientOptions) (*moltnetclient.Client, error) {
	client, _, err := resolveAdminClientResolution(flags, options)
	return client, err
}

// resolveAdminClientResolution resolves an admin-scoped Moltnet client from,
// in order: an explicit --base-url/flags, an explicit or discovered client
// config (.moltnet/config.json and friends, loadClientConfig), or -- only
// once neither of those produced anything -- the local Moltnet *server*
// config (resolveAdminFromServerConfig). The returned adminClientResolution
// records which of those actually happened, specifically whether the
// local-server fallback fired and, if so, its exact path (see its own doc
// comment for why that matters to callers doing a local writeback
// afterward).
func resolveAdminClientResolution(flags *flag.FlagSet, options *adminClientOptions) (*moltnetclient.Client, adminClientResolution, error) {
	var resolution adminClientResolution

	attachment := clientconfig.AttachmentConfig{
		Auth: clientconfig.AuthConfig{
			Mode:      strings.TrimSpace(options.authMode),
			Token:     strings.TrimSpace(options.token),
			TokenEnv:  strings.TrimSpace(options.tokenEnv),
			TokenPath: strings.TrimSpace(options.tokenPath),
		},
		BaseURL:   strings.TrimSpace(options.baseURL),
		MemberID:  strings.TrimSpace(options.memberID),
		NetworkID: strings.TrimSpace(options.networkID),
	}

	explicitClientConfig := strings.TrimSpace(options.configPath) != ""
	if explicitClientConfig || attachment.BaseURL == "" {
		config, _, err := loadClientConfig(options.configPath)
		switch {
		case err == nil:
			resolved, resolveErr := config.ResolveAttachmentFor(options.networkID, options.memberID)
			if resolveErr != nil {
				return nil, adminClientResolution{}, resolveErr
			}
			if attachment.BaseURL == "" {
				attachment.BaseURL = resolved.BaseURL
			}
			if attachment.MemberID == "" {
				attachment.MemberID = resolved.MemberID
			}
			if attachment.NetworkID == "" {
				attachment.NetworkID = resolved.NetworkID
			}
			sourceExplicit := flagWasSet(flags, "token") ||
				flagWasSet(flags, "token-env") ||
				flagWasSet(flags, "token-path")
			attachment.Auth = mergeAuthFromConfig(resolved.Auth, attachment.Auth, flagWasSet(flags, "auth-mode"), sourceExplicit)
			// A resolved client config is never treated as "local" for
			// writeback purposes (F4): it can point at any server,
			// including a remote one, regardless of whether --base-url or
			// --config was passed on the command line.
		case explicitClientConfig:
			return nil, adminClientResolution{}, err
		default:
			// No client config (.moltnet/config.json) on disk. Before giving
			// up, fall back to the local Moltnet *server* config (PLAN.md
			// phase 4's shared config resolution), deriving --base-url and an
			// admin-scoped token from it — --network doubles as the network
			// id to disambiguate ~/.moltnet/ when several networks exist,
			// exactly like start/pair/relay's --id.
			if attachment.BaseURL == "" {
				derived, derivedPath, ok, derivedErr := resolveAdminFromServerConfig(options.networkID)
				if derivedErr != nil {
					return nil, adminClientResolution{}, derivedErr
				}
				if ok {
					attachment.BaseURL = derived.BaseURL
					if attachment.NetworkID == "" {
						attachment.NetworkID = derived.NetworkID
					}
					sourceExplicit := flagWasSet(flags, "token") ||
						flagWasSet(flags, "token-env") ||
						flagWasSet(flags, "token-path")
					attachment.Auth = mergeAuthFromConfig(derived.Auth, attachment.Auth, flagWasSet(flags, "auth-mode"), sourceExplicit)
					resolution.LocalServerConfigPath = derivedPath
				}
			}
		}
	}

	if attachment.BaseURL == "" {
		return nil, adminClientResolution{}, fmt.Errorf("admin command requires --base-url or --config")
	}
	if strings.TrimSpace(attachment.Auth.Mode) == "" || strings.TrimSpace(attachment.Auth.Mode) == bridgeconfig.AuthModeNone {
		if attachment.Auth.HasTokenSource() {
			attachment.Auth.Mode = bridgeconfig.AuthModeBearer
		}
	}

	client, err := moltnetclient.New(attachment)
	if err != nil {
		return nil, adminClientResolution{}, err
	}
	return client, resolution, nil
}
