package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-isatty"
	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/service"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func runPairCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, buildPairUsage())
		return errors.New("pair requires an invite code, or the invite subcommand")
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, buildPairUsage())
		return nil
	}
	if args[0] == "invite" {
		return runPairInvite(ctx, args[1:])
	}

	return runPairJoin(ctx, args)
}

// resolvePairConfigPath discovers the Moltnet server config that a
// pair/relay command should read and write (app.ResolveConfigPath) — the
// same order `moltnet start` and `moltnet admin` use: explicit --config
// wins outright; with id given, ~/.moltnet/<id>/Moltnet is resolved first,
// falling back to cwd only when its config self-identifies as network id
// id; with neither, ./Moltnet in cwd, then the sole network directory under
// ~/.moltnet/.
func resolvePairConfigPath(explicit string, id string) (string, error) {
	path, ok, err := app.ResolveConfigPath(explicit, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("moltnet config not found; run `moltnet init` first: %w", err)
		}
		return "", err
	}
	if !ok {
		return "", errors.New("moltnet config not found; run `moltnet init` first")
	}
	return path, nil
}

// writePairingWithRollback writes pairing, authToken, and any invite-named
// shared rooms into the Moltnet config at path, then re-runs the same full
// config load the server uses at startup (env-merge included) as a
// post-write check. If that reload fails, the file is restored to its prior
// contents so a bad write never lingers.
func writePairingWithRollback(path string, pairing app.PairingWriteback, authToken app.AuthTokenWriteback, roomIDs []string, force bool) error {
	snapshot, err := snapshotFile(path)
	if err != nil {
		return err
	}

	if err := app.WritePairing(path, pairing, authToken, roomIDs, force); err != nil {
		return err
	}

	if _, err := app.LoadConfigForPath(path, ""); err != nil {
		if restoreErr := snapshot.restore(path); restoreErr != nil {
			return fmt.Errorf("wrote pairing but the config failed to reload (%v); restore also failed: %w", err, restoreErr)
		}
		return fmt.Errorf("wrote pairing but the config failed to reload; rolled back: %w", err)
	}

	return nil
}

// pairAftercareOptions bundles what the two pair aftercare printers below
// need beyond the loaded config: whether --restart was requested, the
// shared room ids the pairing declared (for the membership-command
// guidance, PLAN.md phase 4 item 3), and — join only — the remote
// network's id and display name learned straight from the invite (`pair
// invite` never round-trips these; it prints friend-side placeholders
// instead, see printPairInviteAftercare).
type pairAftercareOptions struct {
	restart           bool
	roomIDs           []string
	remoteNetworkID   string
	remoteNetworkName string
}

// pairStatusColumn is the column plain (unstyled) "wrote pairing" status
// text pads to before its abbreviated path annotation. It mirrors
// configLineColumn's *pattern* for `moltnet init` (init_summary.go) — pad
// the plain-width prefix to a fixed column before the dimmed path
// annotation — not its value: pair's own prefix text is a different length,
// so its column is its own number (44 here vs. configLineColumn's 24), not
// a copy of it.
const pairStatusColumn = 44

// printPairWroteLine prints the green "✓ wrote pairing" status line both
// pair commands open their aftercare with, its config path abbreviated to
// "~" via abbreviateHome (init_summary.go, the shared house-style helper)
// and dimmed, column-aligned to pairStatusColumn. Width is computed from
// the plain (unstyled) prefix, matching printInitConfigCheckLine's
// (init_summary.go) and formatNextStep's (nextsteps.go) pattern, so
// alignment stays correct whether or not styling is active.
func printPairWroteLine(id, path string) {
	what := fmt.Sprintf("wrote pairing %q", id)
	plainPrefix := "  ✓ " + what
	displayPrefix := "  " + green("✓") + " " + what
	abbrev := dim(abbreviateHome(path))
	if width := utf8.RuneCountInString(plainPrefix); width < pairStatusColumn {
		fmt.Fprintln(stdout, displayPrefix+strings.Repeat(" ", pairStatusColumn-width)+abbrev)
		return
	}
	fmt.Fprintln(stdout, displayPrefix+"  "+abbrev)
}

// printPairRestartLine prints the restart half of the status block: a green
// "✓ restarted the service for <network>" confirmation (--restart
// succeeded); a yellow warning plus the ordinary manual-restart reminder for
// either "did not restart" outcome — no managed service installed for this
// network at all (outcome.missingService), or the service *is* installed
// but the restart command itself failed (outcome.err, see restartOutcome) —
// so the operator sees what went wrong immediately even though outcome.err
// is only *propagated* by the caller later, after the rest of aftercare
// prints; or, when --restart was not requested at all, the plain reminder,
// preceded by a --restart tip when the session is interactive (PLAN.md
// phase 4 item 3's "offer only if trivially detectable" clause).
func printPairRestartLine(networkID string, outcome restartOutcome, restartRequested bool) {
	if outcome.restarted {
		fmt.Fprintf(stdout, "  %s restarted the service for %s\n", green("✓"), networkID)
		return
	}
	if outcome.missingService {
		fmt.Fprintf(stdout, "  %s no managed service installed for %s; --restart had nothing to restart\n", yellow("warning:"), networkID)
		fmt.Fprintln(stdout, "  restart the Moltnet server for this pairing to take effect")
		return
	}
	if outcome.err != nil {
		fmt.Fprintf(stdout, "  %s %v\n", yellow("warning:"), outcome.err)
		fmt.Fprintln(stdout, "  restart the Moltnet server for this pairing to take effect")
		return
	}
	if !restartRequested && isInteractive() {
		fmt.Fprintf(stdout, "  %s rerun with --restart to restart the managed moltnet service now\n", yellow("tip:"))
	}
	fmt.Fprintln(stdout, "  restart the Moltnet server for this pairing to take effect")
}

// printPairStatusBlock prints the status lines both pair commands' aftercare
// opens with, verbatim: the wrote-pairing checkmark, the restart
// confirmation/warning/reminder (printPairRestartLine), and (when the
// pairing token would not yet be enforced) the auth.mode note. It never
// itself returns an error: a --restart failure of either kind is reported
// inline as part of the block (see printPairRestartLine) and also carried
// out in the returned restartOutcome, so a caller that wants to fail the
// command on a *real* restart failure (never on a merely missing service —
// see restartOutcome) can propagate outcome.err itself, once every
// remaining aftercare line — most importantly, on the invite side, the
// invite code — has already printed. This is the P0 fix: printing the
// status block used to be able to return an error before anything after it
// (including the code) ever printed.
func printPairStatusBlock(ctx context.Context, config app.Config, path, pairingID string, restart bool) restartOutcome {
	printPairWroteLine(pairingID, path)

	outcome := maybeRestartService(ctx, config.NetworkID, restart)
	printPairRestartLine(config.NetworkID, outcome, restart)

	if config.Auth.Mode != "bearer" && config.Auth.Mode != "open" {
		fmt.Fprintf(stdout, "  %s auth.mode in %s is %q; set it to \"bearer\" so the pairing token is enforced\n",
			yellow("note:"), abbreviateHome(path), config.Auth.Mode)
	}

	return outcome
}

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
// *local* config (it is meaningless off-machine), hence the dim "(remote?
// ...)" sub-line both callers print alongside it: add --base-url and
// --token-env explicitly, and drop --network.
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

// pairRemoteAdminNote is the dim sub-line both aftercare printers below
// append under their membership command(s): a reminder that a remote
// operator (not running the command from this same machine) needs
// --base-url/--token-env instead of --network. Demoted to a dim aside
// rather than a bare "(remote? ...)" line so it reads as optional guidance,
// not a required incantation.
func pairRemoteAdminNote() string {
	return dim("(remote? add --base-url <url> --token-env MOLTNET_ADMIN_TOKEN and drop --network)")
}

// printPairInviteAftercare prints the aftercare `moltnet pair invite` ends
// with: the shared status block (printPairStatusBlock), then the invite
// code as its own centered, indented paragraph — the entire point of the
// command, so it gets nothing else on its lines — followed by a numbered
// "Then:" sequence telling the inviter what happens next: the friend runs
// `moltnet pair <code>` on their side (--restart mentioned only as a
// conditional add-on — see step 1 below — since most joiners are not
// running the server as a service yet), and (only when this pairing
// declared shared rooms) a later, explained step naming the membership
// command to run once the friend's network and member ids are known.
// remoteNetworkID/remoteNetworkName in opts are unused here: `pair invite`
// never learns the friend's identity without a round trip, so its
// membership command below names friend-side placeholders instead of real
// ids; `pair <code>` on the friend's side (printPairJoinAftercare) prints
// the real, filled-in command.
//
// The invite code prints unconditionally, on every path where the pairing
// was actually written — restart status is gathered first (printPairStatusBlock
// never itself errors, see its doc comment) but any real restart failure it
// carries in the returned restartOutcome is only returned as this
// function's error at the very end, after the code and the rest of
// aftercare have already printed. This is the P0 fix: a `--restart` failure
// used to be able to swallow the code entirely by returning before it ever
// printed.
func printPairInviteAftercare(ctx context.Context, config app.Config, path, code, pairingID string, opts pairAftercareOptions) error {
	outcome := printPairStatusBlock(ctx, config, path, pairingID, opts.restart)

	days := int(protocol.DefaultInviteTTL.Hours() / 24)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "  Send this invite to your friend — private channel only; it's a")
	fmt.Fprintf(stdout, "  credential and expires in %d days:\n\n", days)
	fmt.Fprintf(stdout, "    %s\n\n", bold(code))

	fmt.Fprintln(stdout, "  Then:")
	fmt.Fprintf(stdout, "    1. they run   %s\n", bold("moltnet pair '<the invite above>'"))
	fmt.Fprintln(stdout, "       (add --restart if they run it as a service)")

	rooms := protocol.UniqueTrimmedStrings(opts.roomIDs)
	if len(rooms) == 0 {
		return outcome.err
	}

	fmt.Fprintf(stdout, "    2. after they've paired, grant their agent access to %s\n", pairMembershipRoomLabel(rooms))
	fmt.Fprintln(stdout, "       (room writes need membership):")
	for _, room := range rooms {
		cmd := pairMembershipCommand(room, "<their-network-id>:<their-agent-id>", config.NetworkID)
		fmt.Fprintf(stdout, "         %s\n", bold(cmd))
	}
	fmt.Fprintln(stdout, "       (you'll know both ids once they've paired; they run the mirrored")
	fmt.Fprintln(stdout, "        command for your agent on their side)")
	fmt.Fprintf(stdout, "       %s\n", pairRemoteAdminNote())

	return outcome.err
}

// printPairJoinAftercare prints the aftercare `moltnet pair <code>` ends
// with: the shared status block (printPairStatusBlock), then — mirroring
// printPairInviteAftercare's narrative on the friend's side — a "you're
// paired with <network>" confirmation naming the sender's real network id,
// and, only when the invite declared shared rooms, the exact, fully filled
// in `moltnet admin room members add` command as the one remaining step
// (the sender's agent id is still a placeholder, `<their-agent-id>` — the
// same placeholder vocabulary printPairInviteAftercare uses on the other
// side — since it is not carried by the invite, only their network id is;
// a one-line explainer right under the command says what to swap it for).
//
// Like printPairInviteAftercare, any real restart failure carried in the
// status block's returned restartOutcome is only returned as this
// function's error at the very end, after the rest of aftercare has
// printed — the same P0 ordering fix, applied on the joiner side too.
func printPairJoinAftercare(ctx context.Context, config app.Config, path, pairingID string, opts pairAftercareOptions) error {
	outcome := printPairStatusBlock(ctx, config, path, pairingID, opts.restart)

	remote := strings.TrimSpace(opts.remoteNetworkID)
	rooms := protocol.UniqueTrimmedStrings(opts.roomIDs)

	if len(rooms) == 0 {
		fmt.Fprintf(stdout, "\n  You're paired with %s.\n", remote)
		return outcome.err
	}

	remoteLabel := strings.TrimSpace(opts.remoteNetworkName)
	if remoteLabel == "" {
		remoteLabel = remote
	}

	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "  You're paired with %s; last step: grant their agent access to\n", remote)
	fmt.Fprintf(stdout, "  %s (room writes need membership):\n\n", pairMembershipRoomLabel(rooms))
	for _, room := range rooms {
		cmd := pairMembershipCommand(room, remote+":<their-agent-id>", config.NetworkID)
		fmt.Fprintf(stdout, "    %s\n", bold(cmd))
	}
	fmt.Fprintln(stdout, "  (swap <their-agent-id> for the agent id they'll post as)")
	fmt.Fprintf(stdout, "\n  (ask whoever runs %s to run the mirrored command for your agent on their side)\n", remoteLabel)
	fmt.Fprintf(stdout, "  %s\n", pairRemoteAdminNote())

	return outcome.err
}

// restartOutcome is maybeRestartService's result. Exactly one of three
// things happened:
//   - restarted: --restart was requested and the managed service actually
//     restarted.
//   - missingService: --restart was requested, but this network has no
//     managed service installed at all (`moltnet service install` was
//     never run for it). This is deliberately *not* carried in err: a
//     pairing was already written to disk by the time --restart ever runs,
//     and a failed convenience flag on top of that successful write should
//     not fail the whole command (P1 — a joiner running `pair <code>
//     --restart` on a hand-run server, never having installed a service,
//     used to lose their entire aftercare over this).
//   - err: --restart was requested, the service *is* installed, but the
//     restart command itself failed (e.g. launchctl/systemctl erroring).
//     This is a real failure and is still meant to be this command's exit
//     error — but only after the rest of aftercare (printPairRestartLine
//     already reports it inline as a warning) has printed; see
//     printPairInviteAftercare and printPairJoinAftercare.
//
// restarted, missingService, and a non-nil err are mutually exclusive.
type restartOutcome struct {
	restarted      bool
	missingService bool
	err            error
}

// maybeRestartService restarts the resolved network's managed service when
// restart is true, distinguishing "no service installed for this network"
// (restartOutcome.missingService, downgraded to a warning by
// printPairRestartLine, never a command-failing error) from a real restart
// failure on an installed service (restartOutcome.err, still a real error —
// see restartOutcome's doc comment for the exact semantics and why).
func maybeRestartService(ctx context.Context, networkID string, restart bool) restartOutcome {
	if !restart {
		return restartOutcome{}
	}
	if err := newServiceManager().Restart(ctx, networkID); err != nil {
		if errors.Is(err, service.ErrNotInstalled) {
			return restartOutcome{missingService: true}
		}
		return restartOutcome{err: fmt.Errorf("--restart requested but could not restart the moltnet service: %w", err)}
	}
	return restartOutcome{restarted: true}
}

// isInteractive reports whether standard input is attached to a terminal.
// It backs the "offer only if trivially detectable" clause of PLAN.md
// phase 4 item 3: an interactive session gets a --restart tip printed;
// a non-interactive one (scripts, CI, the e2e harness) does not. It is also
// the gate `moltnet uninstall` uses before prompting for confirmation. It
// is a var, not a plain func, so tests (see cmd/moltnet/uninstall_test.go)
// can force the confirmation-prompt path without a real terminal attached
// to stdin — go test's own stdin never is one.
var isInteractive = func() bool {
	fd := os.Stdin.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// localBaseURLHint derives an http(s) base URL an operator can paste
// straight into an admin command from server.listen_addr, substituting
// 127.0.0.1 for a wildcard bind address (":8787", "0.0.0.0:8787") since
// that is what a same-machine admin command should actually dial.
func localBaseURLHint(config app.Config) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(config.ListenAddr))
	if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "8787"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}
