package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/noopolis/moltnet/internal/app"
)

// consoleRestartHealthTimeout/-Interval bound restartForConsoleToken's
// post-restart /healthz poll: long enough for a freshly restarted server to
// come back up, short enough that a genuinely stuck restart still fails
// fast instead of hanging the command.
const (
	consoleRestartHealthTimeout  = 5 * time.Second
	consoleRestartHealthInterval = 200 * time.Millisecond
)

// consoleTokenProbeTimeout bounds probeConsoleToken's request -- short
// enough that a genuinely stuck server still fails fast, matching
// consoleHealthTimeout's rationale (console.go).
const consoleTokenProbeTimeout = 1500 * time.Millisecond

// errConsoleRestartFailed is resolveConsoleAccessToken's sentinel wrapped
// error for the one class of failure runConsole must treat as a real,
// nonzero-exit command failure: an automatic service restart THIS COMMAND
// itself attempted -- never one it merely suggested -- did not verifiably
// bring the console token to life. That covers three distinct ways a
// restart can fail to "come back": the restart command itself erroring, its
// post-restart /healthz poll never answering within
// consoleRestartHealthTimeout, or -- the final, most direct check --
// probeConsoleToken still refusing the token even once /healthz says the
// server is back up. Every one of these means this command promised an
// automatic fix and that fix did not actually happen; printing the usual
// green ready line and exiting 0 over that would be exactly the misleading
// checkmark this whole fix round exists to close.
var errConsoleRestartFailed = errors.New("console: self-heal restart did not come back up")

// resolveConsoleAccessToken decides what runConsole's browser-open path
// should append (if anything) to consoleURL on a bearer-mode network. It is
// the one place that decision is made, for both of the two ways a
// console-safe token can come from: one already sitting in cfg (an earlier
// run's self-heal, `moltnet init --bearer`, or a hand-added entry), or a
// freshly self-healed one minted right now. Either way, the token is never
// trusted on the strength of the config file alone -- probeConsoleToken
// checks it against the actually-running server first, since the server
// only reloads auth.tokens[] on restart (P1 fix: a prior run's self-heal
// could write a token with nothing to restart, or restart a service that
// then didn't come back, and a later run finding that token in the config
// used to append it unconditionally, opening a 307-then-401 behind a green
// checkmark).
//
// Return shape:
//   - err != nil: an automatic restart this call attempted genuinely failed
//     (errConsoleRestartFailed); runConsole must exit nonzero and never open
//     the browser.
//   - err == nil, ok == false: nothing is safe to open yet, but nothing this
//     call attempted actually failed -- an existing token just has not been
//     loaded by a restart yet, self-heal had no managed service to restart,
//     or --no-restart was given. The one exact next command is already
//     printed; runConsole should exit 0 without opening a browser.
//   - err == nil, ok == true: token is probe-confirmed live; safe to put in
//     the browser URL.
func resolveConsoleAccessToken(ctx context.Context, absPath string, cfg app.Config, base string, noRestart bool) (token string, ok bool, err error) {
	if existing, found := consoleObserveToken(cfg); found {
		if probeConsoleToken(ctx, base, existing) {
			return existing, true, nil
		}
		printConsoleTokenNotLoadedNote(cfg.NetworkID)
		return "", false, nil
	}

	healed, healErr := selfHealConsoleToken(ctx, absPath, cfg, base, noRestart)
	if healErr != nil {
		return "", false, healErr
	}
	if healed == "" {
		return "", false, nil
	}
	return healed, true, nil
}

// printConsoleTokenNotLoadedNote prints the one line runConsole uses when a
// console token already sits in the config file -- written by a prior
// self-heal run, by `moltnet init --bearer`, or by hand -- but
// probeConsoleToken found the live server does not actually accept it yet.
// This is the exact "run 2" field bug: nothing reloads auth.tokens[] except
// a restart, and this path never restarts a service on the strength of a
// token merely being present on disk (only selfHealConsoleToken's own fresh
// write ever triggers a restart, and only when it just wrote the token
// itself). It never claims ready and always names exactly one next command,
// picked the same way restartForConsoleToken's own guidance is.
func printConsoleTokenNotLoadedNote(networkID string) {
	fmt.Fprintln(stdout, "  "+yellow("note:")+" a console token is on disk, but the running server has not loaded it yet")
	installed, err := newServiceManager().IsInstalled(networkID)
	if err != nil || !installed {
		fmt.Fprintln(stdout, "  restart moltnet by hand (stop it, then re-run `moltnet start`) to load it")
		return
	}
	fmt.Fprintf(stdout, "  run `moltnet service stop --id %s` then `moltnet service start --id %s` to load it\n", networkID, networkID)
}

// selfHealConsoleToken closes the field gap this file exists for: a
// bearer-mode network whose config has no token scoped to exactly
// [observe] (consoleObserveToken already said no) used to leave `moltnet
// console` with nothing safe to hand the browser, opening straight onto a
// raw 401. This mints one -- silently, no prompt: creating a read-only
// token on the operator's own machine is not a decision worth interrupting
// them for -- writes it via the same plaintext-preserving, reload-checked,
// rollback-backed machinery as every other writeback in this package
// (addConsoleTokenWithRollback, auth_token_writeback.go), restarts the
// managed service so the new token actually takes effect (there is no hot
// reload, unless noRestart opts out), and only ever returns a non-empty
// token once probeConsoleToken has confirmed it works against the live,
// restarted server -- a passing /healthz poll alone is not enough (P1 fix:
// it proves the server answers, not that it loaded this specific token).
//
// See resolveConsoleAccessToken's doc comment for the (token, err) shape
// this returns and how callers must interpret each case.
func selfHealConsoleToken(ctx context.Context, absPath string, cfg app.Config, base string, noRestart bool) (string, error) {
	id := nextConsoleTokenID(cfg.Auth.Tokens)
	value, err := app.GenerateRandomToken(256)
	if err != nil {
		printConsoleSelfHealFailure(err)
		return "", nil
	}

	if err := addConsoleTokenWithRollback(absPath, app.AuthTokenWriteback{
		ID:     id,
		Value:  value,
		Scopes: consoleTokenScopes,
	}); err != nil {
		printConsoleSelfHealFailure(err)
		return "", nil
	}
	fmt.Fprintf(stdout, "  %s console token added\n", green("✓"))
	// P2 fix: this writeback goes through the same untyped-map encode as
	// every other config writeback in this package (encodeWritebackDocument,
	// internal/app/config_writeback.go), which does not preserve comments or
	// key order. Silently reformatting the operator's config on their behalf
	// deserves one honest line, not a surprise the next time they open it.
	fmt.Fprintln(stdout, dim(fmt.Sprintf("  updated %s (comments and key order are not preserved)", abbreviateHome(absPath))))

	restarted, restartErr := restartForConsoleToken(ctx, cfg.NetworkID, base, noRestart)
	if restartErr != nil {
		return "", restartErr
	}
	if !restarted {
		// No managed service to restart, or --no-restart: the one exact
		// manual command is already printed. The token is safely on disk
		// for next time either way.
		return "", nil
	}

	if !probeConsoleToken(ctx, base, value) {
		fmt.Fprintf(stdout, "  %s restarted the service for %s, but it still does not accept the new console token\n", yellow("warning:"), cfg.NetworkID)
		return "", fmt.Errorf("%w for network %q", errConsoleRestartFailed, cfg.NetworkID)
	}
	return value, nil
}

// printConsoleSelfHealFailure prints the same missing-observe-token note
// runConsole always printed before self-heal existed, plus the specific
// reason self-heal itself could not fix it this time.
func printConsoleSelfHealFailure(err error) {
	fmt.Fprintln(stdout, "  note: no observe-only-scoped token in this config; the console will ask you to sign in.")
	fmt.Fprintf(stdout, "  note: could not create one automatically (%v); add one to auth.tokens: manually.\n", err)
}

// restartForConsoleToken restarts the resolved network's managed service --
// the same service.Manager.Restart `pair --restart` uses, reached through
// the same newServiceManager seam tests already fake -- so a just-written
// console token actually takes effect: the server only reads auth.tokens[]
// at startup, so writing the file alone changes nothing for an already
// running process. It then polls base's /healthz until the restarted
// server answers again (or consoleRestartHealthTimeout elapses).
//
// Return shape:
//   - restarted == true, err == nil: the service was found, actually
//     restarted, and confirmed answering /healthz again. The caller still
//     owes the token itself a live probe (selfHealConsoleToken does this)
//     before trusting it -- a passing /healthz poll proves the process is
//     up, not that it loaded this specific token.
//   - restarted == false, err == nil: nothing was attempted, because either
//     noRestart was requested or no managed service is installed at all (a
//     foreground `moltnet start`, never `service install` -- there is
//     nothing this command can restart on the operator's behalf). The one
//     manual next step is already printed either way.
//   - err != nil (errConsoleRestartFailed): a restart WAS attempted -- the
//     command itself errored, or /healthz never came back within the
//     timeout -- and did not come back up. This is a genuine command
//     failure, not a soft note.
func restartForConsoleToken(ctx context.Context, networkID, base string, noRestart bool) (restarted bool, err error) {
	manager := newServiceManager()
	installed, instErr := manager.IsInstalled(networkID)
	if instErr != nil || !installed {
		fmt.Fprintf(stdout, "  %s no managed service installed for %s; restart moltnet by hand (stop it, then re-run `moltnet start`) to load the new console token\n", yellow("note:"), networkID)
		return false, nil
	}

	if noRestart {
		fmt.Fprintf(stdout, "  %s --no-restart: run `moltnet service stop --id %s` then `moltnet service start --id %s` to load the new console token\n", yellow("note:"), networkID, networkID)
		return false, nil
	}

	if restartErr := manager.Restart(ctx, networkID); restartErr != nil {
		fmt.Fprintf(stdout, "  %s could not restart the moltnet service for %s (%v); run `moltnet service stop --id %s` then `moltnet service start --id %s` to load the new console token\n", yellow("warning:"), networkID, restartErr, networkID, networkID)
		return false, fmt.Errorf("%w for network %q: %v", errConsoleRestartFailed, networkID, restartErr)
	}
	if !waitForConsoleHealth(ctx, base) {
		fmt.Fprintf(stdout, "  %s restarted the service for %s but it has not answered /healthz yet; rerun `moltnet console --id %s` shortly\n", yellow("warning:"), networkID, networkID)
		return false, fmt.Errorf("%w for network %q", errConsoleRestartFailed, networkID)
	}

	fmt.Fprintf(stdout, "  %s restarted the service for %s to load the new console token\n", green("✓"), networkID)
	return true, nil
}

// waitForConsoleHealth polls probeConsoleHealth every
// consoleRestartHealthInterval until it reports the server at base healthy,
// consoleRestartHealthTimeout elapses, or ctx is cancelled -- whichever
// comes first.
func waitForConsoleHealth(ctx context.Context, base string) bool {
	deadline := time.Now().Add(consoleRestartHealthTimeout)
	for {
		if probeConsoleHealth(ctx, base) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(consoleRestartHealthInterval):
		}
	}
}

// probeConsoleToken reports whether base's LIVE server process actually
// accepts token right now: a real GET /v1/rooms carrying it as an
// "Authorization: Bearer" header -- the same request shape every other API
// client uses. It deliberately never probes GET /console/?access_token=...:
// maybeSetConsoleAuthCookie (internal/transport/auth.go) 307-redirects on
// the mere PRESENCE of that query parameter, before the token itself is
// checked at all, so that request shape cannot tell a working token from a
// broken one -- it is exactly the "307 -> 401" shape the field bug this
// probe exists to catch produced. GET /v1/rooms requires only the observe
// scope (roomListScopes, internal/transport/scopes.go), which every token
// this package ever probes carries.
//
// A token merely being present in the config file on disk is not enough:
// the server only reads auth.tokens[] at startup (there is no hot reload),
// so a token this process itself just wrote, or one that has sat in the
// file since an earlier run, may not be loaded by whatever process is
// actually answering base right now. This probe -- never the config file's
// own contents -- is the one source of truth runConsole trusts before ever
// putting a token in a browser URL.
func probeConsoleToken(ctx context.Context, base, token string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, consoleTokenProbeTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, base+"/v1/rooms", nil)
	if err != nil {
		return false
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := consoleHTTPClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}
