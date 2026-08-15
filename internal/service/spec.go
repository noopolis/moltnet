// Package service generates and manages an OS service definition — a
// launchd LaunchAgent on macOS, a systemd user unit on Linux — that keeps
// one Moltnet network's server running across reboots and restarts it on
// crash. It backs `moltnet service install|uninstall|start|stop|status`
// (PLAN.md phase 4, item 2).
package service

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Spec describes the managed Moltnet server process for one network. All
// paths must be absolute: both launchd and systemd run units outside any
// particular working directory, so a relative path would resolve
// differently (or not at all) once the agent/unit is actually loaded.
type Spec struct {
	// NetworkID identifies the network and names the generated unit/label.
	NetworkID string
	// ConfigPath is the absolute path to the network's Moltnet config file,
	// passed to `moltnet start --config`.
	ConfigPath string
	// BinaryPath is the absolute path to the moltnet executable to run.
	BinaryPath string
	// NetworkDir is the absolute network home directory; stdout/stderr logs
	// are written under NetworkDir/.moltnet/.
	NetworkDir string
}

// Validate reports the first missing required field, so callers get one
// clear error instead of a generated unit referencing an empty path.
func (s Spec) Validate() error {
	if strings.TrimSpace(s.NetworkID) == "" {
		return fmt.Errorf("service spec: network id is required")
	}
	if !filepath.IsAbs(s.ConfigPath) {
		return fmt.Errorf("service spec: config path %q must be absolute", s.ConfigPath)
	}
	if !filepath.IsAbs(s.BinaryPath) {
		return fmt.Errorf("service spec: binary path %q must be absolute", s.BinaryPath)
	}
	if !filepath.IsAbs(s.NetworkDir) {
		return fmt.Errorf("service spec: network dir %q must be absolute", s.NetworkDir)
	}
	return nil
}

// LogDir returns the directory service logs are written to: NetworkDir's
// .moltnet runtime-state directory, matching the convention used for
// relay.json and other per-network runtime files.
func (s Spec) LogDir() string {
	return filepath.Join(s.NetworkDir, ".moltnet")
}

// StdoutLogPath and StderrLogPath name the fixed log files launchd/systemd
// redirect the managed process's output to.
func (s Spec) StdoutLogPath() string { return filepath.Join(s.LogDir(), "service.out.log") }
func (s Spec) StderrLogPath() string { return filepath.Join(s.LogDir(), "service.err.log") }

// LaunchdLabel is the LaunchAgent label/filename stem used on darwin.
func LaunchdLabel(networkID string) string {
	return "dev.moltnet." + strings.TrimSpace(networkID)
}

// SystemdUnitName is the user unit file name used on linux.
func SystemdUnitName(networkID string) string {
	return "moltnet-" + strings.TrimSpace(networkID) + ".service"
}

// ErrUnsupportedOS is returned by lifecycle operations on any GOOS other
// than darwin and linux.
type ErrUnsupportedOS struct {
	GOOS string
}

func (e ErrUnsupportedOS) Error() string {
	return fmt.Sprintf("moltnet service is not supported on %s (supported: darwin, linux)", e.GOOS)
}
