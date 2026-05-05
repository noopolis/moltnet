package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type DefaultInstallDetector struct{}

type installMetadata struct {
	InstallMethod     string `json:"install_method"`
	InstallPath       string `json:"install_path"`
	SelfUpdateAllowed bool   `json:"self_update_allowed"`
	Version           int    `json:"version"`
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
	contents, err := os.ReadFile(path)
	if err != nil {
		return installMetadata{}, false
	}
	var metadata installMetadata
	if err := json.Unmarshal(contents, &metadata); err != nil {
		return installMetadata{}, false
	}
	return metadata, true
}

func defaultInstallMetadataPath() string {
	if stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); stateHome != "" {
		return filepath.Join(stateHome, "moltnet", "install.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "moltnet", "install.json")
	}
	return filepath.Join(home, ".local", "state", "moltnet", "install.json")
}

func samePath(left string, right string) bool {
	leftClean := cleanExecutablePath(left)
	rightClean := cleanExecutablePath(right)
	return leftClean == rightClean
}
