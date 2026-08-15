package service

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func (m *Manager) installLinux(ctx context.Context, spec Spec) error {
	contents, err := RenderSystemdUnit(spec)
	if err != nil {
		return err
	}
	path, err := SystemdUnitPath(spec.NetworkID)
	if err != nil {
		return err
	}
	if err := writeUnitFile(path, contents); err != nil {
		return err
	}

	if _, err := m.runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd user units: %w", err)
	}
	if _, err := m.runner.Run(ctx, "systemctl", "--user", "enable", "--now", SystemdUnitName(spec.NetworkID)); err != nil {
		return fmt.Errorf("enable systemd unit %s: %w", SystemdUnitName(spec.NetworkID), err)
	}
	return nil
}

func (m *Manager) uninstallLinux(ctx context.Context, networkID string) error {
	installed, err := m.IsInstalled(networkID)
	if err != nil {
		return err
	}
	if !installed {
		return nil
	}

	_, _ = m.runner.Run(ctx, "systemctl", "--user", "disable", "--now", SystemdUnitName(networkID))

	path, err := SystemdUnitPath(networkID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %q: %w", path, err)
	}
	_, _ = m.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
	return nil
}

func (m *Manager) startLinux(ctx context.Context, networkID string) error {
	if _, err := m.runner.Run(ctx, "systemctl", "--user", "start", SystemdUnitName(networkID)); err != nil {
		return fmt.Errorf("start systemd unit %s: %w", SystemdUnitName(networkID), err)
	}
	return nil
}

func (m *Manager) stopLinux(ctx context.Context, networkID string) error {
	if _, err := m.runner.Run(ctx, "systemctl", "--user", "stop", SystemdUnitName(networkID)); err != nil {
		return fmt.Errorf("stop systemd unit %s: %w", SystemdUnitName(networkID), err)
	}
	return nil
}

func (m *Manager) restartLinux(ctx context.Context, networkID string) error {
	if _, err := m.runner.Run(ctx, "systemctl", "--user", "restart", SystemdUnitName(networkID)); err != nil {
		return fmt.Errorf("restart systemd unit %s: %w", SystemdUnitName(networkID), err)
	}
	return nil
}

func (m *Manager) statusLinux(ctx context.Context, networkID string) (Status, error) {
	output, err := m.runner.Run(ctx, "systemctl", "--user", "status", SystemdUnitName(networkID))
	detail := strings.TrimSpace(string(output))
	if err != nil && detail == "" {
		detail = err.Error()
	}
	running := strings.Contains(detail, "Active: active (running)")
	return Status{Installed: true, Running: running, Detail: detail}, nil
}
