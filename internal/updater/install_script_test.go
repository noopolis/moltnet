package updater

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstallScriptWritesMoltnetHomeMetadata(t *testing.T) {
	assetName, err := AssetName(Platform{OS: runtime.GOOS, Arch: runtime.GOARCH})
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	archive := archiveWithEntries(t, archiveEntry{
		name: "moltnet",
		body: "#!/bin/sh\nif [ \"$1\" = \"version\" ]; then echo v9.9.9; exit 0; fi\n",
		mode: 0o755,
	})
	sum := sha256.Sum256(archive)
	checksum := fmt.Sprintf("%x", sum[:])

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/" + assetName:
			_, _ = response.Write(archive)
		case "/checksums.txt":
			fmt.Fprintf(response, "%s  %s\n", checksum, assetName)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	installDir := t.TempDir()
	moltnetHome := missingMoltnetHome(t)
	command := exec.Command("sh", "../../website/public/install.sh")
	command.Env = append(os.Environ(),
		"MOLTNET_DOWNLOAD_BASE_URL="+server.URL,
		"MOLTNET_HOME="+moltnetHome,
		"MOLTNET_INSTALL_DIR="+installDir,
		"MOLTNET_REPO=example/moltnet",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install script failed: %v\n%s", err, output)
	}

	installedPath := filepath.Join(installDir, "moltnet")
	if got := runVersion(t, installedPath); got != "v9.9.9" {
		t.Fatalf("installed version = %q", got)
	}

	metadataPath := filepath.Join(moltnetHome, "install.json")
	contents, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read install metadata: %v\nscript output:\n%s", err, output)
	}
	var metadata installMetadata
	if err := json.Unmarshal(contents, &metadata); err != nil {
		t.Fatalf("decode install metadata: %v", err)
	}
	if !samePath(metadata.InstallPath, installedPath) ||
		metadata.InstallMethod != string(InstallMethodReleaseTarball) ||
		!metadata.SelfUpdateAllowed ||
		metadata.OwnerRepo != "example/moltnet" ||
		metadata.AssetName != assetName ||
		metadata.AssetChecksum != "sha256:"+checksum ||
		metadata.InstalledVersion != "v9.9.9" ||
		metadata.InstalledBy != "install.sh" ||
		metadata.Version != 1 {
		t.Fatalf("unexpected metadata %#v", metadata)
	}
	info, err := os.Stat(metadataPath)
	if err != nil {
		t.Fatalf("stat install metadata: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("metadata permissions = %o, want 0600", got)
	}
}

func TestInstallScriptDefaultsMetadataUnderHomeMoltnet(t *testing.T) {
	assetName, err := AssetName(Platform{OS: runtime.GOOS, Arch: runtime.GOARCH})
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v1.2.3"),
		version:   "v1.2.3",
	})
	defer server.Close()

	home := t.TempDir()
	installDir := t.TempDir()
	command := exec.Command("sh", "../../website/public/install.sh")
	command.Env = withoutEnv(os.Environ(), "MOLTNET_HOME")
	command.Env = append(command.Env,
		"HOME="+home,
		"MOLTNET_DOWNLOAD_BASE_URL="+server.URL,
		"MOLTNET_INSTALL_DIR="+installDir,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install script failed: %v\n%s", err, output)
	}

	metadataPath := filepath.Join(home, ".moltnet", "install.json")
	if _, err := os.Stat(metadataPath); err != nil {
		t.Fatalf("expected default metadata path: %v\n%s", err, output)
	}
	assertPrivateMode(t, filepath.Join(home, ".moltnet"), 0o700)
	assertPrivateMode(t, metadataPath, 0o600)
}

func TestInstallScriptRefusesInsecureExistingMoltnetHomeWithoutChmod(t *testing.T) {
	assetName, err := AssetName(Platform{OS: runtime.GOOS, Arch: runtime.GOARCH})
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	server := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetArchive(t, "v1.2.3"),
		version:   "v1.2.3",
	})
	defer server.Close()

	moltnetHome := t.TempDir()
	if err := os.Chmod(moltnetHome, 0o755); err != nil {
		t.Fatalf("make Moltnet home insecure: %v", err)
	}
	defer os.Chmod(moltnetHome, 0o700)

	command := exec.Command("sh", "../../website/public/install.sh")
	command.Env = append(os.Environ(),
		"MOLTNET_DOWNLOAD_BASE_URL="+server.URL,
		"MOLTNET_HOME="+moltnetHome,
		"MOLTNET_INSTALL_DIR="+t.TempDir(),
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("expected install script to refuse insecure Moltnet home\n%s", output)
	}
	info, statErr := os.Stat(moltnetHome)
	if statErr != nil {
		t.Fatalf("stat Moltnet home: %v", statErr)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("install script changed existing Moltnet home mode to %o", got)
	}
}

func TestInstallScriptMetadataAllowsInstalledMoltnetSelfUpdate(t *testing.T) {
	if isContainerInstall() {
		t.Skip("container detection refuses self-update")
	}
	assetName, err := AssetName(Platform{OS: runtime.GOOS, Arch: runtime.GOARCH})
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}

	oldServer := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetBinaryArchive(t, buildMoltnetCLI(t, "v0.1.0")),
		version:   "v0.1.0",
	})
	defer oldServer.Close()

	installDir := t.TempDir()
	moltnetHome := missingMoltnetHome(t)
	install := exec.Command("sh", "../../website/public/install.sh")
	install.Env = append(os.Environ(),
		"MOLTNET_DOWNLOAD_BASE_URL="+oldServer.URL,
		"MOLTNET_HOME="+moltnetHome,
		"MOLTNET_INSTALL_DIR="+installDir,
	)
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install script failed: %v\n%s", err, output)
	}

	newServer := newReleaseServer(t, fakeRelease{
		assetName: assetName,
		archive:   moltnetBinaryArchive(t, buildMoltnetCLI(t, "v0.2.0")),
		version:   "v0.2.0",
	})
	defer newServer.Close()

	workspace := t.TempDir()
	sentinels := writeRuntimeSentinels(t, workspace)
	installedPath := filepath.Join(installDir, "moltnet")
	update := exec.Command(installedPath, "update", "--yes")
	update.Dir = workspace
	update.Env = append(os.Environ(),
		"MOLTNET_DOWNLOAD_BASE_URL="+newServer.URL,
		"MOLTNET_HOME="+moltnetHome,
	)
	if output, err := update.CombinedOutput(); err != nil {
		t.Fatalf("installed moltnet update failed: %v\n%s", err, output)
	}
	if got := runVersion(t, installedPath); got != "v0.2.0" {
		t.Fatalf("installed version after update = %q", got)
	}
	assertSentinelsUnchanged(t, sentinels)

	metadata := readInstallMetadataForTest(t, filepath.Join(moltnetHome, "install.json"))
	if metadata.LastUpdate == nil ||
		metadata.LastUpdate.FromVersion != "v0.1.0" ||
		metadata.LastUpdate.ToVersion != "v0.2.0" ||
		metadata.DownloadBaseURL != newServer.URL {
		t.Fatalf("unexpected updated metadata %#v", metadata)
	}
}

func buildMoltnetCLI(t *testing.T, version string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "moltnet")
	command := exec.Command("go", "build", "-ldflags", "-X main.version="+version, "-o", path, "../../cmd/moltnet")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build moltnet %s: %v\n%s", version, err, output)
	}
	return path
}

func moltnetBinaryArchive(t *testing.T, binaryPath string) []byte {
	t.Helper()

	contents, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read built moltnet binary: %v", err)
	}
	return archiveWithEntries(t, archiveEntry{name: "moltnet", body: string(contents), mode: 0o755})
}

func readInstallMetadataForTest(t *testing.T, path string) installMetadata {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read install metadata: %v", err)
	}
	var metadata installMetadata
	if err := json.Unmarshal(contents, &metadata); err != nil {
		t.Fatalf("decode install metadata: %v", err)
	}
	return metadata
}

func writeRuntimeSentinels(t *testing.T, workspace string) map[string][]byte {
	t.Helper()

	files := map[string]string{
		"Moltnet":                "network_id: sentinel\n",
		"MoltnetNode":            "attachments: []\n",
		".moltnet/config.json":   `{"sentinel":"config"}` + "\n",
		".moltnet/moltnet.db":    "sqlite sentinel\n",
		".moltnet/agent.token":   "token sentinel\n",
		".moltnet/sessions.json": `{"sentinel":"sessions"}` + "\n",
	}
	snapshots := make(map[string][]byte, len(files))
	for name, contents := range files {
		path := filepath.Join(workspace, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create sentinel dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write sentinel %s: %v", name, err)
		}
		snapshots[path] = []byte(contents)
	}
	return snapshots
}

func assertSentinelsUnchanged(t *testing.T, snapshots map[string][]byte) {
	t.Helper()

	for path, want := range snapshots {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read sentinel %s: %v", path, err)
		}
		if string(got) != string(want) {
			t.Fatalf("sentinel %s changed: got %q want %q", path, got, want)
		}
	}
}

func assertPrivateMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

func missingMoltnetHome(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "moltnet-home")
}

func withoutEnv(env []string, names ...string) []string {
	blocked := map[string]struct{}{}
	for _, name := range names {
		blocked[name+"="] = struct{}{}
	}
	var filtered []string
	for _, item := range env {
		blockedItem := false
		for prefix := range blocked {
			if len(item) >= len(prefix) && item[:len(prefix)] == prefix {
				blockedItem = true
				break
			}
		}
		if !blockedItem {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
