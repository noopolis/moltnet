package main

import (
	"errors"
	"fmt"

	"github.com/noopolis/moltnet/internal/app"
)

// buildFreshServerConfig returns the Moltnet config file contents runInit
// should write for a brand-new network at id/name, generating whatever
// token(s) that requires along the way. Callers must only invoke this when
// the server config genuinely does not exist yet: the tokens it generates
// are otherwise silently discarded once writeFileIfMissing no-ops, which is
// exactly the wasted-entropy pattern P3-3 exists to avoid (see runInit's
// own comment at its call site).
//
// listenAddr and roomIDs are runInit's already-resolved and
// already-validated --listen/--room values (init_listen.go/init_rooms.go).
// roomIDs is only ever meaningful on the plain-init path: bearerMoltnetConfig
// takes no rooms parameter at all, since `--bearer --room` is rejected
// before this function is ever called (see runInit's doc comment) --
// passing it through here regardless would silently suggest bearer configs
// could carry authored rooms, which they cannot.
func buildFreshServerConfig(id, name string, bearer bool, listenAddr string, roomIDs []string) (string, error) {
	if bearer {
		// Two tokens, always minted together: the operator token (unchanged
		// id/scopes/behavior) plus a "console" token scoped to exactly
		// [observe] -- the credential `moltnet console` requires before it
		// will ever hand the browser a token (consoleObserveToken,
		// console.go). Neither is ever printed. Without the console token,
		// the canonical init --bearer -> console flow used to land on a raw
		// 401; minting both here closes that gap on the fresh-config path.
		operatorToken, err := app.GenerateRandomToken(256)
		if err != nil {
			return "", err
		}
		consoleToken, err := app.GenerateRandomToken(256)
		if err != nil {
			return "", err
		}
		return bearerMoltnetConfig(id, name, operatorToken, consoleToken, listenAddr), nil
	}

	// PLAN.md phase 7.0: plain init also mints exactly one operator token --
	// [observe, write, admin], deliberately never "pair" (see
	// defaultMoltnetConfig's doc comment, templates.go) -- so the network's
	// own owner has a key to administer it. auth.mode stays open.
	operatorToken, err := app.GenerateRandomToken(256)
	if err != nil {
		return "", err
	}
	return defaultMoltnetConfigWithOptions(id, name, operatorToken, listenAddr, roomIDs), nil
}

// initExistingServerTokenOutcome bundles what applying the right token
// action against an already-existing Moltnet config produced, for
// runInit's initSummary to describe accurately. Every field defaults to
// its zero value ("nothing happened") on the paths that do not apply.
type initExistingServerTokenOutcome struct {
	bearerAdded                    bool
	bearerAddErr                   error
	bearerUpgradeOnly              bool
	operatorTokenRepaired          bool
	operatorTokenRepairSkippedNote string
}

// applyExistingServerConfigTokens runs the one token action appropriate for
// an existing (not just-created by this run) Moltnet config at serverPath,
// picking between three PLAN.md phase 7.0 cases:
//
//   - serverIsSymlink: nothing can be written through it safely. --bearer
//     records a bearerAddErr note; plain init stays silent (it has never
//     promised to touch an existing config at all).
//   - bearer: the "unblock the --bearer upgrade" fix. Adds the operator
//     token ([observe, write, admin]; deliberately no "pair" -- see F1's
//     fix in templates.go's bearerMoltnetConfig, minted the same way here)
//     and the console token via addOperatorTokenWithRollback, which since
//     7.0 almost always finds the
//     plain-init operator token already present and upgrades in place
//     (leaving it untouched, landing only the console token) rather than
//     minting a fresh pair — bearerUpgradeOnly records which one happened.
//     ErrAuthTokensExist becomes a recorded bearerAddErr, not a returned
//     error, so the rest of init's summary still prints; any other error is
//     returned outright.
//   - default (plain init, not a symlink): the repair path. Adds a
//     pair-less operator token via repairOperatorTokenWithRollback,
//     preserving auth.mode. ErrAuthTokensExist (tokens already present) and
//     ErrAuthModeNotOpen (mode already moved off "open") both leave the
//     config untouched; only the former sets operatorTokenRepairSkippedNote,
//     since the latter is simply outside this repair's stated scope, not an
//     actionable surprise. Any other error is returned outright.
func applyExistingServerConfigTokens(serverPath string, bearer bool, serverIsSymlink bool) (initExistingServerTokenOutcome, error) {
	var outcome initExistingServerTokenOutcome

	switch {
	case serverIsSymlink:
		if bearer {
			// Round 2's P3 fix: this used to also carry serverPath and its
			// own "edit auth: there manually to add an operator token"
			// advice. init_summary.go's printInitSummary formats every
			// bearerAddErr through a wrapper that already names the (short,
			// abbreviated) path and gives that exact advice -- %v-ing an
			// error that repeated both made the printed line say the path
			// twice and "edit auth: there ... add an operator token" twice.
			// Keeping this error to the one fact the wrapper cannot already
			// know (why the write was refused) lets the wrapper own the
			// path and the actionable next step, said once each.
			outcome.bearerAddErr = fmt.Errorf("is a symlink")
		}
		return outcome, nil

	case bearer:
		// bearerUpgradeOnly purely picks the accurate verbose-mode sentence
		// for what the write below is about to do -- see
		// existingOperatorTokenPresent's own doc comment.
		outcome.bearerUpgradeOnly = existingOperatorTokenPresent(serverPath)

		operatorToken, err := app.GenerateRandomToken(256)
		if err != nil {
			return outcome, err
		}
		consoleToken, err := app.GenerateRandomToken(256)
		if err != nil {
			return outcome, err
		}
		if writeErr := addOperatorTokenWithRollback(serverPath, app.AuthTokenWriteback{
			ID:     "operator",
			Value:  operatorToken,
			Scopes: []string{"observe", "write", "admin"},
		}, app.AuthTokenWriteback{
			ID:     "console",
			Value:  consoleToken,
			Scopes: consoleTokenScopes,
		}); writeErr != nil {
			if !errors.Is(writeErr, app.ErrAuthTokensExist) {
				return outcome, writeErr
			}
			outcome.bearerAddErr = writeErr
		} else {
			outcome.bearerAdded = true
		}
		return outcome, nil

	default:
		operatorToken, err := app.GenerateRandomToken(256)
		if err != nil {
			return outcome, err
		}
		if writeErr := repairOperatorTokenWithRollback(serverPath, app.AuthTokenWriteback{
			ID:     "operator",
			Value:  operatorToken,
			Scopes: []string{"observe", "write", "admin"},
		}); writeErr != nil {
			switch {
			case errors.Is(writeErr, app.ErrAuthTokensExist):
				// P1-2: a config that already has an admin-capable token
				// (the common case -- a healthy network hitting its own
				// second `init`) needs no note at all; this repair genuinely
				// has nothing to do for it. Only a config whose existing
				// tokens are all non-admin-scoped is the actionable case the
				// note below exists for.
				if !existingConfigHasAdminToken(serverPath) {
					outcome.operatorTokenRepairSkippedNote = fmt.Sprintf("%s already has auth.tokens; left it untouched (edit auth: there to add an operator token by hand if you need one)", abbreviateHome(serverPath))
				}
			case errors.Is(writeErr, app.ErrAuthModeNotOpen):
				// Not this repair's concern: plain init has never rewritten
				// an existing config's auth.mode, and a config already moved
				// off "open" is outside the one shape 7.0 defines this
				// repair for.
			default:
				return outcome, writeErr
			}
		} else {
			outcome.operatorTokenRepaired = true
		}
		return outcome, nil
	}
}
