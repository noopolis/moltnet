package service

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// launchdDomain returns the "gui/<uid>" launchctl domain target for the
// current user — the modern (`bootstrap`/`bootout`/`kickstart`) launchctl
// subcommands operate on a domain + label/plist target rather than a bare
// label, unlike the deprecated `load`/`unload`/`start`/`stop`.
func launchdDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func launchdServiceTarget(networkID string) string {
	return launchdDomain() + "/" + LaunchdLabel(networkID)
}

func (m *Manager) installDarwin(ctx context.Context, spec Spec) error {
	contents, err := RenderLaunchdPlist(spec)
	if err != nil {
		return err
	}
	path, err := LaunchdPlistPath(spec.NetworkID)
	if err != nil {
		return err
	}
	if err := writeUnitFile(path, contents); err != nil {
		return err
	}

	// bootout first so a re-install always reloads the freshly written
	// plist instead of `bootstrap` refusing because the label is already
	// loaded; a "not loaded" failure here is expected and ignored.
	_, _ = m.runner.Run(ctx, "launchctl", "bootout", launchdServiceTarget(spec.NetworkID))

	if _, err := m.runner.Run(ctx, "launchctl", "bootstrap", launchdDomain(), path); err != nil {
		return fmt.Errorf("load LaunchAgent %s: %w", path, err)
	}
	return nil
}

func (m *Manager) uninstallDarwin(ctx context.Context, networkID string) error {
	installed, err := m.IsInstalled(networkID)
	if err != nil {
		return err
	}
	if !installed {
		return nil
	}

	_, _ = m.runner.Run(ctx, "launchctl", "bootout", launchdServiceTarget(networkID))

	path, err := LaunchdPlistPath(networkID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %q: %w", path, err)
	}
	return nil
}

func (m *Manager) startDarwin(ctx context.Context, networkID string) error {
	path, err := LaunchdPlistPath(networkID)
	if err != nil {
		return err
	}
	// bootstrap is idempotent in effect here: if it's already loaded this
	// fails with "service already loaded", which is fine — kickstart below
	// is what actually (re)starts the process either way.
	_, _ = m.runner.Run(ctx, "launchctl", "bootstrap", launchdDomain(), path)
	if _, err := m.runner.Run(ctx, "launchctl", "kickstart", launchdServiceTarget(networkID)); err != nil {
		return fmt.Errorf("start LaunchAgent %s: %w", LaunchdLabel(networkID), err)
	}
	return nil
}

func (m *Manager) stopDarwin(ctx context.Context, networkID string) error {
	if _, err := m.runner.Run(ctx, "launchctl", "bootout", launchdServiceTarget(networkID)); err != nil {
		return fmt.Errorf("stop LaunchAgent %s: %w", LaunchdLabel(networkID), err)
	}
	return nil
}

func (m *Manager) restartDarwin(ctx context.Context, networkID string) error {
	path, err := LaunchdPlistPath(networkID)
	if err != nil {
		return err
	}
	_, _ = m.runner.Run(ctx, "launchctl", "bootstrap", launchdDomain(), path)
	if _, err := m.runner.Run(ctx, "launchctl", "kickstart", "-k", launchdServiceTarget(networkID)); err != nil {
		return fmt.Errorf("restart LaunchAgent %s: %w", LaunchdLabel(networkID), err)
	}
	return nil
}

func (m *Manager) statusDarwin(ctx context.Context, networkID string) (Status, error) {
	output, err := m.runner.Run(ctx, "launchctl", "print", launchdServiceTarget(networkID))
	detail := strings.TrimSpace(string(output))
	if err != nil {
		if detail == "" {
			detail = err.Error()
		}
		return Status{Installed: true, Running: false, Detail: detail}, nil
	}
	running := strings.Contains(detail, "state = running")
	return Status{Installed: true, Running: running, Detail: detail}, nil
}
