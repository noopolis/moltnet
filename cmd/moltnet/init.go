package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
		listenFlag  = flags.String("listen", "", "server.listen_addr to bind (default: 127.0.0.1:8787, loopback-only); a non-loopback value is warned about immediately, not just at server start")
		verboseFlag = flags.Bool("verbose", false, "print full detail: exact paths, per-file status, and the --bearer tip")
	)
	var roomFlag repeatedStringFlag
	flags.Var(&roomFlag, "room", `room id to author instead of the default "general" room; repeatable. Every room gets visibility: public, write_policy: registered_agents, federation: none (the open starter room's shape, not "room create"'s defaults). Passing any --room replaces "general" unless "general" is itself among the values. Rejected together with --bearer.`)
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
		// non-flag arg, so `moltnet init <path> --id x` never reaches --id as
		// a flag — it lands here as extra positional args. Name the
		// offending flag instead of the generic message below when one of
		// the extras looks like one.
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

	// --bearer/--listen/--room validation joins the arg-count checks above,
	// ahead of the banner, for the same reason (P2-2): every check here is
	// pure, so a doomed `moltnet init --bearer --room x` fails instantly
	// instead of after the ~0.9s settle animation. See
	// resolveInitBearerListenRoomFlags's own doc comment (init_listen.go)
	// for what it rejects and resolves.
	listenAddr, roomIDs, err := resolveInitBearerListenRoomFlags(*bearerFlag, *listenFlag, roomFlag)
	if err != nil {
		return err
	}

	// P2-2: the banner must be the first output on every init path — before
	// the positional-path deprecation note below and before
	// resolveInitNetworkID's interactive --id prompt further down — so it
	// prints immediately once flags are known to be well-formed.
	// printBannerAnimated (banner_player.go) plays the settle animation on a
	// real terminal, falling back to plain printBanner's static output
	// otherwise; see its own doc comment.
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

	// PLAN.md phase 7.0's "Corollary work, same change", extended by round
	// 2's P1 fix: offer to close the one path this credential could
	// otherwise reach a remote through -- whenever root is a git checkout
	// itself, or sits inside one (gitCheckoutRoot walks up to find it; a bare
	// go.mod or package.json marker with no .git anywhere in the ancestry
	// never reaches this branch). Runs independently of the warning above: a
	// subdirectory target with no marker of its own still gets its warning
	// from the gitCheckoutRoot branch in checkoutWarning, but the two checks
	// stay separate rather than re-deriving root's warning reason here.
	if gitRoot, ok := gitCheckoutRoot(root); ok {
		if added, gitignoreErr := offerGitignoreCredentialEntries(gitRoot, root); gitignoreErr != nil {
			// Rendered from the same prefixedGitignoreCredentialEntries the
			// write path itself uses (init_checkout.go), not a hardcoded
			// list: a `--dir <subdir-of-checkout>` target needs its entries
			// prefixed with that subdirectory, and this fallback previously
			// hardcoded the unprefixed, three-entry list -- silently
			// dropping the ".moltnet-init-*.tmp" entry the credential-leak
			// fix exists for, and dropping the subdirectory anchor a nested
			// target needs to actually ignore anything.
			byHand := strings.Join(prefixedGitignoreCredentialEntries(gitRoot, root), ", ")
			fmt.Fprintf(stdout, "  %s could not update .gitignore (%v); add %s by hand\n", yellow("note:"), gitignoreErr, byHand)
		} else if len(added) > 0 {
			fmt.Fprintf(stdout, "  %s added %s to .gitignore\n", green("✓"), strings.Join(added, ", "))
		}
	}

	serverPath := filepath.Join(root, app.DefaultPath)
	nodePath := filepath.Join(root, nodeconfig.DefaultPath)

	// Cheap Lstat pre-check, done before any file writes and unconditionally
	// -- not gated on serverExists (round 2's P3 fix): serverExists below
	// uses os.Stat, which follows a symlink, so a *dangling* one (pointing at
	// a target that does not exist) resolved to os.ErrNotExist there and
	// used to read as plain "does not exist," skipping this graceful
	// pre-check entirely. Plain init then fell through to the repair path
	// (applyExistingServerConfigTokens' default case, init_server_config.go),
	// which hard-failed once its own os.ReadFile hit the same dangling
	// target -- but only after MoltnetNode had already been written just
	// below. os.Lstat never follows the link, so it sees the symlink itself
	// regardless of what it points to (or fails to), and if the existing
	// Moltnet config is a symlink, addOperatorTokenWithRollback would refuse
	// it too (app.AddOperatorToken's rejectSymlinkedConfigPath) -- detecting
	// it here instead lets --bearer degrade the same way the tokens-exist
	// case does (skip the add, still print the full summary) and lets plain
	// init stay silent, exactly like it already does for a non-dangling
	// symlinked config.
	serverIsSymlink := false
	if info, lstatErr := os.Lstat(serverPath); lstatErr == nil {
		serverIsSymlink = info.Mode()&os.ModeSymlink != 0
	} else if !errors.Is(lstatErr, os.ErrNotExist) {
		return fmt.Errorf("inspect %q: %w", serverPath, lstatErr)
	}

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

	// --listen/--room silently no-op below once serverPath already exists;
	// tell the operator so. See ignoredExistingConfigFlagsNote (init_listen.go).
	//
	// serverOccupied folds serverIsSymlink into this gate (round 3's fix for
	// a live-verified gap): a dangling symlink or a directory at serverPath
	// reads as serverExists == false (only a plain regular file counts
	// there), but writeFileIfMissing's own Lstat pre-check (init_checkout.go)
	// refuses to publish over *any* existing directory entry regardless --
	// so treating only serverExists as "occupied" here let a dangling
	// symlink take the "fresh" branch below: it printed the non-loopback
	// security warning for a config it was about to author but never
	// actually got to write, printed no ignored-flags note (this branch was
	// skipped), and still generated a throwaway operator token. Folding
	// serverIsSymlink in means this note prints and the fresh-content branch
	// below is skipped for exactly the same targets writeFileIfMissing was
	// always going to refuse anyway.
	serverOccupied := serverExists || serverIsSymlink
	if serverOccupied {
		if note := ignoredExistingConfigFlagsNote(serverPath, *listenFlag, roomFlag); note != "" {
			fmt.Fprintf(stdout, "  %s %s\n", yellow("note:"), note)
		}
	}

	// Contents (and any token generation it needs) are only ever built when
	// there is genuinely a fresh file to write below: writeFileIfMissing
	// no-ops on an existing path without ever looking at serverContents, so
	// computing it regardless of serverOccupied would mean generating a
	// secret only to discard it (P3-3's rule, now applying to the plain
	// path too now that defaultMoltnetConfig also mints one — see
	// buildFreshServerConfig's doc comment, init_server_config.go). Gated on
	// serverOccupied (not just serverExists), matching the note above: a
	// dangling symlink or directory at serverPath is refused by
	// writeFileIfMissing exactly like an existing file, so this must not
	// build fresh contents, mint a token, or print the authoring-time
	// warning for content that will never be written.
	var serverContents string
	if !serverOccupied {
		serverContents, err = buildFreshServerConfig(id, name, *bearerFlag, listenAddr, roomIDs)
		if err != nil {
			return err
		}
		// Authoring-time bind warning (PLAN.md's setup-wizard spec, "Q4 —
		// the warning is computed, never static"): describes serverContents
		// before it is written, same as checkoutWarning above describes root.
		if warning := initNonLoopbackWarning(listenAddr, *bearerFlag, roomIDs); warning != "" {
			fmt.Fprintf(stdout, "  %s %s\n", yellow("warning:"), warning)
		}
	}

	serverCreated, err := writeFileIfMissing(serverPath, serverContents)
	if err != nil {
		return err
	}

	// The MoltnetNode scaffold must point at wherever this run's own server
	// binds, not a hardcoded default -- with --listen 127.0.0.1:8788, the
	// scaffold must not point at whatever else already owns 8787. consoleBaseURL
	// (console.go) is the same wildcard-to-loopback mapping `moltnet console`
	// already relies on, reused rather than duplicated. When serverPath
	// already existed (or is occupied by a symlink/directory), "wherever
	// this run's own server binds" means the existing config's own
	// listen_addr, not --listen's resolved flag value -- see
	// existingServerListenAddr's doc comment (init_listen.go).
	nodeListenAddr := listenAddr
	if serverOccupied {
		nodeListenAddr = existingServerListenAddr(serverPath, listenAddr)
	}

	// The config already existed (not just created above): apply whichever
	// of PLAN.md phase 7.0's token actions applies -- the --bearer upgrade,
	// the plain-init repair, or (a symlinked target) neither. This runs
	// before the MoltnetNode scaffold is written below (round 3's fix):
	// this is also the step that actually surfaces a load error against an
	// existing, undecodable config, while existingServerListenAddr above
	// silently falls back to nodeListenAddr on that exact same load error
	// (its own doc comment says so). Writing the scaffold first meant an
	// undecodable config's fallback base_url ("http://127.0.0.1:8787", not
	// whatever listen_addr the broken config actually names) got durably
	// written to disk before runInit ever failed on it -- and a later,
	// repaired rerun would never rewrite it, since writeFileIfMissing
	// no-ops once MoltnetNode already exists.
	var tokenOutcome initExistingServerTokenOutcome
	if !serverCreated {
		tokenOutcome, err = applyExistingServerConfigTokens(serverPath, *bearerFlag, serverIsSymlink)
		if err != nil {
			return err
		}
	}

	nodeBaseURL, err := consoleBaseURL(nodeListenAddr)
	if err != nil {
		return fmt.Errorf("resolve MoltnetNode base_url from listen_addr %q: %w", nodeListenAddr, err)
	}
	nodeCreated, err := writeFileIfMissing(nodePath, defaultMoltnetNodeConfig(id, nodeBaseURL))
	if err != nil {
		return err
	}

	printInitSummary(initSummary{
		id:                             id,
		root:                           root,
		dirExisted:                     dirExisted,
		serverPath:                     serverPath,
		serverCreated:                  serverCreated,
		nodeCreated:                    nodeCreated,
		bearer:                         *bearerFlag,
		bearerAdded:                    tokenOutcome.bearerAdded,
		bearerAddErr:                   tokenOutcome.bearerAddErr,
		bearerUpgradeOnly:              tokenOutcome.bearerUpgradeOnly,
		operatorTokenRepaired:          tokenOutcome.operatorTokenRepaired,
		operatorTokenRepairSkippedNote: tokenOutcome.operatorTokenRepairSkippedNote,
		verbose:                        *verboseFlag,
	})

	return nil
}

// resolveInitNetworkID and defaultNetworkNameForID live in
// init_network_id.go; checkoutWarning, gitCheckoutRoot,
// offerGitignoreCredentialEntries, and writeFileIfMissing live in
// init_checkout.go -- both split out to keep this file under the repo's
// 400-line limit.
