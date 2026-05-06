package updater

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWritesInstallMetadataAfterUpdate(t *testing.T) {
	moltnetHome := missingMoltnetHome(t)
	t.Setenv(moltnetHomeEnv, moltnetHome)

	assetName := "moltnet_linux_amd64.tar.gz"
	archive := moltnetArchive(t, "v0.2.0")
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   archive,
		version:   "v0.2.0",
	})
	defer server.Close()

	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	options := releaseUpdateOptions(server, installPath)
	options.ReleaseSource = HTTPReleaseSource{Client: server.Client(), DownloadBaseURL: server.URL, OwnerRepo: "example/moltnet"}
	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() update error = %v", err)
	}
	if !result.Updated {
		t.Fatalf("expected update result %#v", result)
	}

	metadataPath := filepath.Join(moltnetHome, "install.json")
	contents, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read install metadata: %v", err)
	}
	var metadata struct {
		AssetChecksum    string `json:"asset_checksum"`
		AssetName        string `json:"asset_name"`
		DownloadBaseURL  string `json:"download_base_url"`
		InstallMethod    string `json:"install_method"`
		InstallPath      string `json:"install_path"`
		InstalledAt      string `json:"installed_at"`
		InstalledBy      string `json:"installed_by"`
		InstalledVersion string `json:"installed_version"`
		LastUpdate       struct {
			FinishedAt  string `json:"finished_at"`
			FromVersion string `json:"from_version"`
			Status      string `json:"status"`
			ToVersion   string `json:"to_version"`
		} `json:"last_update"`
		OwnerRepo         string `json:"owner_repo"`
		PreviousBinary    string `json:"previous_binary"`
		SelfUpdateAllowed bool   `json:"self_update_allowed"`
		Version           int    `json:"version"`
	}
	if err := json.Unmarshal(contents, &metadata); err != nil {
		t.Fatalf("decode install metadata: %v", err)
	}
	sum := sha256.Sum256(archive)
	wantChecksum := "sha256:" + fmt.Sprintf("%x", sum[:])
	if metadata.InstallPath != installPath ||
		metadata.InstallMethod != string(InstallMethodReleaseTarball) ||
		!metadata.SelfUpdateAllowed ||
		metadata.AssetName != assetName ||
		metadata.AssetChecksum != wantChecksum ||
		metadata.DownloadBaseURL != server.URL ||
		metadata.InstalledVersion != "v0.2.0" ||
		metadata.OwnerRepo != "example/moltnet" ||
		metadata.PreviousBinary != result.BackupPath ||
		metadata.Version != 1 ||
		metadata.InstalledBy != "moltnet update" ||
		metadata.LastUpdate.Status != "succeeded" ||
		metadata.LastUpdate.FromVersion != "v0.1.0" ||
		metadata.LastUpdate.ToVersion != "v0.2.0" {
		t.Fatalf("unexpected metadata %#v", metadata)
	}
	if _, err := time.Parse(time.RFC3339, metadata.InstalledAt); err != nil {
		t.Fatalf("installed_at is not RFC3339: %q", metadata.InstalledAt)
	}
	if _, err := time.Parse(time.RFC3339, metadata.LastUpdate.FinishedAt); err != nil {
		t.Fatalf("last_update.finished_at is not RFC3339: %q", metadata.LastUpdate.FinishedAt)
	}
	info, err := os.Stat(metadataPath)
	if err != nil {
		t.Fatalf("stat install metadata: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("metadata permissions = %o, want 0600", got)
	}
}

func TestRunTreatsInstallMetadataFailureAsPostUpdateWarning(t *testing.T) {
	moltnetHome := missingMoltnetHome(t)
	t.Setenv(moltnetHomeEnv, moltnetHome)

	assetName := "moltnet_linux_amd64.tar.gz"
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v0.2.0"),
		version:   "v0.2.0",
	})
	defer server.Close()

	metadataDir := moltnetHome
	if err := os.MkdirAll(metadataDir, 0o700); err != nil {
		t.Fatalf("create metadata dir: %v", err)
	}
	target := filepath.Join(t.TempDir(), "install.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(metadataDir, "install.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	result, err := Run(context.Background(), releaseUpdateOptions(server, installPath))
	if err != nil {
		t.Fatalf("Run() update error = %v", err)
	}
	if !result.Updated {
		t.Fatalf("expected update despite metadata warning %#v", result)
	}
	if got := runVersion(t, installPath); got != "v0.2.0" {
		t.Fatalf("installed version = %q", got)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[len(result.Warnings)-1], "install metadata update failed") {
		t.Fatalf("expected metadata warning, got %#v", result.Warnings)
	}
}

func TestDefaultInstallDetectorRefusesDevelopmentVersionDespiteStaleMetadata(t *testing.T) {
	if isContainerInstall() {
		t.Skip("container detection takes precedence over local install metadata")
	}
	moltnetHome := missingMoltnetHome(t)
	t.Setenv(moltnetHomeEnv, moltnetHome)

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable(): %v", err)
	}
	executable = cleanExecutablePath(executable)
	if err := writeInstallMetadata(installMetadata{
		InstallMethod:     string(InstallMethodReleaseTarball),
		InstallPath:       executable,
		SelfUpdateAllowed: true,
		Version:           1,
	}); err != nil {
		t.Fatalf("write install metadata: %v", err)
	}

	install, err := (DefaultInstallDetector{}).DetectInstall(context.Background(), "0.0.0-dev+local")
	if err != nil {
		t.Fatalf("DetectInstall() error = %v", err)
	}
	if install.Method != InstallMethodSource || install.SelfUpdateAllowed {
		t.Fatalf("expected development version to remain source install, got %#v", install)
	}
}

func TestRunRechecksInstalledBinaryBeforeNoop(t *testing.T) {
	moltnetHome := missingMoltnetHome(t)
	t.Setenv(moltnetHomeEnv, moltnetHome)

	assetName := "moltnet_linux_amd64.tar.gz"
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v0.2.0"),
		version:   "v0.2.0",
	})
	defer server.Close()

	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	options := releaseUpdateOptions(server, installPath)
	options.CurrentVersion = "v0.2.0"

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() update error = %v", err)
	}
	if !result.Updated || result.CurrentVersion != "v0.1.0" {
		t.Fatalf("expected rechecked install version to drive update, got %#v", result)
	}

	contents, err := os.ReadFile(filepath.Join(moltnetHome, "install.json"))
	if err != nil {
		t.Fatalf("read install metadata: %v", err)
	}
	var metadata struct {
		LastUpdate struct {
			FromVersion string `json:"from_version"`
		} `json:"last_update"`
	}
	if err := json.Unmarshal(contents, &metadata); err != nil {
		t.Fatalf("decode install metadata: %v", err)
	}
	if metadata.LastUpdate.FromVersion != "v0.1.0" {
		t.Fatalf("metadata from_version = %q, want rechecked version", metadata.LastUpdate.FromVersion)
	}
}

func TestRunRefusesConcurrentUpdateLock(t *testing.T) {
	moltnetHome := missingMoltnetHome(t)
	t.Setenv(moltnetHomeEnv, moltnetHome)
	if err := os.MkdirAll(moltnetHome, 0o700); err != nil {
		t.Fatalf("create Moltnet home: %v", err)
	}

	assetName := "moltnet_linux_amd64.tar.gz"
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v0.2.0"),
		version:   "v0.2.0",
	})
	defer server.Close()

	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	options := releaseUpdateOptions(server, installPath)
	if err := os.WriteFile(options.LockPath, []byte("active"), 0o600); err != nil {
		t.Fatalf("write active lock: %v", err)
	}

	result, err := Run(context.Background(), options)
	if err == nil {
		t.Fatal("expected active lock error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("unexpected lock error %v", err)
	}
	if result.Updated {
		t.Fatalf("lock refusal updated install %#v", result)
	}
	if got := runVersion(t, installPath); got != "v0.1.0" {
		t.Fatalf("lock refusal mutated install, version = %q", got)
	}
}

func TestRunUsesMoltnetHomeUpdateLockByDefault(t *testing.T) {
	moltnetHome := missingMoltnetHome(t)
	t.Setenv(moltnetHomeEnv, moltnetHome)
	if err := os.MkdirAll(moltnetHome, 0o700); err != nil {
		t.Fatalf("create Moltnet home: %v", err)
	}

	assetName := "moltnet_linux_amd64.tar.gz"
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v0.2.0"),
		version:   "v0.2.0",
	})
	defer server.Close()

	lockPath := filepath.Join(moltnetHome, "update.lock")
	if err := os.WriteFile(lockPath, []byte("active"), 0o600); err != nil {
		t.Fatalf("write active default lock: %v", err)
	}
	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	options := releaseUpdateOptions(server, installPath)
	options.LockPath = ""

	result, err := Run(context.Background(), options)
	if err == nil {
		t.Fatal("expected default lock refusal")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("unexpected lock error %v", err)
	}
	if result.Updated {
		t.Fatalf("lock refusal updated install %#v", result)
	}
	if _, err := os.Stat(installPath + ".update.lock"); !os.IsNotExist(err) {
		t.Fatalf("install-local lock was used, stat err=%v", err)
	}
}

func TestDefaultInstallDetectorIgnoresInsecureMetadata(t *testing.T) {
	if isContainerInstall() {
		t.Skip("container detection takes precedence over local install metadata")
	}
	moltnetHome := missingMoltnetHome(t)
	t.Setenv(moltnetHomeEnv, moltnetHome)
	if err := os.MkdirAll(moltnetHome, 0o700); err != nil {
		t.Fatalf("create Moltnet home: %v", err)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable(): %v", err)
	}
	metadata := installMetadata{
		InstallMethod:     string(InstallMethodReleaseTarball),
		InstallPath:       cleanExecutablePath(executable),
		SelfUpdateAllowed: true,
		Version:           1,
	}
	contents, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	metadataPath := filepath.Join(moltnetHome, "install.json")
	if err := os.WriteFile(metadataPath, append(contents, '\n'), 0o644); err != nil {
		t.Fatalf("write insecure metadata: %v", err)
	}

	install, err := (DefaultInstallDetector{}).DetectInstall(context.Background(), "v0.1.0")
	if err != nil {
		t.Fatalf("DetectInstall() error = %v", err)
	}
	if install.Method != InstallMethodUnknown || install.SelfUpdateAllowed {
		t.Fatalf("expected insecure metadata to be ignored, got %#v", install)
	}
}

func TestWriteInstallMetadataRefusesInsecureExistingHomeWithoutChmod(t *testing.T) {
	moltnetHome := t.TempDir()
	t.Setenv(moltnetHomeEnv, moltnetHome)
	if err := os.Chmod(moltnetHome, 0o755); err != nil {
		t.Fatalf("make home insecure: %v", err)
	}
	defer os.Chmod(moltnetHome, 0o700)

	err := writeInstallMetadata(installMetadata{
		InstallMethod:     string(InstallMethodReleaseTarball),
		InstallPath:       "/tmp/moltnet",
		SelfUpdateAllowed: true,
		Version:           1,
	})
	if err == nil || !strings.Contains(err.Error(), "group/world accessible") {
		t.Fatalf("expected insecure home refusal, got %v", err)
	}
	info, statErr := os.Stat(moltnetHome)
	if statErr != nil {
		t.Fatalf("stat moltnet home: %v", statErr)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("writeInstallMetadata changed existing home mode to %o", got)
	}
}

func TestRunCheckWarnsOnServerAuthFailureAndContinues(t *testing.T) {
	assetName := "moltnet_linux_amd64.tar.gz"
	releaseServer := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v0.2.0"),
		version:   "v0.2.0",
	})
	defer releaseServer.Close()

	probeServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer correct-token" {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(response, `{"version":"v0.2.0"}`)
	}))
	defer probeServer.Close()

	t.Setenv(DefaultServerTokenEnv, "wrong-token")
	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	options := releaseUpdateOptions(releaseServer, installPath)
	options.CheckOnly = true
	options.ServerURL = probeServer.URL
	options.ServerTokenEnv = DefaultServerTokenEnv

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() check error = %v", err)
	}
	if !result.UpdateAvailable || !result.ChecksumAvailable {
		t.Fatalf("binary checks did not continue after server warning %#v", result)
	}
	if !strings.Contains(result.Server.Warning, "401") {
		t.Fatalf("expected auth warning, got %#v", result.Server)
	}
}

func TestRunCheckDoesNotSendAmbientServerToken(t *testing.T) {
	assetName := "moltnet_linux_amd64.tar.gz"
	releaseServer := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v0.2.0"),
		version:   "v0.2.0",
	})
	defer releaseServer.Close()

	var authHeader string
	probeServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authHeader = request.Header.Get("Authorization")
		fmt.Fprint(response, `{"version":"v0.1.0"}`)
	}))
	defer probeServer.Close()

	t.Setenv(DefaultServerTokenEnv, "ambient-secret")
	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	options := releaseUpdateOptions(releaseServer, installPath)
	options.CheckOnly = true
	options.ServerURL = probeServer.URL

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() check error = %v", err)
	}
	if authHeader != "" {
		t.Fatalf("unexpected ambient Authorization header %q", authHeader)
	}
	if result.Server.Warning != "" {
		t.Fatalf("unexpected server warning %#v", result.Server)
	}
}

func TestRunCheckWarnsOnMissingServerTokenEnvAndContinues(t *testing.T) {
	assetName := "moltnet_linux_amd64.tar.gz"
	releaseServer := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v0.2.0"),
		version:   "v0.2.0",
	})
	defer releaseServer.Close()

	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	options := releaseUpdateOptions(releaseServer, installPath)
	options.CheckOnly = true
	options.ServerTokenEnv = "MISSING_MOLTNET_UPDATE_TOKEN"
	options.ServerURL = "http://127.0.0.1:1"

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() check error = %v", err)
	}
	if !result.UpdateAvailable || !result.ChecksumAvailable {
		t.Fatalf("binary checks did not continue after missing token warning %#v", result)
	}
	if !strings.Contains(result.Server.Warning, "MISSING_MOLTNET_UPDATE_TOKEN") {
		t.Fatalf("expected missing token warning, got %#v", result.Server)
	}
}
