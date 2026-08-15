package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/noopolis/moltnet/internal/app"
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
// pair/relay command should read and write: explicit --config > ./Moltnet
// in cwd > sole network directory under ~/.moltnet/, disambiguated by id
// (app.ResolveConfigPath) — the same order `moltnet start` and `moltnet
// admin` use.
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

// pairAftercareOptions bundles what printPairAftercare needs beyond the
// loaded config to finish a pair/pair invite run: the shared room ids the
// pairing declared (for the membership-command hint, PLAN.md phase 4 item
// 3) and the remote network's id (known outright by `pair <code>`; still
// unknown to `pair invite`, which prints a placeholder instead).
type pairAftercareOptions struct {
	restart         bool
	roomIDs         []string
	remoteNetworkID string
}

// printPairAftercare prints the shared aftercare both pair commands end
// with: an auth-mode note when the pair-scoped token would not yet be
// enforced, either a restarted-service confirmation (--restart) or the
// restart reminder (phase 1 has no live config reload), and finally a
// "Next:" block naming the exact `moltnet admin room members add` command
// this side should run for each shared room. It returns an error when
// --restart was requested but this network has no managed service to
// restart.
func printPairAftercare(ctx context.Context, config app.Config, path string, opts pairAftercareOptions) error {
	if config.Auth.Mode != "bearer" && config.Auth.Mode != "open" {
		fmt.Fprintf(stdout, "note: auth.mode in %s is %q; set it to \"bearer\" so the pairing token is enforced\n", path, config.Auth.Mode)
	}

	restarted, err := maybeRestartService(ctx, config.NetworkID, opts.restart)
	if err != nil {
		return err
	}
	if restarted {
		fmt.Fprintf(stdout, "restarted the moltnet service for network %q\n", config.NetworkID)
	} else {
		if !opts.restart && isInteractive() {
			fmt.Fprintln(stdout, "tip: rerun with --restart to restart the managed moltnet service now")
		}
		fmt.Fprintln(stdout, "restart the Moltnet server for this pairing to take effect")
	}

	printMembershipNextSteps(config, opts.remoteNetworkID, opts.roomIDs)
	return nil
}

// maybeRestartService restarts the resolved network's managed service when
// restart is true, propagating a clear error when this network has no
// service installed (`moltnet service install`) to restart.
func maybeRestartService(ctx context.Context, networkID string, restart bool) (bool, error) {
	if !restart {
		return false, nil
	}
	if err := newServiceManager().Restart(ctx, networkID); err != nil {
		return false, fmt.Errorf("--restart requested but could not restart the moltnet service: %w", err)
	}
	return true, nil
}

// isInteractive reports whether standard input is attached to a terminal.
// It backs the "offer only if trivially detectable" clause of PLAN.md
// phase 4 item 3: an interactive session gets a --restart tip printed;
// a non-interactive one (scripts, CI, the e2e harness) does not.
func isInteractive() bool {
	fd := os.Stdin.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// printMembershipNextSteps prints the "Next:" block both pair commands end
// with: for each shared room this pairing declared, the exact `moltnet
// admin room members add` invocation this network should run to grant the
// paired network's relayed actor membership — the "hidden membership step"
// PLAN.md phase 4 calls out. remoteNetworkID is blank when not yet known
// (pair invite, before the friend has paired back), in which case a
// placeholder is printed instead.
//
// The command deliberately omits --base-url and --token: naming --base-url
// would skip resolveAdminClient's local-config fallback
// (admin_config_fallback.go), which only engages when neither --base-url
// nor a client config is given, so leaving both out lets a same-machine
// `moltnet admin` command derive the base URL and admin token from this
// network's own server config automatically. A remote operator has no local
// server config to derive either from, and --network only resolves a
// *local* config (it is meaningless off-machine), hence the one-line note:
// add --base-url and --token-env explicitly, and drop --network.
func printMembershipNextSteps(config app.Config, remoteNetworkID string, roomIDs []string) {
	rooms := protocol.UniqueTrimmedStrings(roomIDs)
	if len(rooms) == 0 {
		return
	}

	remote := strings.TrimSpace(remoteNetworkID)
	if remote == "" {
		remote = "<friend-network-id>"
	}

	steps := make([]nextStep, 0, len(rooms))
	for _, room := range rooms {
		steps = append(steps, nextStep{
			command: fmt.Sprintf("moltnet admin room members add --room %s --member %s:<remote-member-id> --network %s",
				room, remote, config.NetworkID),
			description: "grant membership",
		})
	}

	printNextSteps(steps)
	fmt.Fprintln(stdout, "    (remote? add --base-url <url> --token-env MOLTNET_ADMIN_TOKEN and drop --network)")
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
