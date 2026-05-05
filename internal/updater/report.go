package updater

import (
	"fmt"
	"io"
	"strings"
)

func WriteResult(writer io.Writer, result Result) error {
	_, err := io.WriteString(writer, result.String())
	return err
}

func (r Result) String() string {
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
	writeField(&builder, "asset", emptyDash(r.AssetName))
	writeField(&builder, "checksum", checksumStatus(r.ChecksumAvailable))
	writeField(&builder, "install method", string(r.Install.Method))
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
		builder.WriteString("Self-update is not available for this install.\n")
	case r.Updated:
		builder.WriteString("Updated the installed binary.\n")
		if r.BackupPath != "" {
			builder.WriteString(fmt.Sprintf("Previous binary: %s\n", r.BackupPath))
		}
		builder.WriteString("Restart Moltnet to run the new version.\n")
	case !r.UpdateAvailable:
		builder.WriteString("Moltnet is already at the requested version.\n")
	case (r.CheckOnly || r.DryRun) && !selfUpdateAllowedForReport(r.Install):
		builder.WriteString("Update available, but self-update is not available for this install.\n")
	case r.CheckOnly || r.DryRun:
		builder.WriteString("Update available.\n")
	default:
		builder.WriteString("Update available.\n")
	}
	return builder.String()
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
