package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/service"
)

// fakeServiceRunner never touches a real launchctl/systemctl; it backs
// every cmd/moltnet `service` test via withFakeServiceManager.
type fakeServiceRunner struct{}

func (fakeServiceRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}

// withFakeServiceManager swaps newServiceManager for the duration of the
// test so `moltnet service ...` exercises its full CLI dispatch without any
// real OS service manager call.
func withFakeServiceManager(t *testing.T, goos string) {
	t.Helper()
	previous := newServiceManager
	newServiceManager = func() *service.Manager {
		return service.NewForOS(fakeServiceRunner{}, goos)
	}
	t.Cleanup(func() { newServiceManager = previous })
}

func writeServiceTestConfig(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(defaultMoltnetConfig("acme", "Acme Moltnet")), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func TestRunServiceCommandLifecycle(t *testing.T) {
	withFakeServiceManager(t, "linux")
	t.Setenv("HOME", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeServiceTestConfig(t, configPath)

	install := captureStdout(t, func() {
		if err := run(context.Background(), []string{"service", "install", "--config", configPath}, "test"); err != nil {
			t.Fatalf("service install error = %v", err)
		}
	})
	if !strings.Contains(install, "✓ service running") || !strings.Contains(install, "acme") {
		t.Fatalf("unexpected install output %q", install)
	}
	if !strings.Contains(install, "next:") || !strings.Contains(install, "relay deploy") {
		t.Fatalf("expected a next: nudge toward relay deploy, got %q", install)
	}

	status := captureStdout(t, func() {
		if err := run(context.Background(), []string{"service", "status", "--config", configPath}, "test"); err != nil {
			t.Fatalf("service status error = %v", err)
		}
	})
	if !strings.Contains(status, "running") {
		t.Fatalf("unexpected status output %q", status)
	}

	start := captureStdout(t, func() {
		if err := run(context.Background(), []string{"service", "start", "--config", configPath}, "test"); err != nil {
			t.Fatalf("service start error = %v", err)
		}
	})
	if !strings.Contains(start, "✓ service started") {
		t.Fatalf("unexpected start output %q", start)
	}

	stop := captureStdout(t, func() {
		if err := run(context.Background(), []string{"service", "stop", "--config", configPath}, "test"); err != nil {
			t.Fatalf("service stop error = %v", err)
		}
	})
	if !strings.Contains(stop, "✓ service stopped") {
		t.Fatalf("unexpected stop output %q", stop)
	}

	uninstall := captureStdout(t, func() {
		if err := run(context.Background(), []string{"service", "uninstall", "--config", configPath}, "test"); err != nil {
			t.Fatalf("service uninstall error = %v", err)
		}
	})
	if !strings.Contains(uninstall, "✓ service removed") {
		t.Fatalf("unexpected uninstall output %q", uninstall)
	}
}

func TestRunServiceCommandStatusWhenNotInstalled(t *testing.T) {
	withFakeServiceManager(t, "linux")
	t.Setenv("HOME", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeServiceTestConfig(t, configPath)

	status := captureStdout(t, func() {
		if err := run(context.Background(), []string{"service", "status", "--config", configPath}, "test"); err != nil {
			t.Fatalf("service status error = %v", err)
		}
	})
	if !strings.Contains(status, "no installed service") {
		t.Fatalf("unexpected status output %q", status)
	}
}

func TestRunServiceCommandStartBeforeInstallErrors(t *testing.T) {
	withFakeServiceManager(t, "linux")
	t.Setenv("HOME", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeServiceTestConfig(t, configPath)

	err := run(context.Background(), []string{"service", "start", "--config", configPath}, "test")
	if err == nil {
		t.Fatal("expected an error starting a never-installed service")
	}
}

func TestRunServiceCommandUnsupportedOS(t *testing.T) {
	withFakeServiceManager(t, "windows")
	t.Setenv("HOME", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "Moltnet")
	writeServiceTestConfig(t, configPath)

	err := run(context.Background(), []string{"service", "install", "--config", configPath}, "test")
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("service install error = %v, want unsupported OS error", err)
	}
}

func TestRunServiceCommandHelp(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"service", "help"}, "test"); err != nil {
			t.Fatalf("service help error = %v", err)
		}
	})
	if !strings.Contains(output, "moltnet service install") {
		t.Fatalf("unexpected help output %q", output)
	}
}

func TestRunServiceCommandUnknownSubcommand(t *testing.T) {
	err := run(context.Background(), []string{"service", "wat"}, "test")
	if err == nil {
		t.Fatal("expected an error for an unknown service subcommand")
	}
}

func TestRunServiceCommandRequiresConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	err := run(context.Background(), []string{"service", "status"}, "test")
	if err == nil {
		t.Fatal("expected an error when no config can be resolved")
	}
}
