package main

import (
	"context"
	"fmt"
	"strings"
)

// runRoomCommand dispatches `moltnet room create|list|remove|join`
// (PLAN.md 7A.0's namespace decision (a)): room verbs live at this top-level
// namespace now, not under `admin room`. `room remove` calls
// removeRoomCommand (remove.go) directly — the same implementation the
// deprecated `admin room remove` alias calls, minus its deprecation note —
// one implementation, two entry points. `room list` is a thin alias over
// the existing rooms listing (runRoomList, room_list.go); PLAN.md's Phase 7
// "deliberately not built" list is explicit that a third listing command
// must not exist alongside it and `conversations`.
func runRoomCommand(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stdout, buildRoomUsage())
		return nil
	}

	switch args[0] {
	case "create":
		return runRoomCreate(ctx, args[1:])
	case "list":
		return runRoomList(args[1:])
	case "remove":
		return removeRoomCommand("moltnet room remove", args[1:])
	case "join":
		return runRoomJoin(args[1:])
	default:
		return fmt.Errorf("unknown room command %q\n\n%s", args[0], buildRoomUsage())
	}
}

// splitSingleRoomIDArgs separates args into flag arguments (for a
// flag.FlagSet) and a single leading-or-trailing positional room id, given
// that PLAN.md's own usage text puts <id> right after the subcommand name
// ("moltnet room create <id> [--flags...]") while Go's stdlib flag package
// only supports flags *before* positionals (it stops parsing at the first
// non-flag token). valueFlags names every flag that consumes the following
// token as its value — mirroring splitApplyArgs/splitPairJoinArgs'
// established pattern in this package — so a bare boolean flag (there are
// none on room create/join today, but -h/--help always needs this) is never
// mistaken for one that eats the next argument.
func splitSingleRoomIDArgs(args []string, valueFlags map[string]bool, commandName string) ([]string, string, error) {
	flagArgs := make([]string, 0, len(args))
	roomID := ""

	for index := 0; index < len(args); index++ {
		arg := args[index]
		if valueFlags[arg] {
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
		if roomID != "" {
			return nil, "", fmt.Errorf("%s accepts exactly one room id", commandName)
		}
		roomID = arg
	}

	return flagArgs, roomID, nil
}

// roomCreateValueFlags lists every flag.String/repeatedStringFlag `room
// create` binds — see splitSingleRoomIDArgs' doc comment for why this must
// stay in sync with runRoomCreate's flag definitions (room_create.go).
var roomCreateValueFlags = map[string]bool{
	"--name":         true,
	"--visibility":   true,
	"--write-policy": true,
	"--federation":   true,
	"--member":       true,
	"--credential":   true,
	"--network":      true,
	"--base-url":     true,
	"--token-env":    true,
}

// roomJoinValueFlags lists every flag `room join` binds — see
// splitSingleRoomIDArgs' doc comment; must stay in sync with runRoomJoin's
// flag definitions (room_join.go).
var roomJoinValueFlags = map[string]bool{
	"--config":         true,
	"--member":         true,
	"--network":        true,
	"--credential-env": true,
	"--base-url":       true,
	"--token-env":      true,
}
