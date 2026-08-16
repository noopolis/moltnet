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
