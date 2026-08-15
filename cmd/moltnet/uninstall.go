package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/uninstall"
	"github.com/noopolis/moltnet/internal/updater"
)

// currentExecutable resolves the path of the running moltnet binary
// (symlink-resolved), the same way resolveServiceSpec does. It is a var,
// not a direct call, so `moltnet uninstall` tests can point it at a
// throwaway copy in a temp dir instead of the real go test binary —
// deleting *that* mid-test-run would be actively dangerous.
var currentExecutable = func() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve moltnet binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	return path, nil
}

// runUninstallCommand implements `moltnet uninstall` (PLAN.md phase 5):
// stop and remove every installed service for every network found under
// ~/.moltnet/ or as a dangling unit/plist, delete the running binary, and
// warn about any other `moltnet` copies (or now-dangling symlinks) left on
// PATH. Network data and config survive by default; --purge additionally
// removes ~/.moltnet (and, when MOLTNET_HOME points elsewhere, that install
// state too) after a second, more explicit confirmation naming the
// networks it destroys.
func runUninstallCommand(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(stdout, buildUninstallUsage())
		return nil
	}

	flags := flag.NewFlagSet("moltnet uninstall", flag.ContinueOnError)
	flags.SetOutput(stdout)
	var (
		yes   = flags.Bool("yes", false, "skip confirmation prompt(s); required when not run on a terminal")
		purge = flags.Bool("purge", false, "also remove ~/.moltnet (all network data and config); always confirmed unless --yes")
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("uninstall does not accept positional arguments")
	}

	manager := newServiceManager()
	networkIDs, err := uninstall.NetworkIDs(manager)
	if err != nil {
		return err
	}

	binaryPath, err := currentExecutable()
	if err != nil {
		return err
	}
	overridePath, overrideActive := moltnetHomeOverride()

	printUninstallPlan(networkIDs, binaryPath, *purge, overridePath, overrideActive)

	if !*yes {
		if !isInteractive() {
			return errors.New("moltnet uninstall requires --yes when not run on a terminal")
		}
		confirmed, err := promptYesNo(fmt.Sprintf("Uninstall moltnet (%s)? [y/N] ", binaryPath))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(stdout, "aborted; nothing was changed")
			return nil
		}
	}

	for _, id := range networkIDs {
		if err := manager.Uninstall(ctx, id); err != nil {
			return fmt.Errorf("stop service for network %q: %w", id, err)
		}
		fmt.Fprintf(stdout, "stopped and removed the moltnet service for network %q\n", id)
	}

	if *purge {
		if err := runUninstallPurge(*yes, overridePath, overrideActive); err != nil {
			return err
		}
	}

	removeUninstallBinary(binaryPath)

	otherCopies := uninstall.OtherCopies(os.Getenv("PATH"), binaryPath)
	if len(otherCopies) > 0 {
		fmt.Fprintf(stdout, "%s other moltnet copies remain on PATH:\n", yellow("warning:"))
		for _, copy := range otherCopies {
			fmt.Fprintf(stdout, "  %s\n", copy)
		}
	}

	// Scanned only now, after the binary is actually gone: a PATH entry
	// that is a symlink to the binary just deleted fails os.Stat exactly
	// like "nothing here", so OtherCopies (which relies on os.Stat) cannot
	// report it — DanglingSymlinks uses os.Lstat instead, specifically to
	// catch that case.
	danglingSymlinks := uninstall.DanglingSymlinks(os.Getenv("PATH"), binaryPath)
	if len(danglingSymlinks) > 0 {
		fmt.Fprintf(stdout, "%s dangling moltnet symlinks remain on PATH:\n", yellow("warning:"))
		for _, link := range danglingSymlinks {
			fmt.Fprintf(stdout, "  dangling symlink: %s — remove it with rm %s\n", link, link)
		}
	}

	return nil
}

// printUninstallPlan prints the "print each action" summary PLAN.md phase 5
// item 1 asks for, before any prompt: what will be stopped/removed and
// where the binary lives, so the confirmation prompt is informed rather
// than a blind "are you sure?".
func printUninstallPlan(networkIDs []string, binaryPath string, purge bool, overridePath string, overrideActive bool) {
	fmt.Fprintln(stdout, "moltnet uninstall will:")
	if len(networkIDs) == 0 {
		fmt.Fprintf(stdout, "  %s no installed services found\n", yellow("note:"))
	}
	for _, id := range networkIDs {
		fmt.Fprintf(stdout, "  - stop and remove the moltnet service for network %q\n", id)
	}
	fmt.Fprintf(stdout, "  - delete the moltnet binary at %s\n", binaryPath)
	if purge {
		fmt.Fprintln(stdout, "  - remove ~/.moltnet entirely (--purge): all network data and config")
		if overrideActive {
			fmt.Fprintf(stdout, "  - remove the MOLTNET_HOME install state at %s entirely (--purge)\n", overridePath)
		}
	} else {
		fmt.Fprintln(stdout, "data under ~/.moltnet is not touched; pass --purge to also remove it")
		if overrideActive {
			fmt.Fprintf(stdout, "MOLTNET_HOME is set to %s; its install state is not removed either\n", overridePath)
		}
	}
}

// moltnetHomeOverride reports whether MOLTNET_HOME points updater's
// install/update state (install.json, update.lock) somewhere other than
// the default ~/.moltnet that --purge already removes via
// app.HomeMoltnetDir. It reuses updater.ResolveHome — the exact resolution
// `moltnet update` itself uses to read and write that state — so this can
// never drift from where that state actually lives.
func moltnetHomeOverride() (path string, active bool) {
	resolved := updater.ResolveHome()
	if resolved == "" {
		return "", false
	}
	defaultHome, err := app.HomeMoltnetDir()
	if err != nil {
		return "", false
	}
	if filepath.Clean(resolved) == filepath.Clean(defaultHome) {
		return "", false
	}
	return resolved, true
}

// runUninstallPurge removes ~/.moltnet (and, when active, the MOLTNET_HOME
// override) after the second, more explicit confirmation PLAN.md phase 5
// item 2 requires: it always lists the network ids being destroyed, and
// --yes is the only way to skip it.
func runUninstallPurge(yes bool, overridePath string, overrideActive bool) error {
	root, err := app.HomeMoltnetDir()
	if err != nil {
		return err
	}
	rootState, err := uninstall.InspectHomeDir(root)
	if err != nil {
		return err
	}

	// Only network ids with an actual ~/.moltnet/<id>/ directory belong in
	// a --purge confirmation: a dangling service unit with no directory
	// (already counted in the broader uninstall plan above) has no data
	// here for --purge to destroy.
	dirNetworkIDs, err := app.ListNetworkIDs()
	if err != nil {
		return err
	}

	if !yes {
		confirmed, err := promptYesNo(buildPurgeConfirmationPrompt(dirNetworkIDs, root, rootState, overridePath, overrideActive))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(stdout, "--purge aborted; ~/.moltnet was left in place")
			return nil
		}
	}

	result, err := uninstall.PurgeHomeDir(root)
	if err != nil {
		return err
	}
	printPurgeResult(root, result)

	if overrideActive {
		overrideResult, err := uninstall.PurgeHomeDir(overridePath)
		if err != nil {
			return err
		}
		printPurgeResult(overridePath, overrideResult)
	}
	return nil
}

// printPurgeResult reports what PurgeHomeDir(path) actually did. A missing
// path, a symlink (whose target is never touched), and a real removed
// directory each get a distinct, accurate message — PLAN.md phase 5 item 2
// and the P2-2 fix both require --purge never claim data was removed when
// it was not.
func printPurgeResult(path string, result uninstall.PurgeResult) {
	switch {
	case !result.Existed:
		fmt.Fprintf(stdout, "%s did not exist\n", path)
	case result.IsSymlink && result.SymlinkTarget != "":
		fmt.Fprintf(stdout, "%s was a symlink to %s; removed the link, data at %s untouched\n", path, result.SymlinkTarget, result.SymlinkTarget)
	case result.IsSymlink:
		fmt.Fprintf(stdout, "%s was a dangling symlink; removed the link\n", path)
	default:
		fmt.Fprintf(stdout, "removed %s\n", path)
	}
}

// buildPurgeConfirmationPrompt builds the second, more explicit confirmation
// PLAN.md phase 5 item 2 requires for --purge: it always names the
// dir-derived network ids about to be destroyed, and — when root turns out
// to be a symlink, or MOLTNET_HOME points install state somewhere else —
// says so explicitly, since --purge only ever removes root itself (never a
// symlink's resolved target) and would otherwise silently miss install
// state living outside ~/.moltnet.
func buildPurgeConfirmationPrompt(dirNetworkIDs []string, root string, rootState uninstall.HomeState, overridePath string, overrideActive bool) string {
	var b strings.Builder
	if len(dirNetworkIDs) == 0 {
		if rootState.Existed {
			fmt.Fprintf(&b, "--purge will permanently delete %s; no network data found under it; the directory itself will be removed\n", root)
		} else {
			fmt.Fprintf(&b, "--purge will permanently delete %s; no network data found under it; nothing exists there yet\n", root)
		}
	} else {
		fmt.Fprintf(&b, "--purge will permanently delete %s, destroying data for network(s): %s\n", root, describeNetworkIDs(dirNetworkIDs))
	}
	switch {
	case rootState.IsSymlink && rootState.SymlinkTarget != "":
		fmt.Fprintf(&b, "%s is a symlink to %s; --purge only removes the link, not the data at %s.\n", root, rootState.SymlinkTarget, rootState.SymlinkTarget)
	case rootState.IsSymlink:
		fmt.Fprintf(&b, "%s is a dangling symlink; --purge only removes the link.\n", root)
	}
	if overrideActive {
		fmt.Fprintf(&b, "MOLTNET_HOME is set: this also removes install state at %s.\n", overridePath)
	}
	fmt.Fprintf(&b, "This cannot be undone. Really delete %s? [y/N] ", root)
	return b.String()
}

// describeNetworkIDs joins networkIDs for the confirmation prompt's
// "destroying data for network(s): ..." line. Callers only reach this with
// a non-empty list — buildPurgeConfirmationPrompt handles the empty case
// with its own "no network data found" wording instead.
func describeNetworkIDs(networkIDs []string) string {
	return strings.Join(networkIDs, ", ")
}

// removeUninstallBinary deletes binaryPath and prints the outcome. A
// permission-denied removal (a root-owned install directory such as
// /usr/local/bin) is not treated as fatal: it prints a `sudo rm` fallback
// instead of returning an error, since every other uninstall action already
// succeeded by this point.
func removeUninstallBinary(binaryPath string) {
	if err := uninstall.RemoveBinary(binaryPath); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			fmt.Fprintf(stdout, "could not remove %s: permission denied\n", binaryPath)
			fmt.Fprintf(stdout, "run: sudo rm %s\n", binaryPath)
			return
		}
		fmt.Fprintf(stdout, "could not remove %s: %v\n", binaryPath, err)
		return
	}
	fmt.Fprintf(stdout, "removed %s\n", binaryPath)
}

// promptReader is what promptYesNo reads confirmation answers from. It is a
// var, defaulting to os.Stdin, so tests can drive `moltnet uninstall`'s
// confirmation gates end to end with a scripted answer instead of needing a
// real terminal — see cmd/moltnet/uninstall_test.go.
var promptReader io.Reader = os.Stdin

// promptYesNo prints question, reads one line from promptReader, and
// reports whether it was an affirmative answer ("y" or "yes", case
// insensitive); anything else, including an empty line, is "no". It mirrors
// resolveInitNetworkID's stdin-reading pattern (init.go).
func promptYesNo(question string) (bool, error) {
	fmt.Fprint(stdout, question)
	reader := bufio.NewReader(promptReader)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
