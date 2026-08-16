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
	"github.com/noopolis/moltnet/internal/service"
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
	if len(args) > 0 && isHelpArg(args[0]) {
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
	servicesInstalled, err := installedServiceIDs(manager, networkIDs)
	if err != nil {
		return err
	}

	binaryPath, err := currentExecutable()
	if err != nil {
		return err
	}
	overridePath, overrideActive := moltnetHomeOverride()

	fmt.Fprint(stdout, "  Uninstalling moltnet\n\n")
	printUninstallPlan(networkIDs, servicesInstalled, binaryPath, *purge, overridePath, overrideActive)

	if !*yes {
		if !isInteractive() {
			return errors.New("moltnet uninstall requires --yes when not run on a terminal")
		}
		fmt.Fprintln(stdout)
		confirmed, err := promptYesNo("  Proceed? [y/N] ")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout)
		if !confirmed {
			fmt.Fprintln(stdout, "  aborted; nothing was changed")
			return nil
		}
	} else {
		fmt.Fprintln(stdout)
	}

	hadServiceOutput := false
	for _, id := range networkIDs {
		if err := manager.Uninstall(ctx, id); err != nil {
			return fmt.Errorf("stop service for network %q: %w", id, err)
		}
		// P1 fix: a network whose service was never installed (the common
		// `moltnet init` without `service install` path) gets no ✓ line —
		// manager.Uninstall is a truthful no-op for it, so claiming "stopped
		// and removed" would be untruthful. See installedServiceIDs and
		// printUninstallPlan, which agree on the same servicesInstalled set.
		if servicesInstalled[id] {
			printUninstallCheck("stopped and removed the service for network %q", id)
			hadServiceOutput = true
		}
	}

	if *purge {
		if err := runUninstallPurge(*yes, overridePath, overrideActive, hadServiceOutput); err != nil {
			return err
		}
	}

	removeUninstallBinary(binaryPath)

	otherCopies := uninstall.OtherCopies(os.Getenv("PATH"), binaryPath)
	// Scanned only now, after the binary is actually gone: a PATH entry
	// that is a symlink to the binary just deleted fails os.Stat exactly
	// like "nothing here", so OtherCopies (which relies on os.Stat) cannot
	// report it — DanglingSymlinks uses os.Lstat instead, specifically to
	// catch that case.
	danglingSymlinks := uninstall.DanglingSymlinks(os.Getenv("PATH"), binaryPath)
	if len(otherCopies) > 0 || len(danglingSymlinks) > 0 {
		fmt.Fprintln(stdout)
	}
	if len(otherCopies) > 0 {
		fmt.Fprintf(stdout, "  %s other moltnet copies remain on PATH:\n", yellow("warning:"))
		for _, copy := range otherCopies {
			fmt.Fprintf(stdout, "    %s\n", copy)
		}
	}
	if len(danglingSymlinks) > 0 {
		fmt.Fprintf(stdout, "  %s dangling moltnet symlinks remain on PATH:\n", yellow("warning:"))
		for _, link := range danglingSymlinks {
			fmt.Fprintf(stdout, "    dangling symlink: %s — remove it with rm %s\n", link, link)
		}
	}

	return nil
}

// printUninstallCheck prints one two-space-indented, green-checkmarked
// completed-action line — the shared house-style building block behind
// every "✓ ..." line `moltnet uninstall` prints, mirroring
// printInitConfigCheckLine's role for `moltnet init`.
func printUninstallCheck(format string, args ...any) {
	fmt.Fprintf(stdout, "  %s %s\n", green("✓"), fmt.Sprintf(format, args...))
}

// installedServiceIDs reports, for each id in networkIDs, whether it
// actually has an installed unit/plist (manager.IsInstalled) — not merely a
// ~/.moltnet/<id>/ directory. `moltnet init` alone (no `service install`)
// leaves a network directory with no service, and uninstall.NetworkIDs's
// union deliberately includes that id anyway (so --purge can still find its
// data); this set is what lets the plan and the execution loop agree on
// which of those ids actually have a service to stop and remove, so neither
// prints an untruthful bullet or ✓ for one that was never installed (P1).
func installedServiceIDs(manager *service.Manager, networkIDs []string) (map[string]bool, error) {
	installed := make(map[string]bool, len(networkIDs))
	for _, id := range networkIDs {
		ok, err := manager.IsInstalled(id)
		if err != nil {
			return nil, fmt.Errorf("check installed service for network %q: %w", id, err)
		}
		if ok {
			installed[id] = true
		}
	}
	return installed, nil
}

// printUninstallPlan prints the "print each action" summary PLAN.md phase 5
// item 1 asks for, before any prompt: what will be stopped/removed and
// where the binary lives, so the confirmation prompt is informed rather
// than a blind "are you sure?". It follows the init/pair house style: a
// dim "Plan:" label followed by two-space-indented "- " bullets for each
// action that will actually happen, with informational asides (nothing to
// do, or data that survives) kept to the same block as a yellow "note:"
// line instead. Only ids with servicesInstalled[id] true get a "stop and
// remove the service" bullet (P1): a network with no installed service
// (e.g. `moltnet init` without `service install`) has nothing here for
// uninstall to stop, so it gets no bullet at all — silence, not a false
// promise.
func printUninstallPlan(networkIDs []string, servicesInstalled map[string]bool, binaryPath string, purge bool, overridePath string, overrideActive bool) {
	fmt.Fprintf(stdout, "  %s\n", dim("Plan:"))
	installedCount := 0
	for _, id := range networkIDs {
		if !servicesInstalled[id] {
			continue
		}
		installedCount++
		fmt.Fprintf(stdout, "    - stop and remove the service for network %q\n", id)
	}
	if installedCount == 0 {
		fmt.Fprintf(stdout, "    %s no installed services found\n", yellow("note:"))
	}
	fmt.Fprintf(stdout, "    - delete the binary at %s\n", binaryPath)
	if purge {
		fmt.Fprintln(stdout, "    - remove ~/.moltnet — all network data and config (--purge)")
		if overrideActive {
			fmt.Fprintf(stdout, "    - remove the MOLTNET_HOME install state at %s (--purge)\n", overridePath)
		}
	} else {
		fmt.Fprintf(stdout, "    %s ~/.moltnet is not touched; pass --purge to also remove it\n", yellow("note:"))
		if overrideActive {
			fmt.Fprintf(stdout, "    %s MOLTNET_HOME is set to %s; its install state is not removed either\n", yellow("note:"), overridePath)
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
//
// hadPrecedingOutput reports whether the service-removal loop above already
// printed a ✓ line: the interactive main prompt (runUninstallCommand) always
// emits its own blank line right after "Proceed? [y/N] y", so this function
// only needs to add another one here when something was printed since —
// otherwise the two blank-line emitters collide into a doubled blank line
// with nothing between them (the common case: --purge with zero installed
// services). With preceding output, this reproduces the exact spacing the
// two unconditional Fprintln calls always produced.
func runUninstallPurge(yes bool, overridePath string, overrideActive bool, hadPrecedingOutput bool) error {
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
		if hadPrecedingOutput {
			fmt.Fprintln(stdout)
		}
		confirmed, err := promptYesNo(buildPurgeConfirmationPrompt(dirNetworkIDs, root, rootState, overridePath, overrideActive))
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout)
		if !confirmed {
			fmt.Fprintln(stdout, "  --purge aborted; ~/.moltnet was left in place")
			fmt.Fprintln(stdout)
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
		fmt.Fprintf(stdout, "  %s %s did not exist\n", yellow("note:"), path)
	case result.IsSymlink && result.SymlinkTarget != "":
		printUninstallCheck("%s was a symlink to %s; removed the link, data at %s untouched", path, result.SymlinkTarget, result.SymlinkTarget)
	case result.IsSymlink:
		printUninstallCheck("%s was a dangling symlink; removed the link", path)
	default:
		printUninstallCheck("removed %s", path)
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
	warning := yellow("warning:")
	if len(dirNetworkIDs) == 0 {
		if rootState.Existed {
			fmt.Fprintf(&b, "  %s --purge will permanently delete %s; no network data found under it; the directory itself will be removed\n", warning, root)
		} else {
			fmt.Fprintf(&b, "  %s --purge will permanently delete %s; no network data found under it; nothing exists there yet\n", warning, root)
		}
	} else {
		fmt.Fprintf(&b, "  %s --purge will permanently delete %s, destroying data for network(s): %s\n", warning, root, describeNetworkIDs(dirNetworkIDs))
	}
	switch {
	case rootState.IsSymlink && rootState.SymlinkTarget != "":
		fmt.Fprintf(&b, "  %s is a symlink to %s; --purge only removes the link, not the data at %s.\n", root, rootState.SymlinkTarget, rootState.SymlinkTarget)
	case rootState.IsSymlink:
		fmt.Fprintf(&b, "  %s is a dangling symlink; --purge only removes the link.\n", root)
	}
	if overrideActive {
		fmt.Fprintf(&b, "  MOLTNET_HOME is set: this also removes install state at %s.\n", overridePath)
	}
	fmt.Fprintf(&b, "  %s Really delete %s? [y/N] ", bold("This cannot be undone."), root)
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
			fmt.Fprintf(stdout, "  %s could not remove %s: permission denied\n", yellow("warning:"), binaryPath)
			fmt.Fprintf(stdout, "    run: %s\n", bold("sudo rm "+binaryPath))
			return
		}
		fmt.Fprintf(stdout, "  %s could not remove %s: %v\n", yellow("warning:"), binaryPath, err)
		return
	}
	printUninstallCheck("removed %s", binaryPath)
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
