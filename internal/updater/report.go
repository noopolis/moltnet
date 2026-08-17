package updater

import (
	"fmt"
	"io"
	"strings"
)

// WriteResult writes result's report to writer. verbose additionally
// includes a source update's per-step detail (git pull, make build, binary
// replace, version verify); every other field already prints unconditionally,
// matching this CLI family's "core facts always show, extra narration is
// --verbose-only" convention.
func WriteResult(writer io.Writer, result Result, verbose bool) error {
	_, err := io.WriteString(writer, result.string(verbose))
	return err
}

// String renders result's report with no verbose detail. It exists
// alongside WriteResult for callers (and tests) that only have a Result in
// hand, with no CLI flag to thread through.
func (r Result) String() string {
	return r.string(false)
}

func (r Result) string(verbose bool) string {
	var builder strings.Builder
	if r.CheckOnly {
		builder.WriteString("Moltnet update check\n\n")
	} else if r.DryRun {
		builder.WriteString("Moltnet update dry run\n\n")
	} else {
		builder.WriteString("Moltnet update\n\n")
	}

	writeField(&builder, "installed binary", emptyDash(r.Install.Path))
	writeField(&builder, "installed version", emptyDash(r.CurrentVersion))
	if r.LatestVersion != "" {
		writeField(&builder, "latest version", r.LatestVersion)
	}
	writeField(&builder, "target version", emptyDash(r.TargetVersion))
	if r.SourceUpdate == nil {
		writeField(&builder, "asset", emptyDash(r.AssetName))
		writeField(&builder, "checksum", checksumStatus(r.ChecksumAvailable))
	}
	writeField(&builder, "install method", string(r.Install.Method))
	writeSourceUpdateFields(&builder, r.SourceUpdate, verbose)
	if r.Server.URL != "" {
		if r.Server.Warning != "" {
			writeField(&builder, "server", r.Server.URL+" warning: "+r.Server.Warning)
		} else {
			writeField(&builder, "server", r.Server.URL+" reports "+emptyDash(r.Server.Version))
		}
	}

	for _, warning := range r.Warnings {
		writeField(&builder, "warning", warning)
	}

	builder.WriteRune('\n')
	switch {
	case r.MutationRefused:
		builder.WriteString(refusalSummary(r) + "\n")
	case r.Updated:
		builder.WriteString("Updated the installed binary.\n")
		if r.BackupPath != "" {
			builder.WriteString(fmt.Sprintf("Previous binary: %s\n", r.BackupPath))
		}
		builder.WriteString("Restart Moltnet to run the new version.\n")
	case r.SourceUpdate != nil && !r.UpdateAvailable:
		builder.WriteString("Source checkout is already up to date; nothing to rebuild.\n")
	case !r.UpdateAvailable:
		builder.WriteString("Moltnet is already at the requested version.\n")
	case (r.CheckOnly || r.DryRun) && r.SourceUpdate != nil:
		builder.WriteString(sourceCheckSummary(r.SourceUpdate) + "\n")
	case (r.CheckOnly || r.DryRun) && !selfUpdateAllowedForReport(r.Install):
		builder.WriteString("Update available, but self-update is not available for this install.\n")
	case r.CheckOnly || r.DryRun:
		builder.WriteString("Update available.\n")
	default:
		builder.WriteString("Update available.\n")
	}
	return builder.String()
}

// refusalSummary names the actual blocker for a real run's refusal
// (RefusalReason, set by every mutation-refusal path — an unsupported
// install method, a dirty source checkout, a detached HEAD, an unwritable
// install directory) instead of a single generic sentence that reads the
// same whether the fix is "reinstall from a release tarball" or "commit
// your changes". Falls back to the old generic wording only for a Result
// built by a caller that set MutationRefused without RefusalReason (tests,
// mainly), so the switch case above can never print nothing.
func refusalSummary(r Result) string {
	if strings.TrimSpace(r.RefusalReason) != "" {
		return "Self-update is not available: " + r.RefusalReason
	}
	return "Self-update is not available for this install."
}

// sourceCheckSummary previews what a real run would do for a source
// install: the same "Rebuild available" line only when the checkout is
// clean and on a branch — a real run would refuse otherwise, so
// --check/--dry-run must not promise a rebuild they know would be
// blocked. Otherwise it prints the exact refusal a real run would give,
// built from the same dirtyCheckoutRefusalMessage/detachedHeadRefusalMessage
// helpers a real refusal uses, so the preview and the real refusal can
// never say different things.
func sourceCheckSummary(plan *SourceUpdatePlan) string {
	switch {
	case !plan.Clean:
		return "Rebuild blocked: " + dirtyCheckoutRefusalMessage(plan.Checkout) + "."
	case plan.Detached:
		return "Rebuild blocked: " + detachedHeadRefusalMessage(plan.Checkout) + "."
	default:
		return "Rebuild available: run `moltnet update` to pull and rebuild."
	}
}

// writeSourceUpdateFields prints the git status a source update inspected
// (checkout path, branch, working tree cleanliness, local/upstream commits)
// and, in verbose mode only, the ordered steps a real run actually took.
func writeSourceUpdateFields(builder *strings.Builder, update *SourceUpdatePlan, verbose bool) {
	if update == nil {
		return
	}
	writeField(builder, "checkout", emptyDash(update.Checkout))
	branch := update.Branch
	if update.Detached {
		branch = branch + " (detached HEAD)"
	}
	writeField(builder, "branch", emptyDash(branch))
	writeField(builder, "working tree", workingTreeStatus(update.Clean))
	if update.UpstreamKnown {
		writeField(builder, "local commit", shortCommit(update.LocalCommit))
		writeField(builder, "upstream commit", shortCommit(update.UpstreamCommit))
		writeField(builder, "rebuild needed", yesNo(update.RebuildNeeded))
	}
	if verbose {
		if update.PulledCommit != "" {
			writeField(builder, "pulled commit", shortCommit(update.PulledCommit))
		}
		for _, step := range update.Steps {
			writeField(builder, "step", step)
		}
	}
}

func selfUpdateAllowedForReport(install Install) bool {
	return install.Method == InstallMethodReleaseTarball && install.SelfUpdateAllowed
}

func writeField(builder *strings.Builder, name string, value string) {
	builder.WriteString(fmt.Sprintf("%-18s %s\n", name+":", value))
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return strings.TrimSpace(value)
}

func checksumStatus(available bool) string {
	if available {
		return "available"
	}
	return "unavailable"
}

func workingTreeStatus(clean bool) string {
	if clean {
		return "clean"
	}
	return "dirty"
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func shortCommit(commit string) string {
	trimmed := strings.TrimSpace(commit)
	if trimmed == "" {
		return "-"
	}
	if len(trimmed) > 12 {
		return trimmed[:12]
	}
	return trimmed
}
