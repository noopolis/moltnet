package updater

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCheckReportsUpdateAvailableWithoutMutation(t *testing.T) {
	assetName := "moltnet_linux_amd64.tar.gz"
	archive := moltnetArchive(t, "v0.2.0")
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   archive,
		version:   "v0.2.0",
	})
	defer server.Close()

	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	result, err := Run(context.Background(), Options{
		CheckOnly:      true,
		CurrentVersion: "0.1.0",
		Detector: staticDetector{install: Install{
			Method:            InstallMethodReleaseTarball,
			Path:              installPath,
			SelfUpdateAllowed: true,
		}},
		Platform:      Platform{OS: "linux", Arch: "amd64"},
		ReleaseSource: HTTPReleaseSource{Client: server.Client(), DownloadBaseURL: server.URL},
	})
	if err != nil {
		t.Fatalf("Run() check error = %v", err)
	}
	if !result.UpdateAvailable || result.Updated {
		t.Fatalf("unexpected check result %#v", result)
	}
	if got := runVersion(t, installPath); got != "v0.1.0" {
		t.Fatalf("check mutated install, version = %q", got)
	}
	if server.assetRequests != 0 {
		t.Fatalf("check downloaded archive %d times", server.assetRequests)
	}
}

func TestRunDryRunDoesNotMutateInstall(t *testing.T) {
	assetName := "moltnet_linux_amd64.tar.gz"
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v0.2.0"),
		version:   "v0.2.0",
	})
	defer server.Close()

	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	options := releaseUpdateOptions(server, installPath)
	options.DryRun = true

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() dry-run error = %v", err)
	}
	if !result.UpdateAvailable || result.Updated {
		t.Fatalf("unexpected dry-run result %#v", result)
	}
	if got := runVersion(t, installPath); got != "v0.1.0" {
		t.Fatalf("dry-run mutated install, version = %q", got)
	}
	if server.assetRequests != 0 {
		t.Fatalf("dry-run downloaded archive %d times", server.assetRequests)
	}
}

func TestRunRefusesDevelopmentInstallMutation(t *testing.T) {
	assetName := "moltnet_linux_amd64.tar.gz"
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v0.2.0"),
		version:   "v0.2.0",
	})
	defer server.Close()

	installPath := writeMoltnetScript(t, t.TempDir(), "0.0.0-dev")
	result, err := Run(context.Background(), Options{
		CurrentVersion: "0.0.0-dev",
		Detector:       staticDetector{install: Install{Method: InstallMethodSource, Path: installPath}},
		Platform:       Platform{OS: "linux", Arch: "amd64"},
		ReleaseSource:  HTTPReleaseSource{Client: server.Client(), DownloadBaseURL: server.URL},
	})
	if err == nil {
		t.Fatal("expected source install mutation refusal")
	}
	if !strings.Contains(err.Error(), "source or development builds") {
		t.Fatalf("unexpected error %v", err)
	}
	if !result.MutationRefused {
		t.Fatalf("expected mutation refusal result %#v", result)
	}
	if got := runVersion(t, installPath); got != "0.0.0-dev" {
		t.Fatalf("refused update mutated install, version = %q", got)
	}
}

func TestEnsureMutationAllowedRefusesUnsupportedInstalls(t *testing.T) {
	tests := []struct {
		install Install
		want    string
	}{
		{
			install: Install{Method: InstallMethodContainer, Path: "/usr/bin/moltnet"},
			want:    "container installs",
		},
		{
			install: Install{Method: InstallMethodUnknown, Path: "/usr/bin/moltnet"},
			want:    "install method is unknown",
		},
		{
			install: Install{Method: InstallMethodReleaseTarball, Path: "/usr/bin/moltnet"},
			want:    "metadata does not allow it",
		},
	}

	for _, test := range tests {
		t.Run(string(test.install.Method), func(t *testing.T) {
			err := ensureMutationAllowed(test.install)
			if err == nil {
				t.Fatal("expected mutation refusal")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q in error %v", test.want, err)
			}
		})
	}
}

func TestCheckReportSaysUnsupportedInstallsCannotSelfUpdate(t *testing.T) {
	for _, method := range []InstallMethod{
		InstallMethodSource,
		InstallMethodContainer,
		InstallMethodUnknown,
	} {
		t.Run(string(method), func(t *testing.T) {
			result := Result{
				CheckOnly:       true,
				CurrentVersion:  "v0.1.0",
				Install:         Install{Method: method, Path: "/tmp/moltnet"},
				TargetVersion:   "v0.2.0",
				UpdateAvailable: true,
			}
			output := result.String()
			if !strings.Contains(output, "Update available, but self-update is not available for this install.") {
				t.Fatalf("expected unsupported check wording, got %q", output)
			}
		})
	}
}

func TestRunUsesExplicitTargetVersion(t *testing.T) {
	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	result, err := Run(context.Background(), Options{
		CheckOnly:      true,
		CurrentVersion: "v0.1.0",
		Detector: staticDetector{install: Install{
			Method:            InstallMethodReleaseTarball,
			Path:              installPath,
			SelfUpdateAllowed: true,
		}},
		Platform:      Platform{OS: "linux", Arch: "amd64"},
		ReleaseSource: explicitTargetReleaseSource{},
		TargetVersion: "v0.3.0",
	})
	if err != nil {
		t.Fatalf("Run() explicit target error = %v", err)
	}
	if result.TargetVersion != "v0.3.0" || result.LatestVersion != "" {
		t.Fatalf("unexpected explicit target result %#v", result)
	}
}

func TestRunErrorsOnChecksumMismatch(t *testing.T) {
	assetName := "moltnet_linux_amd64.tar.gz"
	server := newReleaseServer(t, fakeRelease{
		assetName:        assetName,
		archive:          moltnetArchive(t, "v0.2.0"),
		checksumOverride: strings.Repeat("0", 64),
		version:          "v0.2.0",
	})
	defer server.Close()

	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	_, err := Run(context.Background(), releaseUpdateOptions(server, installPath))
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if got := runVersion(t, installPath); got != "v0.1.0" {
		t.Fatalf("checksum failure mutated install, version = %q", got)
	}
}

func TestRunErrorsOnMissingAsset(t *testing.T) {
	assetName := "moltnet_linux_amd64.tar.gz"
	server := newReleaseServer(t, fakeRelease{
		assetName:    assetName,
		archive:      moltnetArchive(t, "v0.2.0"),
		missingAsset: true,
		version:      "v0.2.0",
	})
	defer server.Close()

	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	_, err := Run(context.Background(), releaseUpdateOptions(server, installPath))
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected missing asset error, got %v", err)
	}
	if got := runVersion(t, installPath); got != "v0.1.0" {
		t.Fatalf("missing asset mutated install, version = %q", got)
	}
}

func TestRunReplacesTempInstallRoot(t *testing.T) {
	t.Setenv(moltnetHomeEnv, t.TempDir())

	assetName := "moltnet_linux_amd64.tar.gz"
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v0.2.0"),
		version:   "v0.2.0",
	})
	defer server.Close()

	installPath := writeMoltnetScript(t, t.TempDir(), "v0.1.0")
	result, err := Run(context.Background(), releaseUpdateOptions(server, installPath))
	if err != nil {
		t.Fatalf("Run() update error = %v", err)
	}
	if !result.Updated || result.BackupPath == "" {
		t.Fatalf("unexpected update result %#v", result)
	}
	if got := runVersion(t, installPath); got != "v0.2.0" {
		t.Fatalf("installed version = %q", got)
	}
	if got := runVersion(t, result.BackupPath); got != "v0.1.0" {
		t.Fatalf("backup version = %q", got)
	}
}

type staticDetector struct {
	install Install
}

func (d staticDetector) DetectInstall(context.Context, string) (Install, error) {
	return d.install, nil
}

type explicitTargetReleaseSource struct{}

func (explicitTargetReleaseSource) LatestVersion(context.Context) (string, error) {
	return "", fmt.Errorf("latest version should not be requested")
}

func (explicitTargetReleaseSource) Archive(context.Context, string, string) ([]byte, error) {
	return nil, fmt.Errorf("archive should not be requested")
}

func (explicitTargetReleaseSource) Checksums(context.Context, string) ([]byte, error) {
	return []byte(""), nil
}

type fakeRelease struct {
	archive          []byte
	assetName        string
	checksumOverride string
	missingAsset     bool
	version          string
}

type fakeReleaseServer struct {
	*httptest.Server
	assetRequests int
}

func newReleaseServer(t *testing.T, release fakeRelease) *fakeReleaseServer {
	t.Helper()

	fake := &fakeReleaseServer{}
	handler := http.NewServeMux()
	handler.HandleFunc("/release.json", func(response http.ResponseWriter, request *http.Request) {
		fmt.Fprintf(response, `{"version":%q}`, release.version)
	})
	handler.HandleFunc("/checksums.txt", func(response http.ResponseWriter, request *http.Request) {
		sum := sha256.Sum256(release.archive)
		checksum := fmt.Sprintf("%x", sum[:])
		if release.checksumOverride != "" {
			checksum = release.checksumOverride
		}
		fmt.Fprintf(response, "%s  %s\n", checksum, release.assetName)
	})
	handler.HandleFunc("/"+release.assetName, func(response http.ResponseWriter, request *http.Request) {
		fake.assetRequests++
		if release.missingAsset {
			http.NotFound(response, request)
			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(release.archive)
	})
	fake.Server = httptest.NewServer(handler)
	return fake
}

func releaseUpdateOptions(server *fakeReleaseServer, installPath string) Options {
	return Options{
		CurrentVersion: "v0.1.0",
		Detector: staticDetector{install: Install{
			Method:            InstallMethodReleaseTarball,
			Path:              installPath,
			SelfUpdateAllowed: true,
		}},
		LockPath:      installPath + ".test.update.lock",
		Platform:      Platform{OS: "linux", Arch: "amd64"},
		ReleaseSource: HTTPReleaseSource{Client: server.Client(), DownloadBaseURL: server.URL},
	}
}

func moltnetArchive(t *testing.T, version string) []byte {
	t.Helper()

	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then echo %s; exit 0; fi\n", version)
	return archiveWithEntries(t, archiveEntry{name: "moltnet", body: script, mode: 0o755})
}

func writeMoltnetScript(t *testing.T, directory string, version string) string {
	t.Helper()

	path := filepath.Join(directory, "moltnet")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then echo %s; exit 0; fi\n", version)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write moltnet script: %v", err)
	}
	return path
}

func runVersion(t *testing.T, path string) string {
	t.Helper()

	command := exec.Command(path, "version")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("run %s version: %v", path, err)
	}
	return strings.TrimSpace(string(output))
}
