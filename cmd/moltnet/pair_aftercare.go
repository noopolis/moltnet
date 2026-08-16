package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// pairMembershipCommand builds the exact `moltnet admin room members add`
// invocation both pair aftercare printers below hand out for room, naming
// memberSpec (a "<network>:<member>" pair, real or placeholder depending on
// what each side already knows) as the member to admit and networkID as the
// local network the command should run against.
//
// The command deliberately omits --base-url and --token: naming --base-url
// would skip resolveAdminClient's local-config fallback
// (admin_config_fallback.go), which only engages when neither --base-url
// nor a client config is given, so leaving both out lets a same-machine
// `moltnet admin` command derive the base URL and admin token from this
// network's own server config automatically. A remote operator has no local
// server config to derive either from, and --network only resolves a
// *local* config (it is meaningless off-machine), hence the --verbose-only
// "(remote? ...)" aside both callers print alongside it (pairRemoteAdminNote
// below): add --base-url and --token-env explicitly, and drop --network.
func pairMembershipCommand(room, memberSpec, networkID string) string {
	return fmt.Sprintf("moltnet admin room members add --room %s --member %s --network %s", room, memberSpec, networkID)
}

// pairMembershipRoomLabel renders the shared rooms a pairing declared as the
// short English phrase both aftercare printers below name in their
// membership-guidance sentence: "room \"chat\"" for one, "rooms \"chat\",
// \"news\"" for several.
func pairMembershipRoomLabel(rooms []string) string {
	if len(rooms) == 1 {
		return fmt.Sprintf("room %q", rooms[0])
	}
	quoted := make([]string, len(rooms))
	for i, room := range rooms {
		quoted[i] = strconv.Quote(room)
	}
	return "rooms " + strings.Join(quoted, ", ")
}

// pairRemoteAdminNote is the --verbose-only dim aside both aftercare
// printers below append under their membership command(s): a reminder that
// a remote operator (not running the command from this same machine) needs
// --base-url/--token-env instead of --network.
func pairRemoteAdminNote() string {
	return dim("(remote? add --base-url <url> --token-env MOLTNET_ADMIN_TOKEN and drop --network)")
}

// printPairInviteAftercare prints the aftercare `moltnet pair invite` ends
// with: a single "pairing ready" checkmark, the (--verbose-only) status
// detail block (printPairStatusBlock), the invite as ONE copyable command —
// the full `moltnet pair '<code>'` a friend can paste and run, single-quoted
// so it is paste-safe, bolded as one unmissable paragraph — and, when this
// pairing declared shared rooms, a single "next:" line naming the membership
// command to run once the friend has paired (one line per declared room,
// when there is more than one — see printNextStepList's doc comment for why
// that is not a menu of alternatives).
//
// remoteNetworkID/remoteNetworkName in opts are unused here: `pair invite`
// never learns the friend's identity without a round trip, so its
// membership command below names friend-side placeholders instead of real
// ids; `pair <code>` on the friend's side (printPairJoinAftercare) prints
// the real, filled-in command.
//
// The invite command prints unconditionally, on every path where the
// pairing was actually written — restart status is gathered first
// (printPairStatusBlock never itself errors, see its doc comment) but any
// real restart failure it carries in the returned restartOutcome is only
// returned as this function's error at the very end, after the command and
// the rest of aftercare have already printed. This is the P0 fix: a
// `--restart` failure used to be able to swallow the code entirely by
// returning before it ever printed.
func printPairInviteAftercare(ctx context.Context, config app.Config, path, code, pairingID string, opts pairAftercareOptions) error {
	printInitConfigCheckLine("pairing ready", "")
	outcome := printPairStatusBlock(ctx, config, path, pairingID, opts.restart, opts.verbose)

	days := int(protocol.DefaultInviteTTL.Hours() / 24)
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "  share this with your friend — expires in %d days\n\n", days)
	fmt.Fprintf(stdout, "    %s\n", bold(fmt.Sprintf("moltnet pair '%s'", code)))

	rooms := protocol.UniqueTrimmedStrings(opts.roomIDs)
	if len(rooms) == 0 {
		return outcome.err
	}

	steps := make([]nextStep, len(rooms))
	for i, room := range rooms {
		steps[i] = nextStep{
			command:     pairMembershipCommand(room, "<their-network-id>:<their-agent-id>", config.NetworkID),
			description: "grant access once they've paired",
		}
	}
	printNextStepList(steps)
	if opts.verbose {
		fmt.Fprintf(stdout, "    %s\n", pairRemoteAdminNote())
	}

	return outcome.err
}

// printPairJoinAftercare prints the aftercare `moltnet pair <code>` ends
// with: a single "paired with <network>" checkmark naming the sender's real
// network id, the (--verbose-only) status detail block
// (printPairStatusBlock), and, only when the invite declared shared rooms,
// a single "next:" line per room naming the fully filled-in
// `moltnet admin room members add` command (the sender's agent id is still
// a placeholder, `<their-agent-id>` — the same placeholder vocabulary
// printPairInviteAftercare uses on the other side — since it is not carried
// by the invite, only their network id is).
//
// Like printPairInviteAftercare, any real restart failure carried in the
// status block's returned restartOutcome is only returned as this
// function's error at the very end, after the rest of aftercare has
// printed — the same P0 ordering fix, applied on the joiner side too.
func printPairJoinAftercare(ctx context.Context, config app.Config, path, pairingID string, opts pairAftercareOptions) error {
	remote := strings.TrimSpace(opts.remoteNetworkID)
	printInitConfigCheckLine(fmt.Sprintf("paired with %s", remote), "")
	outcome := printPairStatusBlock(ctx, config, path, pairingID, opts.restart, opts.verbose)

	rooms := protocol.UniqueTrimmedStrings(opts.roomIDs)
	if len(rooms) == 0 {
		return outcome.err
	}

	steps := make([]nextStep, len(rooms))
	for i, room := range rooms {
		steps[i] = nextStep{
			command:     pairMembershipCommand(room, remote+":<their-agent-id>", config.NetworkID),
			description: "grant their agent access",
		}
	}
	printNextStepList(steps)
	if opts.verbose {
		remoteLabel := strings.TrimSpace(opts.remoteNetworkName)
		if remoteLabel == "" {
			remoteLabel = remote
		}
		fmt.Fprintln(stdout, "    (swap <their-agent-id> for the agent id they'll post as)")
		fmt.Fprintf(stdout, "    (ask whoever runs %s to run the mirrored command for your agent on their side)\n", remoteLabel)
		fmt.Fprintf(stdout, "    %s\n", pairRemoteAdminNote())
	}

	return outcome.err
}
