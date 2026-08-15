package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testSpec(t *testing.T) Spec {
	t.Helper()
	return Spec{
		NetworkID:  "acme",
		ConfigPath: "/home/user/.moltnet/acme/Moltnet",
		BinaryPath: "/usr/local/bin/moltnet",
		NetworkDir: "/home/user/.moltnet/acme",
	}
}

func TestRenderLaunchdPlistGolden(t *testing.T) {
	got, err := RenderLaunchdPlist(testSpec(t))
	if err != nil {
		t.Fatalf("RenderLaunchdPlist() error = %v", err)
	}

	want := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>dev.moltnet.acme</string>
	<key>ProgramArguments</key>
	<array>
		<string>/usr/local/bin/moltnet</string>
		<string>start</string>
		<string>--config</string>
		<string>/home/user/.moltnet/acme/Moltnet</string>
	</array>
	<key>WorkingDirectory</key>
	<string>/home/user/.moltnet/acme</string>
	<key>StandardOutPath</key>
	<string>/home/user/.moltnet/acme/.moltnet/service.out.log</string>
	<key>StandardErrorPath</key>
	<string>/home/user/.moltnet/acme/.moltnet/service.err.log</string>
	<key>KeepAlive</key>
	<true/>
	<key>RunAtLoad</key>
	<true/>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
`
	if got != want {
		t.Fatalf("RenderLaunchdPlist() =\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderLaunchdPlistEscapesValues(t *testing.T) {
	spec := testSpec(t)
	spec.ConfigPath = "/home/user & friend/.moltnet/acme/Moltnet"

	got, err := RenderLaunchdPlist(spec)
	if err != nil {
		t.Fatalf("RenderLaunchdPlist() error = %v", err)
	}
	if !containsLine(got, "\t\t<string>/home/user &amp; friend/.moltnet/acme/Moltnet</string>") {
		t.Fatalf("expected escaped ampersand in rendered plist, got:\n%s", got)
	}
}

func TestRenderLaunchdPlistRejectsRelativePaths(t *testing.T) {
	spec := testSpec(t)
	spec.BinaryPath = "moltnet"

	if _, err := RenderLaunchdPlist(spec); err == nil {
		t.Fatal("expected error for relative binary path")
	}
}

func TestLaunchdPlistPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := LaunchdPlistPath("acme")
	if err != nil {
		t.Fatalf("LaunchdPlistPath() error = %v", err)
	}
	want := filepath.Join(home, "Library", "LaunchAgents", "dev.moltnet.acme.plist")
	if path != want {
		t.Fatalf("LaunchdPlistPath() = %q, want %q", path, want)
	}
}

func TestInstalledLaunchdNetworkIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	for _, name := range []string{"dev.moltnet.beta.plist", "dev.moltnet.acme.plist", "com.other.thing.plist"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("placeholder"), 0o644); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}

	ids, err := installedLaunchdNetworkIDs()
	if err != nil {
		t.Fatalf("installedLaunchdNetworkIDs() error = %v", err)
	}
	want := []string{"acme", "beta"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("installedLaunchdNetworkIDs() = %v, want %v", ids, want)
	}
}

func TestInstalledLaunchdNetworkIDsMissingDirIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ids, err := installedLaunchdNetworkIDs()
	if err != nil {
		t.Fatalf("installedLaunchdNetworkIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("installedLaunchdNetworkIDs() = %v, want empty", ids)
	}
}

func containsLine(haystack, line string) bool {
	for _, candidate := range strings.Split(haystack, "\n") {
		if candidate == line {
			return true
		}
	}
	return false
}
