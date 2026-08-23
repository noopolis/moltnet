package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/noopolis/moltnet/internal/app"
)

// runSetup implements `moltnet setup` (SETUP.md, Unit 2 of the wizard: the
// command, the question flow, and the runner seam — the animated header is
// Unit 3, so every prompt below prints its checkmark line immediately and
// statically, exactly the way every other command in this CLI already
// does, rather than collapsing into a live-updating region).
//
// Flags are deliberately few: --print-commands (emit the resolved argv
// instead of running it — setup_plan.go) and the mutually exclusive
// --global/--project, which preselect Q1 so a caller can express "on this
// machine" vs "in this folder" without a prompt (SETUP.md: kept specifically
// so the non-TTY refusal below can name a concrete one-liner for either
// scope). There is no --dir: project scope is always "." (J3's model), and
// widening that to an arbitrary path is out of scope for this unit.
func runSetup(ctx context.Context, args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		fmt.Fprint(stdout, buildSetupUsage())
		return nil
	}

	flags := flag.NewFlagSet("moltnet setup", flag.ContinueOnError)
	flags.SetOutput(stdout)
	var (
		printCommands = flags.Bool("print-commands", false, "print the exact commands this run would execute instead of running them, with no live header -- still requires a real terminal to walk the question tree interactively; not a non-interactive/scriptable mode")
		globalFlag    = flags.Bool("global", false, "preselect \"on this machine\" for Q1 (where should this network live)")
		projectFlag   = flags.Bool("project", false, "preselect \"in this folder\" for Q1 (where should this network live)")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("setup does not accept positional arguments")
	}
	if *globalFlag && *projectFlag {
		return fmt.Errorf("--global and --project cannot be used together")
	}

	var preset *setupScope
	if *globalFlag {
		scope := setupScopeGlobal
		preset = &scope
	}
	if *projectFlag {
		scope := setupScopeProject
		preset = &scope
	}

	// Non-TTY refuses outright rather than blocking on a prompt no one is
	// there to answer (SETUP.md: "Non-TTY refuses with the equivalent
	// one-liner rather than prompting") -- checked before any prompt is
	// even attempted, --print-commands included: printing commands is
	// still fundamentally a walk through the question tree, so it needs the
	// same terminal a normal run does. --print-commands' own help text
	// says so explicitly (no live header, not a non-interactive/scriptable
	// mode) so it no longer reads as implying otherwise.
	if !isInteractive() {
		return refuseSetupNonInteractive(preset)
	}

	fmt.Fprintln(stdout, "  moltnet setup")
	fmt.Fprintln(stdout)

	return runSetupInteractive(ctx, preset, *printCommands)
}

// runSetupInteractive walks the full question tree in order (SETUP.md's
// table), interleaving the two adoption gates (setup_adopt.go) at the exact
// points they must run: posture right after an existing config is found
// (before Q4 is even asked), the immutable-answer comparison right after Q4
// is answered (before Q5 is ever asked, since a refusal here must land
// before any mutation and asking further questions the answer will never
// use would be misleading). Q7's two unimplemented branches are checked
// last, before setupPlanInvocations ever builds an argv -- so a "not yet
// implemented" refusal is guaranteed to land before init or service install
// have run.
func runSetupInteractive(ctx context.Context, preset *setupScope, printCommandsOnly bool) error {
	// Unit 3's live header (setup_header.go): runs for the whole question
	// flow and execution below, settling at the completion screen once this
	// function returns. startSetupHeader itself enforces "no header, no
	// ticker, no goroutine" for --print-commands and a genuinely non-
	// terminal stdout, returning nil in both cases — Stop is a safe no-op
	// on that nil value, so this defer is unconditional.
	header := startSetupHeader(printCommandsOnly)
	defer header.Stop()

	scope, err := askSetupScope(ctx, preset)
	if err != nil {
		return err
	}
	if scope == setupScopeProject {
		if err := checkSetupProjectGitCheckoutGate(); err != nil {
			return err
		}
	}

	id, err := askSetupNetworkID(ctx, app.DefaultNetworkID)
	if err != nil {
		return err
	}

	existing, err := loadExistingSetupNetwork(scope, id)
	if err != nil {
		return err
	}
	if existing.found {
		if err := checkSetupAdoptionPosture(existing); err != nil {
			return err
		}
	}

	defaultWide := existing.found && !app.IsLoopbackListenAddr(existing.config.ListenAddr)
	wideBind, err := askSetupWideBind(ctx, defaultWide)
	if err != nil {
		return err
	}
	if existing.found {
		if err := checkSetupImmutableAnswers(existing, id, wideBind); err != nil {
			return err
		}
	}

	var rooms []string
	if existing.found {
		rooms = existingRoomIDs(existing.config)
		printExistingRoomsReadOnly(rooms)
	} else {
		rooms, err = askSetupRooms(ctx)
		if err != nil {
			return err
		}
	}

	service := false
	if scope == setupScopeGlobal {
		service, err = askSetupService(ctx)
		if err != nil {
			return err
		}
	}

	connect, err := askSetupConnect(ctx)
	if err != nil {
		return err
	}
	// existing.path is resolveSetupConfigPath(scope, id) — the exact path a
	// fresh `init` for this scope/id would write to, computed unconditionally
	// by loadExistingSetupNetwork above regardless of found/not-found — so
	// this needs no second resolution call. planSetupPairing needs it before
	// init has necessarily run yet: the Cloudflare-token and relay-credential
	// files it may read/prompt for live in a .moltnet/ directory alongside
	// this same config path.
	configPath := existing.path
	pairing, err := planSetupPairing(ctx, connect, rooms, configPath, id, printCommandsOnly)
	if err != nil {
		return err
	}

	listenAddr := existing.config.ListenAddr
	if !existing.found {
		host := "127.0.0.1"
		if wideBind {
			host = "0.0.0.0"
		}
		listenAddr, err = preflightListenAddr(host)
		if err != nil {
			return err
		}
		if wideBind {
			if warning := initNonLoopbackWarning(listenAddr, false, rooms); warning != "" {
				fmt.Fprintf(stdout, "  %s %s\n", yellow("warning:"), warning)
			}
		}
	} else if wideBind {
		// Adopting an already wide-bound network still needs this warning
		// reprinted: SETUP.md's adoption table treats that network as
		// wide-bound today exactly like a freshly-authored one, but before
		// this fix the operator only ever saw it on the run that originally
		// created the network -- a later `moltnet setup` adopting the same
		// network printed nothing at all, even though the same
		// plaintext-credential exposure is still live. Reuses the shared,
		// production warning check against the existing config directly
		// (app.NonLoopbackAnonymousWriteWarning) rather than
		// initNonLoopbackWarning's synthetic-config path above, which is
		// built for describing a config about to be authored, not one
		// already on disk.
		if warning := app.NonLoopbackAnonymousWriteWarning(existing.config); warning != "" {
			fmt.Fprintf(stdout, "  %s %s\n", yellow("warning:"), warning)
		}
	}

	answers := setupAnswers{
		scope:      scope,
		id:         id,
		wideBind:   wideBind,
		listenAddr: listenAddr,
		rooms:      rooms,
		service:    service,
		connect:    connect,
		pairing:    pairing,
		configPath: configPath,
	}
	invocations := setupPlanInvocations(answers, existing.found)

	if printCommandsOnly {
		printSetupCommands(invocations)
		printSetupPairOpenUpPrintCommandsNote(answers)
		return nil
	}

	return executeSetupPlan(ctx, answers, invocations, configPath)
}

// existingRoomIDs extracts the id of every room in an adopted config's
// rooms[], for Q5's read-only display (printExistingRoomsReadOnly).
func existingRoomIDs(config app.Config) []string {
	ids := make([]string, 0, len(config.Rooms))
	for _, room := range config.Rooms {
		ids = append(ids, room.ID)
	}
	if len(ids) == 0 {
		return []string{starterRoomID}
	}
	return ids
}

// refuseSetupNonInteractive builds SETUP.md's non-TTY refusal: the
// Enter-through-everything default plan for the requested (or, absent
// --global/--project, global) scope, rendered as the exact commands a
// script should run instead.
func refuseSetupNonInteractive(preset *setupScope) error {
	scope := setupScopeGlobal
	if preset != nil {
		scope = *preset
	}
	if scope == setupScopeProject {
		if err := checkSetupProjectGitCheckoutGate(); err != nil {
			return err
		}
	}

	invocations, err := buildSetupRefusalInvocations(preset)
	if err != nil {
		return fmt.Errorf("moltnet setup requires a terminal to ask questions, and could not even prepare the non-interactive default: %w", err)
	}

	message := "moltnet setup requires a terminal to ask questions; run the equivalent commands directly:"
	for _, invocation := range invocations {
		message += "\n  " + renderChildInvocation(invocation)
	}
	return errors.New(message)
}

// buildSetupRefusalInvocations computes the exact childInvocation plan the
// non-TTY refusal names: the Enter-through-everything answers for scope,
// derived from the very same defaults the interactive question tree itself
// uses (setupWideBindFromIndex, setupDefaultRooms, setupServiceFromIndex —
// all setup_questions.go) rather than a second, hand-maintained copy of
// them (P2-5: mutation-proven — flipping wideBind here to true, with the
// preflight host still hardcoded to "127.0.0.1" regardless, left the suite
// green, since nothing compared this against the interactive path's own
// defaults, and wideBind's stored value has no effect on the printed
// argv unless the preflight host is actually derived from it too — exactly
// the same host-selection runSetupInteractive itself does below). It still
// runs the real port preflight (this requires no terminal at all) so the
// printed --listen value is accurate for right now, rather than a
// hardcoded, possibly-already-taken 8787.
func buildSetupRefusalInvocations(preset *setupScope) ([]childInvocation, error) {
	scope := setupScopeGlobal
	if preset != nil {
		scope = *preset
	}

	wideBind := setupWideBindFromIndex(0)
	answers := setupAnswers{
		scope:    scope,
		id:       app.DefaultNetworkID,
		wideBind: wideBind,
		rooms:    setupDefaultRooms(),
		service:  scope == setupScopeGlobal && setupServiceFromIndex(0),
		connect:  setupConnectNotNow,
	}

	host := "127.0.0.1"
	if wideBind {
		host = "0.0.0.0"
	}
	listenAddr, err := preflightListenAddr(host)
	if err != nil {
		return nil, err
	}
	answers.listenAddr = listenAddr

	// Same exact-path resolution the interactive path uses (runSetupInteractive,
	// above) -- needed so the refusal's printed commands carry the same
	// --config every post-init child now requires, and so this stays
	// byte-for-byte comparable against the interactive Enter-through
	// --print-commands output (TestSetupNonInteractiveRefusalMatchesInteractiveEnterThrough).
	configPath, err := resolveSetupConfigPath(scope, answers.id)
	if err != nil {
		return nil, err
	}
	answers.configPath = configPath

	return setupPlanInvocations(answers, false), nil
}

// errSetupProjectNestedGitCheckout gates Q1's "in this folder" answer (and
// --project) behind an explicit "not yet" refusal for the one combination
// the carried-over relative-`--dir` .gitignore finding is still unresolved
// for, verified empirically rather than by inspection alone: cwd sitting
// inside a git checkout it is not itself the root of.
// prefixedGitignoreCredentialEntries (init_checkout.go) computes
// filepath.Rel(gitRoot, root) with gitRoot always absolutized
// (gitCheckoutRoot's own filepath.Abs call) but root passed through exactly
// as this wizard's own `init --dir .` invocation gives it — the literal
// string ".", still relative. filepath.Rel errors on that abs-vs-relative
// mismatch every time, so the prefix silently stays empty. That happens to
// be harmless when cwd already is the checkout root (an empty prefix is
// also the *correct* answer there), but when cwd is a subdirectory of an
// ancestor checkout, the gitignore entries setup's own init step would
// write land unprefixed at the checkout's top-level .gitignore instead of
// anchored at this subdirectory — not a credential leak (a bare "Moltnet"
// entry with no interior slash still matches this subdirectory's file too,
// since such patterns match at any depth), but broader than SETUP.md's
// "correctly-prefixed" requirement and prefixedGitignoreCredentialEntries'
// own doc comment call for ("this rule can never accidentally ignore an
// unrelated file elsewhere in the tree"). Refusing here — only for this one
// affected combination — rather than shipping the degraded result.
var errSetupProjectNestedGitCheckout = errors.New("setup: \"in this folder\" is not yet supported from inside a subdirectory of an existing git checkout")

// checkSetupProjectGitCheckoutGate implements the refusal above. It must
// run immediately once project scope is known (before Q2 ever asks for a
// name, let alone before any mutation), for both the interactive "in this
// folder" answer and --project.
func checkSetupProjectGitCheckoutGate() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve current directory: %w", err)
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("resolve current directory: %w", err)
	}
	gitRoot, ok := gitCheckoutRoot(cwd)
	if !ok || filepath.Clean(gitRoot) == filepath.Clean(absCwd) {
		return nil
	}
	return fmt.Errorf(
		"%q is inside the git checkout rooted at %q; `moltnet init --dir .`'s .gitignore entries cannot yet be correctly anchored to this subdirectory — run setup from %q instead, or choose \"on this machine\": %w",
		absCwd, gitRoot, gitRoot, errSetupProjectNestedGitCheckout,
	)
}
