package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/protocol"
)

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

// runAdminRemoveRoom implements the deprecated "moltnet admin room remove"
// entry point (PLAN.md 7A.0's namespace decision (a) moved room verbs to the
// top-level "room" namespace and kept this one as an alias that still
// works). It prints a one-line deprecation note, then delegates to
// removeRoomCommand for the actual work — the same implementation
// "moltnet room remove" (room.go) calls directly, without the note, since it
// is not the deprecated spelling.
func runAdminRemoveRoom(args []string) error {
	fmt.Fprintln(stderr, "deprecated: \"moltnet admin room remove\" is now \"moltnet room remove\" (both still work identically)")
	return removeRoomCommand("moltnet admin room remove", args)
}

// removeRoomCommand is the shared implementation behind both
// "moltnet room remove" and the deprecated "moltnet admin room remove": it
// resolves an admin-scoped client (bindAdminClientFlags/resolveAdminClientResolution,
// identical to every other admin command) and calls DELETE /v1/rooms/{id}
// (moltnetclient.RemoveRoom). commandName only affects the flag.FlagSet's
// own usage/error prefix, so each entry point's error messages still name
// the command the caller actually typed.
//
// P0-1 (final-gate review): the live DELETE alone leaves a config-declared
// room's rooms[] entry in place, which reliably bricks the network's next
// restart (see roomRemoveConfigWriteback's doc comment, room_remove_writeback.go,
// for the exact mechanism). resolveAdminClientResolution (rather than
// resolveAdminClient) is used here so the aftercare writeback can reuse the
// resolution's own LocalServerConfigPath -- the same F4 discipline
// runAdminRoomMembers (below) already follows -- instead of re-deriving a
// "local" config path independently and risking it diverging from wherever
// the live request above actually went.
func removeRoomCommand(commandName string, args []string) error {
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(stdout)

	options, roomID := bindAdminClientFlags(flags, "room", "room id to remove")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s does not accept positional arguments", commandName)
	}
	trimmedRoomID := strings.TrimSpace(*roomID)
	if trimmedRoomID == "" {
		return fmt.Errorf("%s requires --room", commandName)
	}

	client, resolution, err := resolveAdminClientResolution(flags, options)
	if err != nil {
		return err
	}
	result, err := client.RemoveRoom(commandContext(), trimmedRoomID)
	if err != nil {
		return err
	}

	roomRemoveConfigWriteback(resolution.LocalServerConfigPath, trimmedRoomID)

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
	client, resolution, err := resolveAdminClientResolution(flags, options)
	if err != nil {
		return err
	}
	room, err := client.UpdateRoomMembers(commandContext(), strings.TrimSpace(*roomID), request)
	if err != nil {
		return err
	}

	// 7B.5: persist the change into the local Moltnet server config, if this
	// command resolved one, so it survives the next restart instead of being
	// silently reconciled away (see
	// internal/app/config_writeback_membership.go's doc comment).
	//
	// F4 (confirmed live): this used to gate on "--base-url/--config were not
	// passed" and then independently re-resolve a local server config by
	// --network alone. That is wrong whenever a client config
	// (.moltnet/config.json, MOLTNET_CLIENT_CONFIG, or the legacy
	// .spawnfile/moltnet.json) exists and points the live request above at a
	// different — possibly remote — server: the PATCH above lands on that
	// client config's target, but the writeback would land on whatever local
	// server config --network happened to name, silently. resolution.LocalServerConfigPath
	// (admin_client_resolve.go) is non-empty only when resolveAdminClient
	// itself resolved through the local-server-config fallback — no
	// --base-url and no client config found at all — which is the only case
	// a follow-up write to that same path is guaranteed to target the server
	// the live request just went to.
	adminRoomMembersConfigWriteback(resolution.LocalServerConfigPath, strings.TrimSpace(*roomID), request.Add, request.Remove)

	return printJSON(room)
}
