package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-isatty"
	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/service"
)

func runPairCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, buildPairUsage())
		return errors.New("pair requires an invite code, or the invite subcommand")
	}
	if isHelpArg(args[0]) {
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

// pairAftercareOptions bundles what the two pair aftercare printers
// (pair_aftercare.go) need beyond the loaded config: whether --restart was
// requested, the shared room ids the pairing declared (for the
// membership-command guidance, PLAN.md phase 4 item 3), whether --verbose
// was passed (restoring the per-line detail --verbose still gates — the
// wrote-pairing path and the --restart tip — behind the quiet single
// checkmark plus "next:" line default; the plain restart reminder and the
// auth.mode note are real, actionable facts and print either way, see
// printPairRestartLine/printPairStatusBlock), and — join only — the remote
// network's id and display name learned straight from the invite (`pair
// invite` never round-trips these; it prints friend-side placeholders
// instead).
type pairAftercareOptions struct {
	restart           bool
	roomIDs           []string
	verbose           bool
	remoteNetworkID   string
	remoteNetworkName string
}

// pairStatusColumn is the column plain (unstyled) "wrote pairing" status
// text pads to before its abbreviated path annotation (--verbose only). It
// mirrors configLineColumn's *pattern* for `moltnet init` (init_summary.go)
// — pad the plain-width prefix to a fixed column before the dimmed path
// annotation — not its value: pair's own prefix text is a different length,
// so its column is its own number (44 here vs. configLineColumn's 24), not
// a copy of it.
const pairStatusColumn = 44

// printPairWroteLine prints the green "✓ wrote pairing" status line
// (--verbose only), its config path abbreviated to "~" via abbreviateHome
// (init_summary.go, the shared house-style helper) and dimmed,
// column-aligned to pairStatusColumn. Width is computed from the plain
// (unstyled) prefix, matching printInitConfigCheckLine's and
// formatNextStepWithPrefix's pattern, so alignment stays correct whether or
// not styling is active.
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

// printPairRestartLine prints the restart half of the status block. A real
// restart outcome — success, or either "did not restart" case (no managed
// service at all, or the restart command itself failed) — always prints:
// these are genuine, actionable facts, never hidden behind --verbose. The
// plain manual-restart reminder is likewise unconditional whenever a restart
// was not actually performed (P1 fix — there is no hot-reload; relay clients
// are built once at startup, so a pairing written without a restart simply
// does not work yet, and an operator running the quiet default deserves to
// know that as much as one running --verbose does). Only the --restart tip
// itself — "you could have passed --restart" — stays boilerplate, gated
// behind --verbose plus an interactive session (PLAN.md phase 4 item 3's
// "offer only if trivially detectable" clause).
func printPairRestartLine(networkID string, outcome restartOutcome, restartRequested, verbose bool) {
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
	// --restart was never requested at all (the only remaining case: a
	// requested restart always returns one of the three outcomes handled
	// above). The reminder below is a real, actionable fact, so it prints
	// regardless of --verbose; only the "you could pass --restart" tip
	// stays --verbose-and-interactive-only boilerplate.
	if verbose && !restartRequested && isInteractive() {
		fmt.Fprintf(stdout, "  %s rerun with --restart to restart the managed moltnet service now\n", yellow("tip:"))
	}
	fmt.Fprintln(stdout, "  restart the Moltnet server for this pairing to take effect")
}

// printPairStatusBlock prints the detail lines both pair commands' aftercare
// opens with: the wrote-pairing checkmark (--verbose only), the restart
// confirmation/warning/reminder (printPairRestartLine, which prints a real
// outcome regardless of --verbose), and the auth.mode note. The auth.mode
// note is unconditional too (P1 fix): on a plain `moltnet init` (no
// --bearer), auth.mode is ModeNone, which makes the just-written pairing
// token inert — gating that fact behind --verbose meant the quiet default
// silently handed back a pairing that could never actually authenticate. It
// never itself returns an error: a --restart failure of either kind is
// reported inline as part of the block (see printPairRestartLine) and also
// carried out in the returned restartOutcome, so a caller that wants to fail
// the command on a *real* restart failure (never on a merely missing
// service — see restartOutcome) can propagate outcome.err itself, once every
// remaining aftercare line — most importantly, on the invite side, the
// invite command — has already printed.
func printPairStatusBlock(ctx context.Context, config app.Config, path, pairingID string, restart, verbose bool) restartOutcome {
	if verbose {
		printPairWroteLine(pairingID, path)
	}

	outcome := maybeRestartService(ctx, config.NetworkID, restart)
	printPairRestartLine(config.NetworkID, outcome, restart, verbose)

	if config.Auth.Mode != "bearer" && config.Auth.Mode != "open" {
		fmt.Fprintf(stdout, "  %s auth.mode in %s is %q; set it to \"bearer\" so the pairing token is enforced\n",
			yellow("note:"), abbreviateHome(path), config.Auth.Mode)
	}

	return outcome
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
//     printPairInviteAftercare and printPairJoinAftercare (pair_aftercare.go).
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
