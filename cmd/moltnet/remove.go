package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
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

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedStringFlag) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			*f = append(*f, trimmed)
		}
	}
	return nil
}

func runApply(args []string) error {
	flags := flag.NewFlagSet("moltnet apply", flag.ContinueOnError)
	flags.SetOutput(stdout)

	options := bindAdminOnlyFlags(flags)
	flagArgs, path, err := splitApplyArgs(args)
	if err != nil {
		return err
	}
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	request, _, err := app.LoadApplyFile(path)
	if err != nil {
		return err
	}
	client, err := resolveAdminClient(flags, options)
	if err != nil {
		return err
	}
	result, err := client.ApplyConfig(commandContext(), request)
	if err != nil {
		return err
	}
	return printJSON(result)
}

func splitApplyArgs(args []string) ([]string, string, error) {
	flagArgs := make([]string, 0, len(args))
	var path string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--auth-mode" ||
			arg == "--base-url" ||
			arg == "--config" ||
			arg == "--member" ||
			arg == "--network" ||
			arg == "--token" ||
			arg == "--token-env" ||
			arg == "--token-path" {
			if index+1 >= len(args) {
				return nil, "", fmt.Errorf("flag %s requires a value", arg)
			}
			flagArgs = append(flagArgs, arg, args[index+1])
			index++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			continue
		}
		if path != "" {
			return nil, "", fmt.Errorf("apply accepts at most one config path")
		}
		path = arg
	}
	return flagArgs, path, nil
}

// isHelpArg reports whether arg is one of the three spellings this CLI's
// subcommand dispatchers (admin, pair, relay, service, skill, uninstall,
// console) treat as a help request before ever reaching a flag.FlagSet —
// "help", "--help", and "-h". Centralizing the three-way check here means a
// dispatcher can't accidentally recognize only "help" (a real bug: a router
// with unrecognized-argument behavior fails when an operator naturally
// types the same "--help"/"-h" every one of this CLI's flag.FlagSet-backed
// leaf commands already accepts).
func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func runAdminCommand(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stdout, buildAdminUsage())
		return nil
	}

	switch args[0] {
	case "agent":
		return runAdminAgentCommand(args[1:])
	case "dm":
		return runAdminDMCommand(args[1:])
	case "room":
		return runAdminRoomCommand(args[1:])
	default:
		return fmt.Errorf("unknown admin command %q\n\n%s", args[0], buildAdminUsage())
	}
}

func runAdminAgentCommand(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stdout, buildAdminUsage())
		return nil
	}
	if args[0] != "remove" {
		return fmt.Errorf("unknown admin agent command %q\n\n%s", args[0], buildAdminUsage())
	}
	return runAdminRemoveAgent(args[1:])
}

func runAdminRoomCommand(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stdout, buildAdminUsage())
		return nil
	}
	switch args[0] {
	case "remove":
		return runAdminRemoveRoom(args[1:])
	case "members":
		return runAdminRoomMembers(args[1:])
	default:
		return fmt.Errorf("unknown admin room command %q\n\n%s", args[0], buildAdminUsage())
	}
}

func runAdminRemoveAgent(args []string) error {
	flags := flag.NewFlagSet("moltnet admin agent remove", flag.ContinueOnError)
	flags.SetOutput(stdout)

	options, agentID := bindAdminClientFlags(flags, "agent", "agent id to remove")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("admin agent remove does not accept positional arguments")
	}
	if strings.TrimSpace(*agentID) == "" {
		return fmt.Errorf("admin agent remove requires --agent")
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

func runAdminRemoveRoom(args []string) error {
	flags := flag.NewFlagSet("moltnet admin room remove", flag.ContinueOnError)
	flags.SetOutput(stdout)

	options, roomID := bindAdminClientFlags(flags, "room", "room id to remove")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("admin room remove does not accept positional arguments")
	}
	if strings.TrimSpace(*roomID) == "" {
		return fmt.Errorf("admin room remove requires --room")
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

func runAdminRoomMembers(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stdout, buildAdminUsage())
		return nil
	}
	action := args[0]
	if action != "add" && action != "remove" {
		return fmt.Errorf("unknown admin room members command %q\n\n%s", action, buildAdminUsage())
	}

	flags := flag.NewFlagSet("moltnet admin room members "+action, flag.ContinueOnError)
	flags.SetOutput(stdout)
	options := bindAdminClientResolverFlags(flags, false)
	roomID := flags.String("room", "", "room id to update")
	var members repeatedStringFlag
	flags.Var(&members, "member", "member id to add or remove; may be repeated or comma-separated")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("admin room members %s does not accept positional arguments", action)
	}
	if strings.TrimSpace(*roomID) == "" {
		return fmt.Errorf("admin room members %s requires --room", action)
	}
	if len(members) == 0 {
		return fmt.Errorf("admin room members %s requires --member", action)
	}

	request := protocol.UpdateRoomMembersRequest{}
	switch action {
	case "add":
		request.Add = []string(members)
	case "remove":
		request.Remove = []string(members)
	}
	client, err := resolveAdminClient(flags, options)
	if err != nil {
		return err
	}
	room, err := client.UpdateRoomMembers(commandContext(), strings.TrimSpace(*roomID), request)
	if err != nil {
		return err
	}
	return printJSON(room)
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

	explicitClientConfig := strings.TrimSpace(options.configPath) != ""
	if explicitClientConfig || attachment.BaseURL == "" {
		config, _, err := loadClientConfig(options.configPath)
		switch {
		case err == nil:
			resolved, resolveErr := config.ResolveAttachmentFor(options.networkID, options.memberID)
			if resolveErr != nil {
				return nil, resolveErr
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
		case explicitClientConfig:
			return nil, err
		default:
			// No client config (.moltnet/config.json) on disk. Before giving
			// up, fall back to the local Moltnet *server* config (PLAN.md
			// phase 4's shared config resolution), deriving --base-url and an
			// admin-scoped token from it — --network doubles as the network
			// id to disambiguate ~/.moltnet/ when several networks exist,
			// exactly like start/pair/relay's --id.
			if attachment.BaseURL == "" {
				derived, ok, derivedErr := resolveAdminFromServerConfig(options.networkID)
				if derivedErr != nil {
					return nil, derivedErr
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
				}
			}
		}
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
