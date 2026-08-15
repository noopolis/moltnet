package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Runner executes one external system command (launchctl or systemctl) and
// returns its combined output. Production code uses execRunner; tests
// inject a fake so they never touch the real service manager.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return output, nil
}

// Status is the result of `moltnet service status`.
type Status struct {
	Installed bool
	Running   bool
	// Detail is the raw platform tool output (launchctl print / systemctl
	// status), included for operators debugging a stuck service.
	Detail string
}

// Manager installs, uninstalls, and controls the OS service for a Moltnet
// network. Use New for production code; NewForOS is the test seam that
// lets tests exercise the darwin and linux code paths regardless of the
// host they run on, without ever calling a real launchctl/systemctl.
type Manager struct {
	runner Runner
	goos   string
}

// New returns a Manager that dispatches on the real runtime.GOOS and runs
// real launchctl/systemctl commands.
func New() *Manager {
	return NewForOS(execRunner{}, runtime.GOOS)
}

// NewForOS returns a Manager for goos ("darwin" or anything else, treated
// as linux-family for systemd), using runner instead of exec.Command. Tests
// use this to golden-test both platforms' lifecycle behavior from one host.
func NewForOS(runner Runner, goos string) *Manager {
	return &Manager{runner: runner, goos: goos}
}

// ErrNotInstalled is returned by Start, Stop, Restart, and Uninstall when
// no unit/plist has been installed for the network yet.
var ErrNotInstalled = errors.New("moltnet service is not installed for this network")

// Install renders and writes the platform unit file for spec, then loads
// (and, per KeepAlive/enable --now, starts) it. Re-running Install updates
// the file in place and reloads it, so editing a network's binary or config
// path just means re-running `moltnet service install`.
func (m *Manager) Install(ctx context.Context, spec Spec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(spec.LogDir(), 0o700); err != nil {
		return fmt.Errorf("create service log directory %q: %w", spec.LogDir(), err)
	}

	switch m.goos {
	case "darwin":
		return m.installDarwin(ctx, spec)
	case "linux":
		return m.installLinux(ctx, spec)
	default:
		return ErrUnsupportedOS{GOOS: m.goos}
	}
}

// Uninstall stops and unloads the network's service, then removes the unit
// file. It is not an error to uninstall a network that has no service
// installed; it is simply a no-op in that case.
func (m *Manager) Uninstall(ctx context.Context, networkID string) error {
	switch m.goos {
	case "darwin":
		return m.uninstallDarwin(ctx, networkID)
	case "linux":
		return m.uninstallLinux(ctx, networkID)
	default:
		return ErrUnsupportedOS{GOOS: m.goos}
	}
}

// Start starts (or restarts, if already running) the network's installed
// service.
func (m *Manager) Start(ctx context.Context, networkID string) error {
	installed, err := m.IsInstalled(networkID)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("%w: run `moltnet service install` first", ErrNotInstalled)
	}

	switch m.goos {
	case "darwin":
		return m.startDarwin(ctx, networkID)
	case "linux":
		return m.startLinux(ctx, networkID)
	default:
		return ErrUnsupportedOS{GOOS: m.goos}
	}
}

// Stop stops the network's installed service without uninstalling it.
func (m *Manager) Stop(ctx context.Context, networkID string) error {
	installed, err := m.IsInstalled(networkID)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("%w: run `moltnet service install` first", ErrNotInstalled)
	}

	switch m.goos {
	case "darwin":
		return m.stopDarwin(ctx, networkID)
	case "linux":
		return m.stopLinux(ctx, networkID)
	default:
		return ErrUnsupportedOS{GOOS: m.goos}
	}
}

// Restart restarts the network's installed service, propagating
// ErrNotInstalled when it is not (the `pair --restart` error case).
func (m *Manager) Restart(ctx context.Context, networkID string) error {
	installed, err := m.IsInstalled(networkID)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("%w: run `moltnet service install` first, or restart the server manually", ErrNotInstalled)
	}

	switch m.goos {
	case "darwin":
		return m.restartDarwin(ctx, networkID)
	case "linux":
		return m.restartLinux(ctx, networkID)
	default:
		return ErrUnsupportedOS{GOOS: m.goos}
	}
}

// Status reports whether the network's service is installed and running.
func (m *Manager) Status(ctx context.Context, networkID string) (Status, error) {
	installed, err := m.IsInstalled(networkID)
	if err != nil {
		return Status{}, err
	}
	if !installed {
		return Status{Installed: false}, nil
	}

	switch m.goos {
	case "darwin":
		return m.statusDarwin(ctx, networkID)
	case "linux":
		return m.statusLinux(ctx, networkID)
	default:
		return Status{}, ErrUnsupportedOS{GOOS: m.goos}
	}
}

// IsInstalled reports whether a unit/plist file exists for networkID,
// independent of the current GOOS's support: `pair --restart` uses this to
// give a clear "no managed service for this network" error rather than an
// OS-support error when the operator simply never ran `service install`.
func (m *Manager) IsInstalled(networkID string) (bool, error) {
	path, err := m.UnitPath(networkID)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect %q: %w", path, err)
	}
	return !info.IsDir(), nil
}

// UnitPath returns the platform-specific unit/plist file path for
// networkID: the plist under ~/Library/LaunchAgents on darwin, the unit
// file under ~/.config/systemd/user on linux. Callers (the CLI's `service`
// command) use this to tell the operator exactly what file was written.
func (m *Manager) UnitPath(networkID string) (string, error) {
	switch m.goos {
	case "darwin":
		return LaunchdPlistPath(networkID)
	case "linux":
		return SystemdUnitPath(networkID)
	default:
		return "", ErrUnsupportedOS{GOOS: m.goos}
	}
}

func writeUnitFile(path string, contents string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %q: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}
