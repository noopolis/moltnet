package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const moltnetHomeEnv = "MOLTNET_HOME"

type DefaultInstallDetector struct{}

type installMetadata struct {
	Arch              string                     `json:"arch,omitempty"`
	AssetChecksum     string                     `json:"asset_checksum,omitempty"`
	AssetName         string                     `json:"asset_name,omitempty"`
	DownloadBaseURL   string                     `json:"download_base_url,omitempty"`
	InstallMethod     string                     `json:"install_method"`
	InstallPath       string                     `json:"install_path"`
	InstalledAt       string                     `json:"installed_at,omitempty"`
	InstalledBy       string                     `json:"installed_by,omitempty"`
	InstalledVersion  string                     `json:"installed_version,omitempty"`
	LastUpdate        *installMetadataLastUpdate `json:"last_update,omitempty"`
	OS                string                     `json:"os,omitempty"`
	OwnerRepo         string                     `json:"owner_repo,omitempty"`
	PreviousBinary    string                     `json:"previous_binary,omitempty"`
	SelfUpdateAllowed bool                       `json:"self_update_allowed"`
	Version           int                        `json:"version"`
}

type installMetadataLastUpdate struct {
	FinishedAt  string `json:"finished_at"`
	FromVersion string `json:"from_version,omitempty"`
	Status      string `json:"status"`
	ToVersion   string `json:"to_version,omitempty"`
}

func (DefaultInstallDetector) DetectInstall(_ context.Context, currentVersion string) (Install, error) {
	path, err := os.Executable()
	if err != nil {
		return Install{}, err
	}
	path = cleanExecutablePath(path)

	if isContainerInstall() {
		return Install{Method: InstallMethodContainer, Path: path}, nil
	}
	if IsDevelopmentVersion(currentVersion) {
		return Install{Method: InstallMethodSource, Path: path}, nil
	}
	if metadata, ok := loadInstallMetadata(); ok && samePath(metadata.InstallPath, path) {
		method := InstallMethod(strings.TrimSpace(metadata.InstallMethod))
		return Install{
			Method:            method,
			Path:              path,
			SelfUpdateAllowed: method == InstallMethodReleaseTarball && metadata.SelfUpdateAllowed,
		}, nil
	}

	return Install{Method: InstallMethodUnknown, Path: path}, nil
}

func ReplaceBinary(installPath string, replacementPath string) (string, error) {
	if strings.TrimSpace(installPath) == "" {
		return "", fmt.Errorf("install path is empty")
	}
	backupPath := installPath + ".previous"
	_ = os.Remove(backupPath)

	if err := os.Rename(installPath, backupPath); err != nil {
		return "", fmt.Errorf("backup existing binary: %w", err)
	}
	if err := os.Rename(replacementPath, installPath); err != nil {
		_ = os.Rename(backupPath, installPath)
		return "", fmt.Errorf("replace binary: %w", err)
	}
	return backupPath, nil
}

func writeInstallMetadata(metadata installMetadata) error {
	path := defaultInstallMetadataPath()
	if path == "" {
		return fmt.Errorf("install metadata path is unavailable")
	}
	if metadata.Version == 0 {
		metadata.Version = 1
	}
	contents, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')

	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory, "install metadata directory"); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing install metadata symlink %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	file, err := os.CreateTemp(directory, ".install-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)

	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func cleanExecutablePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

func isContainerInstall() bool {
	if strings.TrimSpace(os.Getenv("MOLTNET_CONTAINER")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("container")) != "" {
		return true
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return false
}

func loadInstallMetadata() (installMetadata, bool) {
	path := defaultInstallMetadataPath()
	if path == "" {
		return installMetadata{}, false
	}
	if err := validatePrivateDirectory(filepath.Dir(path), "install metadata directory"); err != nil {
		return installMetadata{}, false
	}
	if err := validatePrivateRegularFile(path, "install metadata"); err != nil {
		return installMetadata{}, false
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return installMetadata{}, false
	}
	var metadata installMetadata
	if err := json.Unmarshal(contents, &metadata); err != nil {
		legacy, ok := parseLegacyInstallMetadata(contents)
		if !ok {
			return installMetadata{}, false
		}
		metadata = legacy
	}
	return metadata, true
}

func parseLegacyInstallMetadata(contents []byte) (installMetadata, bool) {
	var stringVersionLegacy struct {
		Asset             string `json:"asset"`
		Checksum          string `json:"checksum"`
		InstallMethod     string `json:"install_method"`
		InstallPath       string `json:"install_path"`
		LastUpdate        string `json:"last_update"`
		PreviousBinary    string `json:"previous_binary"`
		SchemaVersion     int    `json:"schema_version"`
		SelfUpdateAllowed bool   `json:"self_update_allowed"`
		Version           string `json:"version"`
	}
	if err := json.Unmarshal(contents, &stringVersionLegacy); err == nil &&
		(strings.TrimSpace(stringVersionLegacy.InstallPath) != "" || strings.TrimSpace(stringVersionLegacy.InstallMethod) != "") {
		metadata := installMetadata{
			AssetName:         stringVersionLegacy.Asset,
			AssetChecksum:     normalizeAssetChecksum(stringVersionLegacy.Checksum),
			InstallMethod:     stringVersionLegacy.InstallMethod,
			InstallPath:       stringVersionLegacy.InstallPath,
			InstalledVersion:  stringVersionLegacy.Version,
			PreviousBinary:    stringVersionLegacy.PreviousBinary,
			SelfUpdateAllowed: stringVersionLegacy.SelfUpdateAllowed,
			Version:           stringVersionLegacy.SchemaVersion,
		}
		if strings.TrimSpace(stringVersionLegacy.LastUpdate) != "" {
			metadata.LastUpdate = &installMetadataLastUpdate{
				FinishedAt:  stringVersionLegacy.LastUpdate,
				Status:      "succeeded",
				ToVersion:   stringVersionLegacy.Version,
				FromVersion: "",
			}
		}
		return metadata, true
	}

	var legacy struct {
		InstallMethod     string `json:"install_method"`
		InstallPath       string `json:"install_path"`
		SelfUpdateAllowed bool   `json:"self_update_allowed"`
		Version           int    `json:"version"`
	}
	if err := json.Unmarshal(contents, &legacy); err != nil {
		return installMetadata{}, false
	}
	return installMetadata{
		InstallMethod:     legacy.InstallMethod,
		InstallPath:       legacy.InstallPath,
		SelfUpdateAllowed: legacy.SelfUpdateAllowed,
		Version:           legacy.Version,
	}, true
}

func normalizeAssetChecksum(checksum string) string {
	trimmed := strings.TrimSpace(checksum)
	if trimmed == "" || strings.Contains(trimmed, ":") {
		return trimmed
	}
	return "sha256:" + trimmed
}

func validatePrivateDirectory(path string, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s %q must not be a symlink", label, path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s %q must be a directory", label, path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s %q must not be group/world accessible", label, path)
	}
	return nil
}

func ensurePrivateDirectory(path string, label string) error {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return validatePrivateDirectory(path, label)
}

func validatePrivateRegularFile(path string, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s %q must not be a symlink", label, path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s %q must be a regular file", label, path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s %q must not be group/world accessible", label, path)
	}
	return nil
}

func defaultInstallMetadataPath() string {
	home := defaultMoltnetHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "install.json")
}

func defaultMoltnetHome() string {
	if override := strings.TrimSpace(os.Getenv(moltnetHomeEnv)); override != "" {
		if absolute, err := filepath.Abs(override); err == nil {
			return absolute
		}
		return filepath.Clean(override)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".moltnet")
}

func samePath(left string, right string) bool {
	leftClean := cleanExecutablePath(left)
	rightClean := cleanExecutablePath(right)
	return leftClean == rightClean
}
