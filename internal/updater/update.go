package updater

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Run(ctx context.Context, options Options) (Result, error) {
	options = options.withDefaults()
	result := Result{
		CheckOnly:      options.CheckOnly,
		CurrentVersion: strings.TrimSpace(options.CurrentVersion),
		DryRun:         options.DryRun,
	}

	install, err := options.Detector.DetectInstall(ctx, options.CurrentVersion)
	if err != nil {
		return result, err
	}
	result.Install = install

	assetName, err := AssetName(options.Platform)
	if err != nil {
		return result, err
	}
	result.AssetName = assetName

	if strings.TrimSpace(options.ServerURL) != "" {
		server, err := options.ServerProbe.ProbeServer(ctx, options.ServerURL)
		if err != nil {
			result.Server = ServerInfo{URL: strings.TrimSpace(options.ServerURL), Warning: err.Error()}
		} else {
			result.Server = server
		}
	}

	if options.CheckOnly || options.DryRun {
		if err := ensureMutationAllowed(install); err != nil {
			result.Warnings = append(result.Warnings, err.Error())
		}
	} else {
		if err := ensureMutationAllowed(install); err != nil {
			result.MutationRefused = true
			return result, err
		}
	}

	targetVersion, err := resolveTargetVersion(ctx, options)
	if err != nil {
		return result, err
	}
	result.TargetVersion = targetVersion
	if strings.TrimSpace(options.TargetVersion) == "" {
		result.LatestVersion = targetVersion
	}

	updateAvailable, err := versionDiffers(options.CurrentVersion, targetVersion)
	if err != nil {
		return result, err
	}
	result.UpdateAvailable = updateAvailable

	if checksums, err := options.ReleaseSource.Checksums(ctx, targetVersion); err == nil {
		if len(checksums) > 0 {
			result.ChecksumAvailable = checksumManifestHasAsset(assetName, checksums)
		}
	} else if options.CheckOnly || options.DryRun {
		result.Warnings = append(result.Warnings, fmt.Sprintf("checksum manifest unavailable: %v", err))
	}

	if options.CheckOnly || options.DryRun {
		return result, nil
	}
	if !updateAvailable {
		return result, nil
	}

	archive, err := options.ReleaseSource.Archive(ctx, targetVersion, assetName)
	if err != nil {
		return result, err
	}
	checksums, err := options.ReleaseSource.Checksums(ctx, targetVersion)
	if err != nil {
		return result, err
	}
	result.ChecksumAvailable = checksumManifestHasAsset(assetName, checksums)
	if err := VerifyChecksum(assetName, archive, checksums); err != nil {
		return result, err
	}

	workspace, err := os.MkdirTemp(options.TempDir, "moltnet-update-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(workspace)

	extractedPath, err := ExtractMoltnetBinary(archive, workspace)
	if err != nil {
		return result, err
	}
	if err := verifyDownloadedVersion(ctx, extractedPath, targetVersion); err != nil {
		return result, err
	}

	backupPath, err := ReplaceBinary(install.Path, extractedPath)
	if err != nil {
		return result, err
	}
	result.BackupPath = backupPath
	result.Updated = true
	return result, nil
}

func (options Options) withDefaults() Options {
	if strings.TrimSpace(options.CurrentVersion) == "" {
		options.CurrentVersion = "0.0.0-dev"
	}
	options.Platform = options.Platform.WithDefaults()
	if options.Detector == nil {
		options.Detector = DefaultInstallDetector{}
	}
	if options.ReleaseSource == nil {
		source := NewHTTPReleaseSource(nil)
		options.ReleaseSource = source
	}
	if options.ServerProbe == nil {
		options.ServerProbe = HTTPServerProbe{}
	}
	return options
}

func resolveTargetVersion(ctx context.Context, options Options) (string, error) {
	if strings.TrimSpace(options.TargetVersion) != "" {
		return strings.TrimSpace(options.TargetVersion), nil
	}
	version, err := options.ReleaseSource.LatestVersion(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(version), nil
}

func versionDiffers(current string, target string) (bool, error) {
	if IsDevelopmentVersion(current) || IsDevelopmentVersion(target) {
		return NormalizeVersion(current) != NormalizeVersion(target), nil
	}
	comparison, err := CompareVersions(current, target)
	if err != nil {
		return false, err
	}
	return comparison != 0, nil
}

func checksumManifestHasAsset(assetName string, checksums []byte) bool {
	_, ok := checksumForAsset(assetName, checksums)
	return ok
}

func ensureMutationAllowed(install Install) error {
	if install.SelfUpdateAllowed && install.Method == InstallMethodReleaseTarball {
		return nil
	}
	switch install.Method {
	case InstallMethodContainer:
		return fmt.Errorf("self-update is not available inside container installs; pull a newer Moltnet image and restart the container")
	case InstallMethodSource:
		return fmt.Errorf("self-update is not available for source or development builds; install a release tarball with curl -fsSL https://moltnet.dev/install.sh | sh")
	case InstallMethodReleaseTarball:
		return fmt.Errorf("self-update is not available because release install metadata does not allow it; reinstall with curl -fsSL https://moltnet.dev/install.sh | sh")
	default:
		return fmt.Errorf("self-update is not available for this install because its install method is unknown; reinstall with curl -fsSL https://moltnet.dev/install.sh | sh")
	}
}

func verifyDownloadedVersion(ctx context.Context, binaryPath string, targetVersion string) error {
	command := exec.CommandContext(ctx, binaryPath, "version")
	command.Dir = filepath.Dir(binaryPath)
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("verify downloaded moltnet version: %w", err)
	}
	reported := strings.TrimSpace(string(output))
	same, err := versionDiffers(reported, targetVersion)
	if err != nil {
		return err
	}
	if same {
		return fmt.Errorf("downloaded moltnet reports version %q, expected %q", reported, targetVersion)
	}
	return nil
}
