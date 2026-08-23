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
	if err := os.WriteFile(path, []byte(defaultMoltnetConfig("acme", "Acme Moltnet", "test-operator-token")), 0o600); err != nil {
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

// TestServiceInstallNextStepsGoldenThreeIntents is P2-2's coverage fix: no
// test previously referenced runServiceInstall or
// printServiceInstallNextSteps at all, so PLAN.md 7A.4's three-intent fork
// (local agents only / open up via relay / join an existing network) was
// entirely unpinned -- a regression collapsing it back to a single generic
// nudge, dropping a line, or reordering them would have passed every test
// in the repo. This pins the exact three lines, in order, against a real
// config on disk.
func TestServiceInstallNextStepsGoldenThreeIntents(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "Moltnet")
	writeServiceTestConfig(t, configPath)

	output := captureStdout(t, func() {
		if err := printServiceInstallNextSteps(service.Spec{NetworkID: "acme", ConfigPath: configPath}); err != nil {
			t.Fatalf("printServiceInstallNextSteps() error = %v", err)
		}
	})

	wantLines := []string{
		"http://127.0.0.1:8787/install.md",
		"local agents only: point them here to join",
		"moltnet relay deploy --id acme",
		"open it up: relay on Cloudflare (pair across NAT)",
		"moltnet pair '<invite-code>'",
		"join a network someone already invited you to",
	}
	for _, want := range wantLines {
		if !strings.Contains(output, want) {
			t.Fatalf("expected next-steps output to contain %q, got %q", want, output)
		}
	}
	if strings.Count(output, "next:") != 3 {
		t.Fatalf("expected exactly 3 next: lines (the three-intent fork), got %d in %q", strings.Count(output, "next:"), output)
	}

	// Round 2's P3 fix: the doc comment above pins these "in order," but
	// nothing here actually asserted an order -- a regression reordering the
	// three intents (e.g. swapping "open it up" and "join a network") passed
	// every check above unchanged, since strings.Contains and a bare line
	// count are both order-blind. Comparing successive strings.Index results
	// closes that: each wanted string's index must strictly increase.
	lastIndex := -1
	for _, want := range wantLines {
		index := strings.Index(output, want)
		if index <= lastIndex {
			t.Fatalf("expected %q to appear after the preceding line in order, got output %q", want, output)
		}
		lastIndex = index
	}
}

// TestServiceInstallNextStepsFallsBackWhenConfigUnreadable is P2-3's fix:
// `manager.Install` (runServiceInstall's caller) has already installed and
// started the service by the time printServiceInstallNextSteps runs -- the
// config reload here only picks a real base URL for the /install.md line,
// a cosmetic nicety, not remaining required work. An unreadable config used
// to turn that into a returned error, which runServiceInstall propagates as
// a non-zero exit from a command that already succeeded at the one thing it
// promised to do. This must instead fall back to the old generic
// "relay deploy" line and report no error at all.
func TestServiceInstallNextStepsFallsBackWhenConfigUnreadable(t *testing.T) {
	spec := service.Spec{
		NetworkID:  "acme",
		ConfigPath: filepath.Join(t.TempDir(), "does-not-exist", "Moltnet"),
	}

	output := captureStdout(t, func() {
		if err := printServiceInstallNextSteps(spec); err != nil {
			t.Fatalf("printServiceInstallNextSteps() error = %v, want nil (fallback, not failure)", err)
		}
	})

	if !strings.Contains(output, "moltnet relay deploy --id acme") {
		t.Fatalf("expected the generic relay deploy fallback line, got %q", output)
	}
	if strings.Contains(output, "/install.md") || strings.Contains(output, "invite-code") {
		t.Fatalf("expected only the generic fallback line when the config can't load, got %q", output)
	}
	if strings.Count(output, "next:") != 1 {
		t.Fatalf("expected exactly 1 next: line on the fallback path, got %d in %q", strings.Count(output, "next:"), output)
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
