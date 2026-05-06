package updater

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
		probeRequest, warning := serverProbeRequest(options)
		if warning != "" {
			result.Server = ServerInfo{URL: strings.TrimSpace(options.ServerURL), Warning: warning}
		} else if server, err := options.ServerProbe.ProbeServer(ctx, probeRequest); err != nil {
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
			result.Warnings = append(result.Warnings, err.Error())
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
	lock, err := acquireUpdateLock(updateLockOptions{
		BinaryPath:    install.Path,
		Path:          updateLockPath(options, install.Path),
		StaleAfter:    options.LockStaleAfter,
		TargetVersion: targetVersion,
	})
	if err != nil {
		return result, err
	}
	defer lock.Release()

	installedVersion, err := readBinaryVersion(ctx, install.Path)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not re-check installed binary version before replacement: %v", err))
	} else {
		result.CurrentVersion = installedVersion
		updateAvailable, err = versionDiffers(installedVersion, targetVersion)
		if err != nil {
			return result, err
		}
		result.UpdateAvailable = updateAvailable
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
	checksum, _ := checksumForAsset(assetName, checksums)

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
	finishedAt := time.Now().UTC().Format(time.RFC3339)
	sourceMetadata := releaseSourceMetadata(options.ReleaseSource)
	if err := writeInstallMetadata(installMetadata{
		Arch:              options.Platform.Arch,
		AssetChecksum:     normalizeAssetChecksum(checksum),
		AssetName:         assetName,
		DownloadBaseURL:   sourceMetadata.DownloadBaseURL,
		InstallMethod:     string(InstallMethodReleaseTarball),
		InstallPath:       install.Path,
		InstalledAt:       finishedAt,
		InstalledBy:       "moltnet update",
		InstalledVersion:  targetVersion,
		LastUpdate:        &installMetadataLastUpdate{Status: "succeeded", FromVersion: result.CurrentVersion, ToVersion: targetVersion, FinishedAt: finishedAt},
		OS:                options.Platform.OS,
		OwnerRepo:         sourceMetadata.OwnerRepo,
		PreviousBinary:    backupPath,
		SelfUpdateAllowed: true,
		Version:           1,
	}); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("install metadata update failed after binary replacement: %v", err))
	}
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

func serverProbeRequest(options Options) (ServerProbeRequest, string) {
	token := strings.TrimSpace(options.ServerToken)
	tokenEnv := strings.TrimSpace(options.ServerTokenEnv)
	if token == "" && tokenEnv != "" {
		token = strings.TrimSpace(os.Getenv(tokenEnv))
		if token == "" {
			return ServerProbeRequest{URL: strings.TrimSpace(options.ServerURL)}, fmt.Sprintf("server token env %s is not set", tokenEnv)
		}
	}
	return ServerProbeRequest{Token: token, URL: strings.TrimSpace(options.ServerURL)}, ""
}

func updateLockPath(options Options, installPath string) string {
	if strings.TrimSpace(options.LockPath) != "" {
		return strings.TrimSpace(options.LockPath)
	}
	return defaultUpdateLockPath(installPath)
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

func releaseSourceMetadata(source ReleaseSource) ReleaseSourceMetadata {
	type metadataSource interface {
		InstallMetadata() ReleaseSourceMetadata
	}
	if source, ok := source.(metadataSource); ok {
		metadata := source.InstallMetadata()
		if strings.TrimSpace(metadata.OwnerRepo) == "" {
			metadata.OwnerRepo = defaultOwnerRepo
		}
		return metadata
	}
	return ReleaseSourceMetadata{OwnerRepo: defaultOwnerRepo}
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
	reported, err := readBinaryVersion(ctx, binaryPath)
	if err != nil {
		return fmt.Errorf("verify downloaded moltnet version: %w", err)
	}
	same, err := versionDiffers(reported, targetVersion)
	if err != nil {
		return err
	}
	if same {
		return fmt.Errorf("downloaded moltnet reports version %q, expected %q", reported, targetVersion)
	}
	return nil
}

func readBinaryVersion(ctx context.Context, binaryPath string) (string, error) {
	command := exec.CommandContext(ctx, binaryPath, "version")
	command.Dir = filepath.Dir(binaryPath)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
