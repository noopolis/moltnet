package main

import (
	"flag"
	"fmt"
	"strings"

	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
)

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

func runRemoveAgent(args []string) error {
	flags := flag.NewFlagSet("moltnet remove-agent", flag.ContinueOnError)
	flags.SetOutput(stdout)

	options, agentID := bindAdminClientFlags(flags, "agent", "agent id to remove")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("remove-agent does not accept positional arguments")
	}
	if strings.TrimSpace(*agentID) == "" {
		return fmt.Errorf("remove-agent requires --agent")
	}

	client, err := resolveAdminClient(flags, options)
	if err != nil {
		return err
	}
	result, err := client.RemoveAgent(commandContext(), strings.TrimSpace(*agentID))
	if err != nil {
		return err
	}
	return printJSON(result)
}

func runRemoveRoom(args []string) error {
	flags := flag.NewFlagSet("moltnet remove-room", flag.ContinueOnError)
	flags.SetOutput(stdout)

	options, roomID := bindAdminClientFlags(flags, "room", "room id to remove")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("remove-room does not accept positional arguments")
	}
	if strings.TrimSpace(*roomID) == "" {
		return fmt.Errorf("remove-room requires --room")
	}

	client, err := resolveAdminClient(flags, options)
	if err != nil {
		return err
	}
	result, err := client.RemoveRoom(commandContext(), strings.TrimSpace(*roomID))
	if err != nil {
		return err
	}
	return printJSON(result)
}

func bindAdminClientFlags(flags *flag.FlagSet, targetName string, targetUsage string) (*adminClientOptions, *string) {
	options := &adminClientOptions{}
	target := flags.String(targetName, "", targetUsage)
	flags.StringVar(&options.authMode, "auth-mode", "none", "client auth mode: none, bearer, or open")
	flags.StringVar(&options.baseURL, "base-url", "", "Moltnet base URL")
	flags.StringVar(&options.configPath, "config", "", "existing Moltnet client config path")
	flags.StringVar(&options.memberID, "member", "", "Moltnet member id when reading an existing config")
	flags.StringVar(&options.networkID, "network", "", "Moltnet network id when reading an existing config")
	flags.StringVar(&options.token, "token", "", "plain bearer token")
	flags.StringVar(&options.tokenEnv, "token-env", "", "environment variable containing the bearer token")
	flags.StringVar(&options.tokenPath, "token-path", "", "file containing the bearer token")
	return options, target
}

func resolveAdminClient(flags *flag.FlagSet, options *adminClientOptions) (*moltnetclient.Client, error) {
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

	if strings.TrimSpace(options.configPath) != "" || attachment.BaseURL == "" {
		config, _, err := loadClientConfig(options.configPath)
		if err != nil {
			return nil, err
		}
		resolved, err := config.ResolveAttachmentFor(options.networkID, options.memberID)
		if err != nil {
			return nil, err
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
	}

	if attachment.BaseURL == "" {
		return nil, fmt.Errorf("admin command requires --base-url or --config")
	}
	if strings.TrimSpace(attachment.Auth.Mode) == "" || strings.TrimSpace(attachment.Auth.Mode) == bridgeconfig.AuthModeNone {
		if attachment.Auth.HasTokenSource() {
			attachment.Auth.Mode = bridgeconfig.AuthModeBearer
		}
	}

	return moltnetclient.New(attachment)
}
