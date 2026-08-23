package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// setupScope answers Q1 ("Where should this network live?"): global (the
// default, "on this machine") writes into ~/.moltnet/<id>/ exactly like
// plain `moltnet init` does with no --dir; project ("in this folder")
// passes `--dir .`, matching UX.md's J3 journey. There is no third scope in
// v1 (SETUP.md never proposes one), so this is a bool in spirit, given a
// named type for readability at call sites.
type setupScope int

const (
	setupScopeGlobal setupScope = iota
	setupScopeProject
)

// setupConnectChoice answers Q7 ("Connect to another network?"). All three
// are real, fully wired branches (setup_pair.go): setupConnectNotNow builds
// nothing extra, setupConnectOpenUp deploys the relay and generates an
// invite, and setupConnectHaveCode consumes one.
type setupConnectChoice int

const (
	setupConnectNotNow setupConnectChoice = iota
	setupConnectOpenUp
	setupConnectHaveCode
)

// setupAnswers is the fully resolved result of walking the question tree —
// everything setup_plan.go needs to build the child commands that realize
// it. listenAddr is filled in after Q4 by the port preflight (setup_port.go),
// not by the question itself: Q4 only decides the *interface family*
// ("this machine only" vs "all network interfaces"), never a port (SETUP.md's
// "Port selection — decided, not asked").
type setupAnswers struct {
	scope      setupScope
	id         string
	wideBind   bool
	listenAddr string
	rooms      []string
	service    bool
	connect    setupConnectChoice
	// pairing carries Q7's resolved plan when connect is setupConnectOpenUp
	// or setupConnectHaveCode (setup_pair.go's planSetupPairing) — zero
	// value for setupConnectNotNow, which needs nothing extra.
	pairing setupPairResolution
	// configPath is resolveSetupConfigPath(scope, id) -- the exact Moltnet
	// config path this run's `init` (or the adopted existing network) writes
	// to. Every post-init child invocation (setup_plan.go, setup_pair.go)
	// threads this through as `--config`, rather than re-deriving its target
	// from --id: a child's own --id-based discovery (app.ResolveConfigPath)
	// resolves ~/.moltnet/<id>/Moltnet before it ever looks at cwd, and
	// MOLTNET_CONFIG in the operator's ambient environment wins outright over
	// --id entirely, so either one can silently point a child at a different
	// network than the one this wizard just decided on and printed to the
	// operator. An explicit --config removes both ambiguities: it is always
	// what this exact run resolved, checked before either --id or
	// MOLTNET_CONFIG are ever consulted (discoverExplicitConfigPath,
	// internal/app/config_load.go).
	configPath string
}

// maxCanonicalNetworkIDLength and canonicalNetworkIDPattern reuse the exact
// grammar init_rooms.go already defines for --room ids
// (maxCanonicalRoomIDLength, canonicalRoomIDPattern) rather than
// duplicating the regex literal a second time: SETUP.md's Q2 spec calls for
// precisely the same grammar ("^[a-z0-9][a-z0-9._-]*$", max 128) for the
// network id it mints, and init_rooms.go is off-limits to edit in this unit
// (owned by the init* work), but its unexported symbols are still callable
// from anywhere else in this package.
func validateCanonicalNetworkID(id string) error {
	if len(id) > maxCanonicalRoomIDLength {
		return fmt.Errorf("network id %q: must be %d characters or fewer", id, maxCanonicalRoomIDLength)
	}
	if !canonicalRoomIDPattern.MatchString(id) {
		return fmt.Errorf("network id %q: must match ^[a-z0-9][a-z0-9._-]*$ (lowercase letters, digits, '.', '_', '-'; no ':')", id)
	}
	return nil
}

// askSetupScope implements Q1. preset is non-nil when --global/--project was
// passed on the setup command line itself, which skips the prompt entirely
// (SETUP.md: the scope flags exist so a non-interactive one-liner can still
// express Q1) and just confirms the preselected answer with the same
// checkmark line every other answered question prints.
func askSetupScope(ctx context.Context, preset *setupScope) (setupScope, error) {
	if preset != nil {
		label := "on this machine"
		if *preset == setupScopeProject {
			label = "in this folder"
		}
		printInitConfigCheckLine(label, "")
		return *preset, nil
	}

	options := []selectOption{
		{label: "on this machine", hint: "always available, any agent that can reach it"},
		{label: "in this folder", hint: "lives with this project"},
	}
	index, err := setupPromptSelect(ctx, "Where should this network live?", options, 0)
	if err != nil {
		return setupScopeGlobal, err
	}
	if index == 1 {
		return setupScopeProject, nil
	}
	return setupScopeGlobal, nil
}

// askSetupNetworkID implements Q2: a typed name, validated against the
// canonical id grammar, defaulting to defaultID on an empty answer (the
// "Enter-through-everything" rule). Unlike the multiple-choice questions,
// this reads through promptLineCtx rather than promptSelect — there is
// nothing to arrow through when the answer is free text — but still needs
// the same ctx-aware interruptibility every select question already has
// (P1-2: promptLineCtx's own doc comment, setup_prompt_line_ctx.go).
func askSetupNetworkID(ctx context.Context, defaultID string) (string, error) {
	answer, err := setupPromptLineCtx(ctx, fmt.Sprintf("  Name it [%s]: ", defaultID))
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	id := strings.TrimSpace(answer)
	if id == "" {
		id = defaultID
	}
	if err := validateCanonicalNetworkID(id); err != nil {
		return "", err
	}
	printInitConfigCheckLine(id, "")
	return id, nil
}

// askSetupWideBind implements Q4. defaultWide is true only when adopting an
// existing network that is already bound non-loopback — the wizard's
// default always nudges toward whatever the existing network already is,
// never silently proposing to widen or narrow it.
func askSetupWideBind(ctx context.Context, defaultWide bool) (bool, error) {
	options := []selectOption{
		{label: "this machine only", hint: "only agents running on this computer"},
		{label: "all network interfaces", hint: "⚠ any device on your LAN, over plain HTTP"},
	}
	defaultIndex := 0
	if defaultWide {
		defaultIndex = 1
	}
	index, err := setupPromptSelect(ctx, "Reachable from?", options, defaultIndex)
	if err != nil {
		return false, err
	}
	return setupWideBindFromIndex(index), nil
}

// setupWideBindFromIndex maps Q4's chosen option index to the wideBind
// answer askSetupWideBind returns. Pulled out to a named func — the one
// place this mapping is defined — rather than left inline, so
// refuseSetupNonInteractive's Enter-through default (setupWideBindFromIndex(0))
// shares it instead of a second, hand-maintained `false` literal that can
// silently drift from Q4's own option order (P2-5: mutation-proven —
// flipping the refusal's hardcoded wideBind to true left the suite green).
func setupWideBindFromIndex(index int) bool {
	return index == 1
}

// setupDefaultRooms is askSetupRooms' index-0 ("keep \"general\"") answer,
// pulled out to a named func returning a fresh slice each call (never a
// shared package-level slice a caller could mutate in place) so
// refuseSetupNonInteractive's Enter-through default can call the same
// source askSetupRooms itself uses instead of a second, hand-maintained
// []string{starterRoomID} literal (P2-5).
func setupDefaultRooms() []string {
	return []string{starterRoomID}
}

// askSetupRooms implements Q5 for a fresh network (never called when
// adopting an existing one — see printExistingRoomsReadOnly instead, which
// SETUP.md's adoption table requires be read-only there). "rename" and "add
// more" both read one further typed line, since SETUP.md's question table
// treats them as this question's own sub-choices, not separate numbered
// questions of their own.
func askSetupRooms(ctx context.Context) ([]string, error) {
	options := []selectOption{
		{label: "keep \"general\"", hint: "one room, ready to talk in"},
		{label: "rename", hint: "call that first room something else"},
		{label: "add more", hint: "several rooms, named now"},
	}
	index, err := setupPromptSelect(ctx, "Rooms", options, 0)
	if err != nil {
		return nil, err
	}

	switch index {
	case 0:
		return setupDefaultRooms(), nil
	case 1:
		name, err := setupPromptLineCtx(ctx, "  new name for the starter room: ")
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			// A typed sub-question has no "Enter-through" default of its
			// own to fall back to -- "rename" was already an explicit,
			// deliberate choice away from the default -- so an empty answer
			// here is a plain usage error, not a silent no-op back to
			// "general".
			return nil, fmt.Errorf("rename: a new room name is required")
		}
		if err := validateCanonicalRoomID(name); err != nil {
			return nil, err
		}
		printInitConfigCheckLine(name, "")
		return []string{name}, nil
	default: // "add more"
		extra, err := setupPromptLineCtx(ctx, "  additional room ids (comma-separated): ")
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		ids := protocol.UniqueTrimmedStrings(strings.Split(extra, ","))
		if len(ids) == 0 {
			return nil, fmt.Errorf("add more: at least one additional room id is required")
		}
		for _, id := range ids {
			if err := validateCanonicalRoomID(id); err != nil {
				return nil, err
			}
		}
		// "general" is prepended explicitly, not left to init's own
		// replace-unless-present rule (init_rooms.go's
		// resolveInitRoomIDs): this question's whole point is "general
		// plus more", and spelling it out here means the argv setup
		// prints/runs is self-explanatory on its own, without requiring
		// the reader to already know init's replace-vs-append rule.
		rooms := protocol.UniqueTrimmedStrings(append([]string{starterRoomID}, ids...))
		printInitConfigCheckLine(strings.Join(rooms, ", "), "")
		return rooms, nil
	}
}

// printExistingRoomsReadOnly implements Q5's adoption behavior: "Q5 is
// read-only" (SETUP.md's adoption table) since `init --room` has no writer
// against an existing config (writeFileIfMissing no-ops). It just states
// the existing rooms and points at the one command that can actually add
// more, post-setup.
func printExistingRoomsReadOnly(roomIDs []string) {
	fmt.Fprintf(stdout, "  Rooms: %s %s\n", strings.Join(roomIDs, ", "), dim("(existing; unchanged)"))
	fmt.Fprintf(stdout, "    %s use `moltnet room create <id>` to add more after setup\n", yellow("note:"))
}

// askSetupService implements Q6, never called at all when scope is project
// (runSetup's caller skips straight to answers.service = false — see
// SETUP.md's table: "skipped entirely when Q1 = in this folder").
func askSetupService(ctx context.Context) (bool, error) {
	options := []selectOption{
		{label: "yes", hint: "starts on login, restarts if it crashes"},
		{label: "no", hint: "you run `moltnet start` when you want it"},
	}
	index, err := setupPromptSelect(ctx, "Run as a service?", options, 0)
	if err != nil {
		return false, err
	}
	return setupServiceFromIndex(index), nil
}

// setupServiceFromIndex maps Q6's chosen option index to the service
// answer askSetupService returns, shared with refuseSetupNonInteractive's
// Enter-through default the same way setupWideBindFromIndex is (P2-5).
func setupServiceFromIndex(index int) bool {
	return index == 0
}

// askSetupConnect implements Q7. The caller (runSetupInteractive, setup.go)
// dispatches the answer to planSetupPairing (setup_pair.go), which asks
// whatever conditional follow-up questions (Q7.1, Q7.2) the chosen branch
// needs.
func askSetupConnect(ctx context.Context) (setupConnectChoice, error) {
	options := []selectOption{
		{label: "not now", hint: "just my own agents; you can pair later"},
		{label: "invite a friend", hint: "deploy a relay, get a code to send them"},
		{label: "I have a code", hint: "a friend already sent you an invite"},
	}
	index, err := setupPromptSelect(ctx, "Connect to another network?", options, 0)
	if err != nil {
		return setupConnectNotNow, err
	}
	return setupConnectChoice(index), nil
}
