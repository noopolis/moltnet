package service

import (
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
