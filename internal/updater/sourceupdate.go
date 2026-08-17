package updater

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// sourceInstallRefusalMessage is the same message ensureMutationAllowed has
// always returned for source installs. A source build with no usable
// stamped checkout (unstamped binary, moved/deleted checkout, or a checkout
// that is not a git repository) degrades to this exact wording, so
// operators who built without the new ldflags stamp see no behavior change.
const sourceInstallRefusalMessage = "self-update is not available for source or development builds; install a release tarball with curl -fsSL https://moltnet.dev/install.sh | sh"

// SourceUpdatePlan reports what a source install's `moltnet update` found
// or did: the git status it inspected, and — only once a mutation actually
// ran — the steps it took, in order.
type SourceUpdatePlan struct {
	Branch         string
	Checkout       string
	Clean          bool
	Detached       bool
	LocalCommit    string
	PulledCommit   string
	RebuildNeeded  bool
	Steps          []string
	UpstreamCommit string
	UpstreamKnown  bool
}

// dirtyCheckoutRefusalMessage and detachedHeadRefusalMessage are the exact
// wording both a real run's refusal and a --check/--dry-run preview
// (report.go's sourceCheckPreview) use, so the two can never drift apart:
// what --check promises to block is exactly what a real run then blocks.
func dirtyCheckoutRefusalMessage(checkout string) string {
	return fmt.Sprintf("source checkout %q has uncommitted changes; commit or stash them yourself, then retry `moltnet update`", checkout)
}

func detachedHeadRefusalMessage(checkout string) string {
	return fmt.Sprintf("source checkout %q has no branch checked out (detached HEAD); check out a branch, then retry `moltnet update`", checkout)
}

// runSourceUpdate is Run's entire source-install branch: it never falls
// through to the release-tarball download/checksum/replace path below it,
// and the release path never calls into this file. `--check`/`--dry-run`
// only ever run read-only git inspection plus a `git fetch` (which updates
// remote-tracking refs but never the working tree); a real run additionally
// requires a clean, non-detached checkout before it runs `git pull
// --ff-only`, `make build`, and replaces the installed binary.
func runSourceUpdate(ctx context.Context, options Options, install Install, result Result) (Result, error) {
	if target := strings.TrimSpace(options.TargetVersion); target != "" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("--version %q is not supported for source installs; a source update always rebuilds whatever the checkout's tracked branch currently points at", target))
	}

	checkout := strings.TrimSpace(install.SourceCheckout)
	if checkout == "" {
		return refuseSourceUpdate(result)
	}
	if !isGitCheckout(ctx, options.CommandRunner, checkout) {
		return refuseSourceUpdate(result)
	}

	// Fetch before comparing against upstream: without this, @{u} (read by
	// inspectGitCheckout below) only reflects whatever the last unrelated
	// fetch happened to leave behind, so a real upstream change can be
	// invisible to both --check and a real run indefinitely. A fetch
	// failure (offline, unreachable remote) does not abort the command —
	// it downgrades the comparison to "as of the last successful fetch"
	// and, on a real run, disables the "nothing to pull" shortcut below so
	// `git pull --ff-only` itself gets the final say instead of stale data.
	fetchErr := gitFetch(ctx, options.CommandRunner, checkout)

	status, err := inspectGitCheckout(ctx, options.CommandRunner, checkout)
	if err != nil {
		return refuseSourceUpdate(result)
	}

	plan := SourceUpdatePlan{
		Branch:         status.Branch,
		Checkout:       checkout,
		Clean:          status.Clean,
		Detached:       status.Detached,
		LocalCommit:    status.LocalCommit,
		RebuildNeeded:  status.UpstreamKnown && status.LocalCommit != status.UpstreamCommit,
		UpstreamCommit: status.UpstreamCommit,
		UpstreamKnown:  status.UpstreamKnown,
	}
	result.SourceUpdate = &plan
	result.UpdateAvailable = !status.UpstreamKnown || plan.RebuildNeeded

	if fetchErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("git fetch in %q failed (%v); comparison reflects the last successful fetch, not live upstream state", checkout, fetchErr))
	}

	if options.CheckOnly || options.DryRun {
		if !status.UpstreamKnown {
			result.Warnings = append(result.Warnings, fmt.Sprintf("source checkout %q has no upstream tracking branch configured; cannot compare against origin", checkout))
		}
		return result, nil
	}

	if !status.Clean {
		refusal := errors.New(dirtyCheckoutRefusalMessage(checkout))
		result.MutationRefused = true
		result.RefusalReason = refusal.Error()
		return result, refusal
	}
	if status.Detached {
		refusal := errors.New(detachedHeadRefusalMessage(checkout))
		result.MutationRefused = true
		result.RefusalReason = refusal.Error()
		return result, refusal
	}

	if fetchErr == nil && status.UpstreamKnown && !plan.RebuildNeeded {
		// Nothing new to pull and the tree is already clean: no-op, same
		// spirit as the release path's "already at the requested version".
		// Only taken when the fetch above actually succeeded — otherwise
		// this data could be stale and `git pull --ff-only` below, which
		// re-fetches on its own, is the one allowed to decide.
		return result, nil
	}

	lock, err := acquireUpdateLock(updateLockOptions{
		BinaryPath: install.Path,
		Path:       updateLockPath(options, install.Path),
		StaleAfter: options.LockStaleAfter,
	})
	if err != nil {
		return result, err
	}
	defer lock.Release()

	return performSourceUpdate(ctx, options, install, result, plan)
}

// performSourceUpdate runs the mutating half of a source update: preflight,
// pull, build, verify, replace. Every failure here returns before the
// installed binary has been touched, so the old binary is always left
// intact.
func performSourceUpdate(ctx context.Context, options Options, install Install, result Result, plan SourceUpdatePlan) (Result, error) {
	checkout := plan.Checkout
	runner := options.CommandRunner

	// Preflight the install directory's writability before pulling or
	// building anything, instead of discovering it only after a full
	// pull+build inside replaceInstalledBinary.
	if err := ensureInstallDirWritable(install.Path); err != nil {
		return result, err
	}

	if err := gitPullFastForward(ctx, runner, checkout); err != nil {
		return result, fmt.Errorf("git pull --ff-only in %q failed (%v); installed binary left unchanged", checkout, err)
	}
	plan.Steps = append(plan.Steps, fmt.Sprintf("pulled latest changes in %s", checkout))
	if pulled, err := inspectGitCheckout(ctx, runner, checkout); err == nil {
		plan.PulledCommit = pulled.LocalCommit
	}

	if err := runMakeBuild(ctx, runner, checkout); err != nil {
		return result, fmt.Errorf("`make build` in %q failed (%v); installed binary left unchanged", checkout, err)
	}
	plan.Steps = append(plan.Steps, "ran `make build`")

	builtBinary := filepath.Join(checkout, "bin", "moltnet")
	newVersion, err := readBinaryVersion(ctx, builtBinary)
	if err != nil {
		return result, fmt.Errorf("run freshly built binary %q to verify its version (%v); installed binary left unchanged", builtBinary, err)
	}
	plan.Steps = append(plan.Steps, fmt.Sprintf("built binary reports version %s", newVersion))

	backupPath, err := replaceInstalledBinary(install.Path, builtBinary)
	if err != nil {
		return result, err
	}
	plan.Steps = append(plan.Steps, fmt.Sprintf("replaced installed binary at %s", install.Path))
	result.BackupPath = backupPath
	result.Updated = true
	result.TargetVersion = newVersion

	installedVersion, verifyErr := readBinaryVersion(ctx, install.Path)
	if verifyErr != nil || strings.TrimSpace(installedVersion) != strings.TrimSpace(newVersion) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("installed binary now reports %q, expected %q; run `moltnet version` to confirm", installedVersion, newVersion))
	} else {
		plan.Steps = append(plan.Steps, fmt.Sprintf("verified installed binary now reports version %s", installedVersion))
	}

	// Deliberately does not call writeInstallMetadata: install.json is
	// global (one file per MOLTNET_HOME, not per install path), and a
	// source build has never needed it — DetectInstall recognizes a source
	// install from IsDevelopmentVersion(currentVersion) before it ever
	// looks at install.json. Writing it here would overwrite a co-resident
	// release install's own metadata with this source install's `install_
	// method: source` and a mismatched `install_path`, which then makes
	// DetectInstall unable to match the release binary's own path against
	// it — degrading that install to InstallMethodUnknown and silently
	// disabling ITS self-update. Two installs sharing one MOLTNET_HOME must
	// never be able to disable each other this way.

	result.SourceUpdate = &plan
	return result, nil
}

// refuseSourceUpdate applies the same CheckOnly/DryRun-vs-mutate distinction
// ensureMutationAllowed's caller (Run) applies for every other refused
// install method: a warning during --check/--dry-run, a hard error and
// MutationRefused during a real run.
func refuseSourceUpdate(result Result) (Result, error) {
	refusal := errors.New(sourceInstallRefusalMessage)
	if result.CheckOnly || result.DryRun {
		result.Warnings = append(result.Warnings, refusal.Error())
		return result, nil
	}
	result.MutationRefused = true
	result.RefusalReason = refusal.Error()
	return result, refusal
}
