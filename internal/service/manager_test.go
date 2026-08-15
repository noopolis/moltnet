package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner records every call and returns a scripted (output, err) per
// command name, so manager tests never touch a real launchctl/systemctl.
type fakeRunner struct {
	calls   [][]string
	outputs map[string]string
	errs    map[string]error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{outputs: map[string]string{}, errs: map[string]error{}}
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	key := strings.Join(call, " ")
	return []byte(f.outputs[key]), f.errs[key]
}

func installedSpec(t *testing.T) Spec {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return Spec{
		NetworkID:  "acme",
		ConfigPath: home + "/.moltnet/acme/Moltnet",
		BinaryPath: "/usr/local/bin/moltnet",
		NetworkDir: home + "/.moltnet/acme",
	}
}

func TestManagerInstallDarwinWritesPlistAndBootstraps(t *testing.T) {
	spec := installedSpec(t)
	runner := newFakeRunner()
	mgr := NewForOS(runner, "darwin")

	if err := mgr.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	installed, err := mgr.IsInstalled(spec.NetworkID)
	if err != nil || !installed {
		t.Fatalf("IsInstalled() = %v, %v; want true, nil", installed, err)
	}

	foundBootstrap := false
	for _, call := range runner.calls {
		if len(call) > 1 && call[0] == "launchctl" && call[1] == "bootstrap" {
			foundBootstrap = true
		}
	}
	if !foundBootstrap {
		t.Fatalf("expected a launchctl bootstrap call, got %v", runner.calls)
	}
}

func TestManagerInstallLinuxWritesUnitAndEnables(t *testing.T) {
	spec := installedSpec(t)
	runner := newFakeRunner()
	mgr := NewForOS(runner, "linux")

	if err := mgr.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	installed, err := mgr.IsInstalled(spec.NetworkID)
	if err != nil || !installed {
		t.Fatalf("IsInstalled() = %v, %v; want true, nil", installed, err)
	}

	want := []string{"systemctl --user daemon-reload", "systemctl --user enable --now moltnet-acme.service"}
	for _, expected := range want {
		found := false
		for _, call := range runner.calls {
			if strings.Join(call, " ") == expected {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected call %q, got %v", expected, runner.calls)
		}
	}
}

func TestManagerStartBeforeInstallErrors(t *testing.T) {
	spec := installedSpec(t)
	runner := newFakeRunner()
	mgr := NewForOS(runner, "linux")

	err := mgr.Start(context.Background(), spec.NetworkID)
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Start() error = %v, want ErrNotInstalled", err)
	}
}

func TestManagerRestartBeforeInstallErrors(t *testing.T) {
	spec := installedSpec(t)
	runner := newFakeRunner()
	mgr := NewForOS(runner, "darwin")

	err := mgr.Restart(context.Background(), spec.NetworkID)
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Restart() error = %v, want ErrNotInstalled", err)
	}
}

func TestManagerUninstallRemovesUnitFile(t *testing.T) {
	spec := installedSpec(t)
	runner := newFakeRunner()
	mgr := NewForOS(runner, "linux")

	if err := mgr.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if err := mgr.Uninstall(context.Background(), spec.NetworkID); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}

	installed, err := mgr.IsInstalled(spec.NetworkID)
	if err != nil {
		t.Fatalf("IsInstalled() error = %v", err)
	}
	if installed {
		t.Fatal("expected service to be uninstalled")
	}
}

func TestManagerUninstallUninstalledIsNoop(t *testing.T) {
	spec := installedSpec(t)
	runner := newFakeRunner()
	mgr := NewForOS(runner, "darwin")

	if err := mgr.Uninstall(context.Background(), spec.NetworkID); err != nil {
		t.Fatalf("Uninstall() on never-installed network error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected no launchctl calls, got %v", runner.calls)
	}
}

func TestManagerStatusReportsRunningFromDetail(t *testing.T) {
	spec := installedSpec(t)
	runner := newFakeRunner()
	mgr := NewForOS(runner, "linux")
	if err := mgr.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	runner.outputs["systemctl --user status moltnet-acme.service"] = "Active: active (running) since now"

	status, err := mgr.Status(context.Background(), spec.NetworkID)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Installed || !status.Running {
		t.Fatalf("Status() = %+v, want installed and running", status)
	}
}

func TestManagerStatusNotInstalled(t *testing.T) {
	spec := installedSpec(t)
	runner := newFakeRunner()
	mgr := NewForOS(runner, "linux")

	status, err := mgr.Status(context.Background(), spec.NetworkID)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Installed {
		t.Fatalf("Status() = %+v, want not installed", status)
	}
}

func TestManagerUnsupportedOS(t *testing.T) {
	spec := installedSpec(t)
	runner := newFakeRunner()
	mgr := NewForOS(runner, "windows")

	err := mgr.Install(context.Background(), spec)
	var unsupported ErrUnsupportedOS
	if !errors.As(err, &unsupported) {
		t.Fatalf("Install() error = %v, want ErrUnsupportedOS", err)
	}
}

func TestManagerStartStopRestartDispatchLinux(t *testing.T) {
	spec := installedSpec(t)
	runner := newFakeRunner()
	mgr := NewForOS(runner, "linux")
	if err := mgr.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if err := mgr.Start(context.Background(), spec.NetworkID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := mgr.Restart(context.Background(), spec.NetworkID); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if err := mgr.Stop(context.Background(), spec.NetworkID); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	for _, expected := range []string{
		"systemctl --user start moltnet-acme.service",
		"systemctl --user restart moltnet-acme.service",
		"systemctl --user stop moltnet-acme.service",
	} {
		found := false
		for _, call := range runner.calls {
			if strings.Join(call, " ") == expected {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected call %q, got %v", expected, runner.calls)
		}
	}
}

func TestManagerInstalledNetworkIDsDarwin(t *testing.T) {
	spec := installedSpec(t)
	mgr := NewForOS(newFakeRunner(), "darwin")
	if err := mgr.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	ids, err := mgr.InstalledNetworkIDs()
	if err != nil {
		t.Fatalf("InstalledNetworkIDs() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != spec.NetworkID {
		t.Fatalf("InstalledNetworkIDs() = %v, want [%q]", ids, spec.NetworkID)
	}
}

func TestManagerInstalledNetworkIDsLinux(t *testing.T) {
	spec := installedSpec(t)
	mgr := NewForOS(newFakeRunner(), "linux")
	if err := mgr.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	ids, err := mgr.InstalledNetworkIDs()
	if err != nil {
		t.Fatalf("InstalledNetworkIDs() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != spec.NetworkID {
		t.Fatalf("InstalledNetworkIDs() = %v, want [%q]", ids, spec.NetworkID)
	}
}

func TestManagerInstalledNetworkIDsUnsupportedOS(t *testing.T) {
	mgr := NewForOS(newFakeRunner(), "windows")
	_, err := mgr.InstalledNetworkIDs()
	var unsupported ErrUnsupportedOS
	if !errors.As(err, &unsupported) {
		t.Fatalf("InstalledNetworkIDs() error = %v, want ErrUnsupportedOS", err)
	}
}
