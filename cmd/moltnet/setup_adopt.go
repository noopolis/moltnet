package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/noopolis/moltnet/internal/app"
	authn "github.com/noopolis/moltnet/internal/auth"
)

// setupExistingNetwork is what loadExistingSetupNetwork finds (or doesn't)
// at the exact path a fresh `moltnet init` for scope/id would write to.
// Found is false, with a zero Config, when nothing exists there yet — the
// common case, and the only one where setup goes on to actually author a
// config.
type setupExistingNetwork struct {
	found  bool
	path   string
	config app.Config
}

// resolveSetupConfigPath returns the exact Moltnet config path a fresh
// `moltnet init` for scope/id would write to — never a fuzzy discovery
// order (app.ResolveConfigPath's fallback tiers are the wrong tool here:
// setup needs to know whether *this exact* target already exists, not
// whichever config cwd/~/.moltnet happens to resolve to). Global mirrors
// runInit's own usingGlobalHome branch (init.go): app.NetworkHomeDir(id) +
// app.DefaultPath. Project mirrors its --dir "." branch: app.DefaultPath
// directly under the current directory, matching `moltnet init --dir .`
// byte for byte.
func resolveSetupConfigPath(scope setupScope, id string) (string, error) {
	if scope == setupScopeProject {
		return app.DefaultPath, nil
	}
	root, err := app.NetworkHomeDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, app.DefaultPath), nil
}

// loadExistingSetupNetwork reports whether a Moltnet config already exists
// at resolveSetupConfigPath(scope, id) and, if so, loads it through the same
// full, env-merged load path the server itself uses at startup
// (app.LoadConfigForPath — not LoadFile — for the same reason service.go's
// resolveServiceSpec and console.go's runConsole use it: an adoption
// decision must see the config the way it will actually behave once
// running, environment overrides included, not just its literal file
// contents).
func loadExistingSetupNetwork(scope setupScope, id string) (setupExistingNetwork, error) {
	path, err := resolveSetupConfigPath(scope, id)
	if err != nil {
		return setupExistingNetwork{}, err
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return setupExistingNetwork{path: path}, nil
		}
		return setupExistingNetwork{}, fmt.Errorf("inspect %q: %w", path, statErr)
	}
	if info.IsDir() {
		return setupExistingNetwork{}, fmt.Errorf("%q is a directory, not a Moltnet config file", path)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return setupExistingNetwork{}, fmt.Errorf("resolve %q: %w", path, err)
	}
	config, err := app.LoadConfigForPath(absPath, "")
	if err != nil {
		return setupExistingNetwork{}, fmt.Errorf("load existing config %q: %w", path, err)
	}
	return setupExistingNetwork{found: true, path: path, config: config}, nil
}

// errSetupNotOpenPosture is loadExistingSetupNetwork callers' sentinel for
// SETUP.md's adoption rule "Not the open posture → hand off": the wizard
// only ever authors and understands the open posture (auth.mode: open), so
// it must not present its own completion screen against a bearer/none
// network it did not shape, and must not attempt to reconcile one either.
var errSetupNotOpenPosture = errors.New("setup: existing network is not in the open posture")

// checkSetupAdoptionPosture is the first adoption gate, run immediately
// after an existing config is found and before Q4/Q5 are ever asked: a
// non-open existing network is a hard hand-off, not a question the wizard
// can meaningfully continue past.
func checkSetupAdoptionPosture(existing setupExistingNetwork) error {
	if existing.config.Auth.Mode == authn.ModeOpen {
		return nil
	}
	return fmt.Errorf(
		"%q has auth.mode %q; this wizard only understands the open posture — use `moltnet validate %s`, `moltnet room create`, and `moltnet admin` directly to manage it: %w",
		abbreviateHome(existing.path), existing.config.Auth.Mode, filepath.Dir(existing.path), errSetupNotOpenPosture,
	)
}

// errSetupImmutableConflict is the sentinel for SETUP.md's other adoption
// gate: "Differs on an immutable answer (id, bind, scope) → refuse before
// any mutation, and print the granular commands that would change it." No
// CLI command in this repo can actually rewrite an existing network's id or
// listen_addr in place (SETUP.md's own "The init gap"/"Adoption needs a
// state table" sections are explicit that init only ever repairs tokens
// against an existing config, never re-renders it) — so the "granular
// commands" this prints are deliberately the honest manual remedy (a
// different --id, or a hand-edit plus restart), not a fabricated
// reconciliation command this CLI does not actually have.
var errSetupImmutableConflict = errors.New("setup: existing network answers differ from the ones just given")

// checkSetupImmutableAnswers is the second adoption gate, run once Q4 has
// been answered: it compares the *interface family* of the existing
// network's bind (loopback vs. wide — never the exact port, which the
// preflight step is free to have already changed between runs for
// unrelated reasons, e.g. the previous run's port later being reclaimed by
// something else) against wideBind, and the existing network's own id
// (a defensive check — for project scope in particular, an existing
// ./Moltnet can carry a different id than the one just typed at Q2, since
// nothing ties them together the way the global home path does) against id.
func checkSetupImmutableAnswers(existing setupExistingNetwork, id string, wideBind bool) error {
	if existing.config.NetworkID != id {
		return fmt.Errorf(
			"%q already identifies itself as network %q, not %q; use a different name, or run against the existing one by typing %q at the name prompt: %w",
			abbreviateHome(existing.path), existing.config.NetworkID, id, existing.config.NetworkID, errSetupImmutableConflict,
		)
	}

	existingWide := !app.IsLoopbackListenAddr(existing.config.ListenAddr)
	if existingWide != wideBind {
		existingLabel, wantLabel := "this machine only", "all network interfaces"
		if existingWide {
			existingLabel, wantLabel = wantLabel, existingLabel
		}
		return fmt.Errorf(
			"%q is already bound to %q (%s), not %s; this wizard cannot change an existing network's bind — edit server.listen_addr by hand and restart the service, or answer %q at the reachable-from prompt to match what is already running: %w",
			abbreviateHome(existing.path), existing.config.ListenAddr, existingLabel, wantLabel, existingLabel, errSetupImmutableConflict,
		)
	}
	return nil
}
