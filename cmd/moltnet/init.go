package main

import (
	"bufio"
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
func runInit(args []string) error {
	flags := flag.NewFlagSet("moltnet init", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		idFlag     = flags.String("id", "", "network id (prompted on a TTY when omitted and --dir is not given; required otherwise)")
		nameFlag   = flags.String("name", "", "network display name (default: derived from the network id)")
		dirFlag    = flags.String("dir", "", "write into this directory instead of ~/.moltnet/<network-id>/ (the pre-phase-4 default was --dir .)")
		bearerFlag = flags.Bool("bearer", false, "set auth.mode to bearer and generate a scoped operator token, stored in Moltnet (0600); never printed")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}

	// P2-2: the banner must be the first output on every init path — before
	// the positional-path deprecation note below and before
	// resolveInitNetworkID's interactive --id prompt further down — so it
	// prints immediately once flags are known to be well-formed.
	// printBannerAnimated (banner_player.go) plays the settle animation on
	// a real terminal, falling back to plain printBanner's own static
	// output otherwise — init is the one place this CLI can afford the
	// ~1s a full animation takes; see printBannerAnimated's doc comment.
	printBannerAnimated()

	if flags.NArg() > 1 {
		return fmt.Errorf("init accepts at most one positional path (deprecated; use --dir)")
	}

	dir := strings.TrimSpace(*dirFlag)
	if flags.NArg() == 1 {
		if dir != "" {
			return fmt.Errorf("init: pass either a positional path or --dir, not both")
		}
		dir = flags.Arg(0)
		fmt.Fprintf(stdout, "  %s `moltnet init <path>` is deprecated; use `moltnet init --dir <path>` instead\n", yellow("note:"))
	}

	usingGlobalHome := dir == ""

	id := strings.TrimSpace(*idFlag)
	var err error
	if usingGlobalHome {
		id, err = resolveInitNetworkID(id)
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
		operatorToken, tokenErr := app.GenerateRandomToken(256)
		if tokenErr != nil {
			return tokenErr
		}
		serverContents = bearerMoltnetConfig(id, name, operatorToken)
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
			operatorToken, tokenErr := app.GenerateRandomToken(256)
			if tokenErr != nil {
				return tokenErr
			}
			if writeErr := addOperatorTokenWithRollback(serverPath, app.AuthTokenWriteback{
				ID:     "operator",
				Value:  operatorToken,
				Scopes: []string{"observe", "write", "admin", "pair"},
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
	})

	return nil
}

// resolveInitNetworkID returns idFlag unchanged when non-empty; otherwise,
// on a TTY, it prompts for one; otherwise it is a hard error (PLAN.md:
// "--id <network-id> sets the network id (interactive prompt on a TTY when
// omitted; hard error when non-interactive)").
func resolveInitNetworkID(idFlag string) (string, error) {
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
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read network id: %w", err)
	}
	id := strings.TrimSpace(line)
	if id == "" {
		return "", fmt.Errorf("network id is required")
	}
	if err := protocol.ValidateMessageID(id); err != nil {
		return "", fmt.Errorf("network id: %w", err)
	}
	return id, nil
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

// addOperatorTokenWithRollback calls app.AddOperatorToken and then re-runs
// the same full config load the server uses at startup (env-merge included)
// as a post-write check, restoring the file to its prior contents if that
// reload fails — the same snapshot/reload/restore backstop
// writePairingWithRollback (pair.go) gives every WritePairing caller,
// applied here so `moltnet init --bearer` against an existing config gets
// the identical guarantee.
func addOperatorTokenWithRollback(path string, token app.AuthTokenWriteback) error {
	snapshot, err := snapshotFile(path)
	if err != nil {
		return err
	}

	if err := app.AddOperatorToken(path, token); err != nil {
		return err
	}

	if _, err := app.LoadConfigForPath(path, ""); err != nil {
		if restoreErr := snapshot.restore(path); restoreErr != nil {
			return fmt.Errorf("added operator token but the config failed to reload (%v); restore also failed: %w", err, restoreErr)
		}
		return fmt.Errorf("added operator token but the config failed to reload; rolled back: %w", err)
	}

	return nil
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
