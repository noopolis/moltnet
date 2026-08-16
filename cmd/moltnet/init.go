package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// checkoutMarkers are the files whose presence in the init target directory
// suggests it is a source checkout rather than a runtime install directory
// (PLAN.md phase 4's checkout-detection warning).
var checkoutMarkers = []string{".git", "go.mod", "package.json"}

// runInit implements `moltnet init` (PLAN.md phase 4, item 1).
//
// With no --dir, it writes into the global home ~/.moltnet/<network-id>/,
// prompting for --id on a TTY (hard error otherwise). --dir opts out of the
// global home entirely — including the --id prompt/requirement, so
// `moltnet init --dir .` keeps behaving exactly like the pre-phase-4
// `moltnet init` did: a "local" network written into the given directory,
// no prompt, no TTY requirement.
//
// The positional `moltnet init [path]` form from before phase 4 still
// works, mapped onto --dir with a deprecation note, so existing scripts and
// muscle memory are not broken outright.
//
// ctx is runCLI's signal.NotifyContext(SIGINT, SIGTERM) (cli.go), threaded
// through so Ctrl-C is actually responsive during init: printBannerAnimated
// checks it between animation frames, and resolveInitNetworkID checks it
// while blocked on the interactive --id prompt. A ctx already done when
// either returns is treated exactly like any other runInit error — this
// function returns before touching the filesystem, so nothing is created
// (no directory, no config) on a cancelled init.
func runInit(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("moltnet init", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		idFlag      = flags.String("id", "", "network id (prompted on a TTY when omitted and --dir is not given; required otherwise)")
		nameFlag    = flags.String("name", "", "network display name (default: derived from the network id)")
		dirFlag     = flags.String("dir", "", "write into this directory instead of ~/.moltnet/<network-id>/ (the pre-phase-4 default was --dir .)")
		bearerFlag  = flags.Bool("bearer", false, "set auth.mode to bearer and generate a scoped operator token, stored in Moltnet (0600); never printed")
		verboseFlag = flags.Bool("verbose", false, "print full detail: exact paths, per-file status, and the --bearer tip")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}

	// Arg-count/conflict validation happens before the banner plays (not
	// just before flags.Parse's own error path): both checks below are
	// pure — no output on the success path — so hoisting them ahead of
	// printBannerAnimated costs nothing on a well-formed invocation, but
	// means a doomed `moltnet init a b` (or a positional path combined with
	// --dir) fails instantly instead of only after burning the ~0.9s settle
	// animation. A real user hit exactly that: waited out the animation to
	// see a usage error the arg count alone already made inevitable.
	if flags.NArg() > 1 {
		// Same fix as runNode (node.go): flag stops parsing at the first
		// non-flag arg, so `moltnet init <path> --id x` never reaches --id
		// as a flag — it lands here as extra positional args, *idFlag left
		// at its zero value. Name the offending flag instead of the generic
		// message below when one of the extras looks like one.
		for _, extra := range flags.Args()[1:] {
			if strings.HasPrefix(extra, "-") {
				return fmt.Errorf("init: flags must precede the path (got %q after it); usage: moltnet init [--id <network-id>] [--name <name>] [--dir <path>] [--bearer] [path]", extra)
			}
		}
		return fmt.Errorf("init accepts at most one positional path (deprecated; use --dir)")
	}
	if flags.NArg() == 1 && strings.TrimSpace(*dirFlag) != "" {
		return fmt.Errorf("init: pass either a positional path or --dir, not both")
	}

	// P2-2: the banner must be the first output on every init path — before
	// the positional-path deprecation note below and before
	// resolveInitNetworkID's interactive --id prompt further down — so it
	// prints immediately once flags are known to be well-formed.
	// printBannerAnimated (banner_player.go) plays the settle animation on
	// a real terminal, falling back to plain printBanner's own static
	// output otherwise — init is the one place this CLI can afford the
	// ~1s a full animation takes; see printBannerAnimated's doc comment.
	printBannerAnimated(ctx)
	if err := ctx.Err(); err != nil {
		// SIGINT/SIGTERM landed mid-animation (P2-2/item-3): nothing has
		// been written to the filesystem yet, so returning here is a clean
		// abort — runCLI's normal error path takes it from here, no special
		// cleanup needed (playBanner never uses the alternate screen or
		// hides the cursor; see its doc comment).
		return err
	}

	dir := strings.TrimSpace(*dirFlag)
	if flags.NArg() == 1 {
		dir = flags.Arg(0)
		fmt.Fprintf(stdout, "  %s `moltnet init <path>` is deprecated; use `moltnet init --dir <path>` instead\n", yellow("note:"))
	}

	usingGlobalHome := dir == ""

	id := strings.TrimSpace(*idFlag)
	var err error
	if usingGlobalHome {
		id, err = resolveInitNetworkID(ctx, id)
		if err != nil {
			return err
		}
	} else if id == "" {
		id = app.DefaultNetworkID
	} else if err := protocol.ValidateMessageID(id); err != nil {
		return fmt.Errorf("--id: %w", err)
	}

	name := strings.TrimSpace(*nameFlag)
	if name == "" {
		if id == app.DefaultNetworkID {
			name = app.DefaultNetworkName
		} else {
			name = defaultNetworkNameForID(id)
		}
	}

	var root string
	if usingGlobalHome {
		root, err = app.NetworkHomeDir(id)
		if err != nil {
			return err
		}
	} else {
		root = filepath.Clean(dir)
		if root == "" {
			root = "."
		}
	}

	fmt.Fprintf(stdout, "  Initializing %s\n\n", id)

	dirExisted := false
	if info, statErr := os.Stat(root); statErr == nil && info.IsDir() {
		dirExisted = true
	}

	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create init directory %q: %w", root, err)
	}
	if root != "." {
		if err := os.Chmod(root, 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("secure init directory %q: %w", root, err)
		}
	}

	if warning := checkoutWarning(root); warning != "" {
		fmt.Fprintln(stdout, warning)
	}

	serverPath := filepath.Join(root, app.DefaultPath)
	nodePath := filepath.Join(root, nodeconfig.DefaultPath)

	// Check existence up front (rather than generating a token first and
	// discarding it — P3-3) so the token is only ever generated when it will
	// actually be used: embedded in a fresh config below, or added to an
	// existing one (P1-2).
	serverExists := false
	if info, statErr := os.Stat(serverPath); statErr == nil && !info.IsDir() {
		serverExists = true
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect %q: %w", serverPath, statErr)
	}

	// Cheap Lstat pre-check, done before any file writes: if the existing
	// Moltnet config is a symlink, addOperatorTokenWithRollback would refuse
	// it too (app.AddOperatorToken's rejectSymlinkedConfigPath), but only
	// after MoltnetNode had already been written below and only by hard-
	// failing the whole command with no summary. Detecting it here lets
	// --bearer degrade the same way the tokens-exist case does: skip the
	// add, still print the full summary.
	serverIsSymlink := false
	if serverExists {
		if info, lstatErr := os.Lstat(serverPath); lstatErr != nil {
			return fmt.Errorf("inspect %q: %w", serverPath, lstatErr)
		} else if info.Mode()&os.ModeSymlink != 0 {
			serverIsSymlink = true
		}
	}

	serverContents := defaultMoltnetConfig(id, name)
	if *bearerFlag && !serverExists {
		// Two tokens, always minted together: the operator token (unchanged
		// id/scopes/behavior) plus a "console" token scoped to exactly
		// [observe] -- the credential `moltnet console` requires before it
		// will ever hand the browser a token (consoleObserveToken,
		// console.go). Neither is ever printed. Without the console token,
		// the canonical init --bearer -> console flow used to land on a raw
		// 401; minting both here closes that gap on the fresh-config path.
		operatorToken, tokenErr := app.GenerateRandomToken(256)
		if tokenErr != nil {
			return tokenErr
		}
		consoleToken, tokenErr := app.GenerateRandomToken(256)
		if tokenErr != nil {
			return tokenErr
		}
		serverContents = bearerMoltnetConfig(id, name, operatorToken, consoleToken)
	}

	serverCreated, err := writeFileIfMissing(serverPath, serverContents)
	if err != nil {
		return err
	}
	nodeCreated, err := writeFileIfMissing(nodePath, defaultMoltnetNodeConfig(id))
	if err != nil {
		return err
	}

	var bearerAdded bool
	var bearerAddErr error
	if *bearerFlag && !serverCreated {
		if serverIsSymlink {
			bearerAddErr = fmt.Errorf("moltnet config %q is a symlink; edit auth: there manually to add an operator token", serverPath)
		} else {
			// P1-2: the config already existed, so rather than the old no-op
			// "no token was generated" note, actually add the operator token to
			// it via the same plaintext-preserving writeback machinery the pair
			// commands use — refusing only when auth.tokens already has
			// entries, so this never silently appends a second admin-scoped
			// credential next to one an operator already set up by hand.
			//
			// It mints the console token (id "console", scopes [observe]) in
			// the same atomic write, for the same reason as the fresh-config
			// branch above: a config that gets bearer auth for the first time
			// must always end up with a token `moltnet console` is allowed to
			// use. AddOperatorToken's own guard (auth.tokens must be empty)
			// means auth.tokens is genuinely empty here, so "console" can
			// never collide with an id already in the file.
			operatorToken, tokenErr := app.GenerateRandomToken(256)
			if tokenErr != nil {
				return tokenErr
			}
			consoleToken, tokenErr := app.GenerateRandomToken(256)
			if tokenErr != nil {
				return tokenErr
			}
			if writeErr := addOperatorTokenWithRollback(serverPath, app.AuthTokenWriteback{
				ID:     "operator",
				Value:  operatorToken,
				Scopes: []string{"observe", "write", "admin", "pair"},
			}, app.AuthTokenWriteback{
				ID:     "console",
				Value:  consoleToken,
				Scopes: consoleTokenScopes,
			}); writeErr != nil {
				if !errors.Is(writeErr, app.ErrAuthTokensExist) {
					return writeErr
				}
				bearerAddErr = writeErr
			} else {
				bearerAdded = true
			}
		}
	}

	printInitSummary(initSummary{
		id:            id,
		root:          root,
		dirExisted:    dirExisted,
		serverPath:    serverPath,
		serverCreated: serverCreated,
		nodeCreated:   nodeCreated,
		bearer:        *bearerFlag,
		bearerAdded:   bearerAdded,
		bearerAddErr:  bearerAddErr,
		verbose:       *verboseFlag,
	})

	return nil
}

// resolveInitNetworkID returns idFlag unchanged when non-empty; otherwise,
// on a TTY, it prompts for one; otherwise it is a hard error (PLAN.md:
// "--id <network-id> sets the network id (interactive prompt on a TTY when
// omitted; hard error when non-interactive)").
//
// The prompt read happens in its own goroutine so it can be raced against
// ctx.Done() (item 3): bufio.Reader.ReadString blocks synchronously on
// os.Stdin with no ctx-aware variant, so there is no way to interrupt the
// read itself — a SIGINT during the prompt instead makes this function
// return ctx.Err() immediately, leaving the read goroutine to exit on its
// own once stdin closes or the process does. That goroutine leak is
// bounded: runCLI is about to exit on the same cancelled ctx either way.
func resolveInitNetworkID(ctx context.Context, idFlag string) (string, error) {
	if idFlag != "" {
		if err := protocol.ValidateMessageID(idFlag); err != nil {
			return "", fmt.Errorf("--id: %w", err)
		}
		return idFlag, nil
	}
	if !isInteractive() {
		return "", fmt.Errorf("moltnet init requires --id when run non-interactively (no TTY to prompt on)")
	}

	fmt.Fprint(stdout, "network id (letters, digits, hyphens; identifies this network to friends): ")

	// os.Stdin is read into a local here, before the goroutine starts,
	// rather than inside it: os.Stdin is a mutable package var, and this
	// function is about to return (on the ctx.Done() branch below) without
	// waiting for the goroutine to actually run, so a caller reassigning
	// os.Stdin concurrently (only ever a test — production never does)
	// would otherwise be a real data race on the var itself, not just on
	// what it points to.
	stdin := os.Stdin

	type readResult struct {
		line string
		err  error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		reader := bufio.NewReader(stdin)
		line, err := reader.ReadString('\n')
		resultCh <- readResult{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resultCh:
		if res.err != nil && !errors.Is(res.err, io.EOF) {
			return "", fmt.Errorf("read network id: %w", res.err)
		}
		id := strings.TrimSpace(res.line)
		if id == "" {
			return "", fmt.Errorf("network id is required")
		}
		if err := protocol.ValidateMessageID(id); err != nil {
			return "", fmt.Errorf("network id: %w", err)
		}
		return id, nil
	}
}

// defaultNetworkNameForID derives a human-readable display name from a
// network id when --name is not given, e.g. "acme-friends" -> "Acme
// Friends Moltnet".
func defaultNetworkNameForID(id string) string {
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' })
	for index, word := range words {
		if word == "" {
			continue
		}
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	name := strings.Join(words, " ")
	if name == "" {
		name = id
	}
	return name + " Moltnet"
}

// checkoutWarning returns a non-empty warning when root contains any of
// checkoutMarkers, suggesting it is a source checkout rather than a runtime
// install directory (PLAN.md: "warn before writing").
func checkoutWarning(root string) string {
	var found []string
	for _, marker := range checkoutMarkers {
		if info, err := os.Stat(filepath.Join(root, marker)); err == nil && info != nil {
			found = append(found, marker)
		}
	}
	if len(found) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"  %s %s looks like a source checkout (found %s); writing Moltnet config here is unusual for a runtime install — did you mean a different --dir, or the default ~/.moltnet/ home?",
		yellow("warning:"), root, strings.Join(found, ", "),
	)
}

func writeFileIfMissing(path string, contents string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect %q: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		return false, fmt.Errorf("write %q: %w", path, err)
	}

	return true, nil
}
