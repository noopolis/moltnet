package main

import (
	"fmt"

	"github.com/noopolis/moltnet/internal/app"
	authn "github.com/noopolis/moltnet/internal/auth"
)

// consoleTokenScopes is the exact, minimal scope set `moltnet console`
// requires before it will ever put a token in the browser's URL bar
// (consoleObserveToken, console.go) -- shared by every path that mints a
// console token, so init.go's fresh-config path, its existing-config path,
// and console.go's self-heal path can never drift into minting a token
// with one scope too many.
var consoleTokenScopes = []string{"observe"}

// writeConfigWithRollback snapshots path, runs write, then re-runs the same
// full config load the server uses at startup (env-merge included) as a
// post-write check, restoring path to its prior contents if that reload
// fails. what names the change for the error message (e.g. "added operator
// token", "added console token"). This is the shared rollback guarantee
// every plaintext-preserving auth-token writeback in this package gets;
// WritePairing's caller keeps its own copy in pair.go
// (writePairingWithRollback) because its error wording names a pairing
// specifically.
func writeConfigWithRollback(path, what string, write func() error) error {
	snapshot, err := snapshotFile(path)
	if err != nil {
		return err
	}

	if err := write(); err != nil {
		return err
	}

	if _, err := app.LoadConfigForPath(path, ""); err != nil {
		if restoreErr := snapshot.restore(path); restoreErr != nil {
			return fmt.Errorf("%s but the config failed to reload (%v); restore also failed: %w", what, err, restoreErr)
		}
		return fmt.Errorf("%s but the config failed to reload; rolled back: %w", what, err)
	}

	return nil
}

// addOperatorTokenWithRollback adds operatorToken -- plus any extra tokens,
// e.g. the console token `moltnet init --bearer` now mints alongside it in
// one atomic write -- to the Moltnet config at path via the same
// plaintext-preserving writeback machinery the pair commands use,
// backstopped by writeConfigWithRollback.
func addOperatorTokenWithRollback(path string, operatorToken app.AuthTokenWriteback, extra ...app.AuthTokenWriteback) error {
	return writeConfigWithRollback(path, "added operator token", func() error {
		return app.AddOperatorToken(path, operatorToken, extra...)
	})
}

// existingOperatorTokenPresent reports whether the Moltnet config at path
// already has at least one auth.tokens[] entry, read fresh right before
// addOperatorTokenWithRollback's own write. It exists only to pick the
// right verbose-mode wording (runInit's bearerUpgradeOnly) for what that
// write is about to do: mint a brand-new operator+console pair into a
// genuinely tokenless config, or leave a pre-existing operator token
// untouched and add only the console token plus flip the mode
// (app.AddOperatorToken's PLAN.md phase 7.0 upgrade case, triggered once
// plain `init` started minting its own operator token). A read failure
// here conservatively answers false rather than erroring — the write
// immediately following it will hit and report the same underlying
// failure, so this never affects a refusal or rollback decision, only
// which sentence a successful add gets described with.
func existingOperatorTokenPresent(path string) bool {
	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		return false
	}
	return len(config.Auth.Tokens) > 0
}

// existingConfigHasAdminToken reports whether the Moltnet config at path
// already carries a token scoped admin -- this network's owner can
// administer with it, whatever its id. It backs P1-2's fix to
// applyExistingServerConfigTokens' repair-skip note (init_server_config.go):
// that note used to fire on ANY existing auth.tokens[], including a
// perfectly healthy network hitting its own second `init`, which meant the
// single most-repeated command in this CLI told a fine setup to hand-edit
// auth: for no reason. A config that already has an admin-capable token
// needs no note at all -- the repair this note explains genuinely has
// nothing left to do for it. A read failure here conservatively answers
// false (show the note) rather than silently assuming the network is fine;
// applyExistingServerConfigTokens' own repair attempt immediately above
// this call already surfaced the real underlying error if there is one.
//
// Round 2's P3 fix: this used to also treat any token literally named
// "operator" as admin-capable regardless of its actual scopes, so a
// hand-written `id: operator, scopes: [observe]` config was told it needed
// no repair -- while `init --bearer`'s own canUpgradeOperatorOnlyOpenConfig
// (config_writeback_tokens.go) deliberately refuses that exact shape,
// requiring both admin and write scopes before trusting an "operator"-named
// token. The id shortcut here disagreed with that refusal outright; scopes
// alone are now the only signal, matching it.
func existingConfigHasAdminToken(path string) bool {
	config, err := app.LoadConfigForPath(path, "")
	if err != nil {
		return false
	}
	for _, token := range config.Auth.Tokens {
		for _, scope := range token.Scopes {
			if string(scope) == "admin" {
				return true
			}
		}
	}
	return false
}

// repairOperatorTokenWithRollback adds operatorToken to an existing
// tokenless open-mode Moltnet config at path via
// app.AddOperatorTokenPreservingOpenMode, backstopped by the same
// writeConfigWithRollback guarantee every writeback in this package gets.
// It is PLAN.md phase 7.0's repair path for a network that predates plain
// `init` minting an operator token. Unlike addOperatorTokenWithRollback
// (the --bearer path above), the underlying write never flips auth.mode:
// AddOperatorTokenPreservingOpenMode leaves it exactly as read, so a plain
// `init` rerun can never silently reverse phase 6a's local-first default.
func repairOperatorTokenWithRollback(path string, operatorToken app.AuthTokenWriteback) error {
	return writeConfigWithRollback(path, "added operator token", func() error {
		return app.AddOperatorTokenPreservingOpenMode(path, operatorToken)
	})
}

// addConsoleTokenWithRollback adds a single observe-only token to the
// Moltnet config at path, for `moltnet console`'s self-heal path
// (console_selfheal.go).
func addConsoleTokenWithRollback(path string, token app.AuthTokenWriteback) error {
	return writeConfigWithRollback(path, "added console token", func() error {
		return app.AddConsoleToken(path, token)
	})
}

// nextConsoleTokenID returns "console" when no token in tokens already
// carries that id, or a suffixed alternative ("console-2", "console-3", ...)
// when it does -- an id collision with a token this flow did not itself
// create (e.g. a hand-configured "console" token used for something else
// entirely). It never reuses, widens, or downgrades that colliding token's
// scopes; it only ever picks a different id for the new observe-only token.
func nextConsoleTokenID(tokens []authn.TokenConfig) string {
	used := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		used[token.ID] = true
	}
	if !used["console"] {
		return "console"
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("console-%d", suffix)
		if !used[candidate] {
			return candidate
		}
	}
}
