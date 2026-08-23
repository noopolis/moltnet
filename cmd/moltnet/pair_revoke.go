package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
)

// runPairRevoke implements `moltnet pair revoke <pairing-id>` (PLAN.md
// 7B.3): the missing half of the peering lifecycle. It removes the
// pairings[] entry, the peer's inbound auth.tokens[] credential, and this
// pairing's grant from every room's federation list (see
// app.RevokePairing's doc comment for the reasoning behind stripping
// federation too, not just the pairing and its token).
//
// Argument shape mirrors `pair <invite-code>`: one positional id plus the
// same --config/--id flags (splitPairJoinArgs, pair_join.go, is reused
// as-is -- it already separates "one positional token plus flags" generically,
// it is not specific to invite codes despite the name).
func runPairRevoke(ctx context.Context, args []string) error {
	flagArgs, pairingID, err := splitPairJoinArgs(args)
	if err != nil {
		return fmt.Errorf("pair revoke accepts exactly one pairing id: %w", err)
	}

	flags := flag.NewFlagSet("moltnet pair revoke", flag.ContinueOnError)
	flags.SetOutput(stdout)
	var (
		configPath  = flags.String("config", "", "Moltnet config path")
		networkID   = flags.String("id", "", "network id to select under ~/.moltnet when several exist")
		restart     = flags.Bool("restart", false, "restart the managed service for the resolved network after revoking")
		verboseFlag = flags.Bool("verbose", false, "print full detail: the --restart tip")
	)
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	// Trim at this CLI boundary only (Fix 4): InviteReceiptPath hashes
	// pairingID raw, not trimmed, so an operator's typo'd surrounding
	// whitespace is normalized here rather than inside the hash two
	// different ids must not collide through.
	pairingID = strings.TrimSpace(pairingID)
	if pairingID == "" {
		return fmt.Errorf("pair revoke requires a pairing id")
	}

	path, err := resolvePairConfigPath(*configPath, *networkID)
	if err != nil {
		return err
	}
	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		return err
	}

	result, err := revokePairingWithRollback(path, pairingID)
	if err != nil {
		// app.RevokePairing's own removePairing already wraps
		// ErrPairingNotFound with the pairing id (%w: %q); appending it a
		// second time here used to print it twice, e.g. `pairing not found:
		// "peer1": "peer1"`.
		return err
	}

	return printPairRevokeAftercare(ctx, config, pairingID, result, *restart, *verboseFlag)
}

// revokePairingWithRollback mirrors writePairingWithRollback's (pair.go)
// snapshot/write/reload/restore-on-failure shape for app.RevokePairing: a
// revoke that leaves the config unable to reload through the same
// full-startup load path the server uses is rolled back instead of left in
// place.
//
// Once the reload check passes -- the revoke is actually committed, not
// merely attempted -- it also removes pairingID's invite receipt, if any
// (app.RemoveInviteReceipt). Without this, a revoked pairing would still
// leave `pair invite show <pairing-id>` handing back a code that grants
// access to a pairing that no longer exists anywhere in the config: exactly
// the "live-looking invite" this unit is required to prevent. It runs after,
// not before, the reload check: a revoke that gets rolled back never
// happened, so its receipt (for a pairing that is once again live) must
// survive untouched.
func revokePairingWithRollback(path string, pairingID string) (app.RevokeResult, error) {
	snapshot, err := snapshotFile(path)
	if err != nil {
		return app.RevokeResult{}, err
	}

	result, err := app.RevokePairing(path, pairingID)
	if err != nil {
		// F2 (live-confirmed): an interruption between pair.go's
		// WriteInviteReceipt and its WritePairing call leaves an orphan
		// receipt for a pairing that never actually landed in config --
		// app.RevokePairing correctly fails ErrPairingNotFound for it, since
		// there is nothing in pairings[] to remove, but before this, that
		// also meant RemoveInviteReceipt was never reached: no CLI path
		// could ever clear that receipt (`pair invite show` would keep
		// handing back its code forever; see pairingConfigured's caller,
		// pair_invite.go). Clearing the receipt here too, even though the
		// pairing itself was never found, makes this the orphan's cleanup
		// path. Best-effort and silent: a failure here must not shadow the
		// real ErrPairingNotFound the caller still needs to see and act on.
		if errors.Is(err, app.ErrPairingNotFound) {
			_ = app.RemoveInviteReceipt(path, pairingID)
		}
		return app.RevokeResult{}, err
	}

	if _, err := app.LoadConfigForPath(path, ""); err != nil {
		if restoreErr := snapshot.restore(path); restoreErr != nil {
			return app.RevokeResult{}, fmt.Errorf("revoked pairing but the config failed to reload (%v); restore also failed: %w", err, restoreErr)
		}
		return app.RevokeResult{}, fmt.Errorf("revoked pairing but the config failed to reload; rolled back: %w", err)
	}

	if err := app.RemoveInviteReceipt(path, pairingID); err != nil {
		return app.RevokeResult{}, fmt.Errorf("revoked pairing but failed to remove its invite receipt: %w", err)
	}

	return result, nil
}

// printPairRevokeAftercare is pair invite/join's aftercare
// (printPairStatusBlock, pair.go) trimmed to what a revoke actually needs:
// no "wrote pairing" checkmark (nothing was written, something was removed)
// and no auth.mode note (irrelevant to a credential that no longer exists),
// but the same restart confirmation/warning/reminder, since revocation is
// exactly as restart-dependent as invite/join (no live config reload).
//
// F2 (confirmed live by two independent reviewers): the config edit above
// this function's call site is not the whole story. There is no in-process
// hot-reload of auth policy, pairing state, or relay clients anywhere in
// this codebase (see printPairRestartLine's own doc comment) -- a server
// that was already running keeps trusting the just-revoked credential,
// keeps listing the pairing's rooms to it, and keeps relaying to it, right
// up until it is actually restarted. Reporting bare success here regardless
// would tell an operator a credential was cut off when it demonstrably was
// not. This function now probes for exactly that live-server case
// (probeStatusHealth against localBaseURLHint, status.go -- the same
// anonymous /healthz check `moltnet status` uses, so no admin token is
// needed) and, whenever a restart did not just happen, refuses to print the
// "revoked" success line and returns a real error instead of nil: the
// minimum acceptable fix the finding allows when a true live revoke (drop
// the credential and pairing from the running process, close its relay
// connections and streams) is out of scope for this unit. --restart that
// actually restarts the managed service still gets the full success report,
// since the live server picks up the rewritten config on the way back up.
func printPairRevokeAftercare(ctx context.Context, config app.Config, pairingID string, result app.RevokeResult, restart, verbose bool) error {
	outcome := maybeRestartService(ctx, config.NetworkID, restart)

	// serverReachable is only computed (and only meaningful) when
	// outcome.restarted is false -- a successful restart is itself proof the
	// server is up. Used again below (F5) to decide whether
	// printPairRestartLine's generic "restart the server" reminder is even
	// true, not just to gate the early "still running" refusal.
	var serverReachable bool
	if !outcome.restarted {
		serverReachable, _ = probeStatusHealth(ctx, localBaseURLHint(config))
		if serverReachable {
			fmt.Fprintf(stdout, "  %s revoked pairing %q in the config, but a moltnet server for %s is still running\n",
				yellow("warning:"), pairingID, config.NetworkID)
			fmt.Fprintln(stdout, "  the peer's credential is NOT yet cut off: it can keep listing, writing, and relaying against that running server until it restarts")
			printPairRestartLine(config.NetworkID, outcome, restart, verbose)
			if outcome.err != nil {
				return outcome.err
			}
			return fmt.Errorf("pairing %q was removed from the config but a running moltnet server for %s has not reloaded it yet; restart the server (moltnet service restart --id %s, or pass --restart) before treating this peer as revoked", pairingID, config.NetworkID, config.NetworkID)
		}
	}

	fmt.Fprintf(stdout, "  %s revoked pairing %q\n", green("✓"), pairingID)
	if result.TokenRemoved {
		fmt.Fprintln(stdout, "  the peer's credential and this pairing's room federation grants are both removed")
	} else {
		// New P2 (confirmed live): removeAuthToken is a deliberate silent
		// no-op when no auth.tokens[] entry has this exact id -- most
		// plausibly a hand-edited config whose token id diverged from the
		// pairing id. Printing the line above unconditionally used to tell
		// the operator the opposite of what happened in exactly that case.
		fmt.Fprintf(stdout, "  %s no auth.tokens[] entry with id %q was found; this pairing's room federation grants are removed, but if the peer's credential uses a different id it was NOT removed and is still authenticated\n",
			yellow("warning:"), pairingID)
	}

	// F5 (review round 2, confirmed live): reaching here with
	// !outcome.restarted && !serverReachable means probeStatusHealth found a
	// clean connection refusal for this network -- no server is running at
	// all, confirmed above, not merely assumed. printPairRestartLine's
	// generic "restart the Moltnet server for this pairing to take effect"
	// reminder is simply false in that case: there is nothing running to
	// restart, and the next `moltnet service start`/manual run reads this
	// already-revoked config fresh, with no restart step needed.
	if outcome.restarted || serverReachable {
		printPairRestartLine(config.NetworkID, outcome, restart, verbose)
	} else {
		fmt.Fprintf(stdout, "  %s no Moltnet server is currently running for %s; it will read this revoked pairing the next time it starts\n",
			green("✓"), config.NetworkID)
	}

	return outcome.err
}
