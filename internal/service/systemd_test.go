package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderSystemdUnitGolden(t *testing.T) {
	got, err := RenderSystemdUnit(testSpec(t))
	if err != nil {
		t.Fatalf("RenderSystemdUnit() error = %v", err)
	}

	want := `[Unit]
Description=Moltnet server (acme)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/moltnet start --config /home/user/.moltnet/acme/Moltnet
WorkingDirectory=/home/user/.moltnet/acme
StandardOutput=append:/home/user/.moltnet/acme/.moltnet/service.out.log
StandardError=append:/home/user/.moltnet/acme/.moltnet/service.err.log
Restart=always
RestartSec=2

[Install]
WantedBy=default.target
`
	if got != want {
		t.Fatalf("RenderSystemdUnit() =\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderSystemdUnitQuotesWhitespace(t *testing.T) {
	spec := testSpec(t)
	spec.ConfigPath = "/home/user/my moltnet/Moltnet"

	got, err := RenderSystemdUnit(spec)
	if err != nil {
		t.Fatalf("RenderSystemdUnit() error = %v", err)
	}
	want := `ExecStart=/usr/local/bin/moltnet start --config "/home/user/my moltnet/Moltnet"`
	if !containsLine(got, want) {
		t.Fatalf("expected quoted ExecStart line %q, got:\n%s", want, got)
	}
}

func TestRenderSystemdUnitRejectsRelativePaths(t *testing.T) {
	spec := testSpec(t)
	spec.NetworkDir = "acme"

	if _, err := RenderSystemdUnit(spec); err == nil {
		t.Fatal("expected error for relative network dir")
	}
}

func TestSystemdUnitPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := SystemdUnitPath("acme")
	if err != nil {
		t.Fatalf("SystemdUnitPath() error = %v", err)
	}
	want := filepath.Join(home, ".config", "systemd", "user", "moltnet-acme.service")
	if path != want {
		t.Fatalf("SystemdUnitPath() = %q, want %q", path, want)
	}
}

func TestInstalledSystemdNetworkIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	for _, name := range []string{"moltnet-beta.service", "moltnet-acme.service", "other-thing.service"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("placeholder"), 0o644); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}

	ids, err := installedSystemdNetworkIDs()
	if err != nil {
		t.Fatalf("installedSystemdNetworkIDs() error = %v", err)
	}
	want := []string{"acme", "beta"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("installedSystemdNetworkIDs() = %v, want %v", ids, want)
	}
}

func TestInstalledSystemdNetworkIDsMissingDirIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ids, err := installedSystemdNetworkIDs()
	if err != nil {
		t.Fatalf("installedSystemdNetworkIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("installedSystemdNetworkIDs() = %v, want empty", ids)
	}
}
