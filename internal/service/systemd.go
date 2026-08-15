package service

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// systemdUnitDir returns ~/.config/systemd/user, the directory every
// network's systemd user unit lives in.
func systemdUnitDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// SystemdUnitPath returns ~/.config/systemd/user/moltnet-<network-id>.service.
func SystemdUnitPath(networkID string) (string, error) {
	dir, err := systemdUnitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, SystemdUnitName(networkID)), nil
}

// installedSystemdNetworkIDs lists the network ids with a
// moltnet-<id>.service under ~/.config/systemd/user, by scanning the
// directory and parsing ids back out of the SystemdUnitName naming
// convention. `moltnet uninstall` unions this with the ~/.moltnet/<id>/
// directory listing so a dangling unit (its network directory already
// removed by hand) still gets stopped and disabled. A missing directory is
// not an error: it just means no units have ever been installed.
func installedSystemdNetworkIDs() ([]string, error) {
	dir, err := systemdUnitDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list %q: %w", dir, err)
	}

	const prefix, suffix = "moltnet-", ".service"
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		if id := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// RenderSystemdUnit renders the systemd user unit for spec. It is a pure
// function (no filesystem or systemctl access) so it can be golden-tested.
//
// Restart=always plus a short RestartSec keeps the server running across
// crashes, mirroring the launchd KeepAlive behavior; stdout/stderr go to
// fixed log files under spec.LogDir() rather than only the journal, so
// `moltnet service status` can point at the same kind of path on both OSes.
func RenderSystemdUnit(spec Spec) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	buf.WriteString("[Unit]\n")
	fmt.Fprintf(&buf, "Description=Moltnet server (%s)\n", spec.NetworkID)
	buf.WriteString("After=network-online.target\n")
	buf.WriteString("Wants=network-online.target\n\n")

	buf.WriteString("[Service]\n")
	buf.WriteString("Type=simple\n")
	fmt.Fprintf(&buf, "ExecStart=%s\n", quoteSystemdArgs(spec.BinaryPath, "start", "--config", spec.ConfigPath))
	fmt.Fprintf(&buf, "WorkingDirectory=%s\n", spec.NetworkDir)
	fmt.Fprintf(&buf, "StandardOutput=append:%s\n", spec.StdoutLogPath())
	fmt.Fprintf(&buf, "StandardError=append:%s\n", spec.StderrLogPath())
	buf.WriteString("Restart=always\n")
	buf.WriteString("RestartSec=2\n\n")

	buf.WriteString("[Install]\n")
	buf.WriteString("WantedBy=default.target\n")

	return buf.String(), nil
}

// quoteSystemdArgs joins argv into one ExecStart= line, quoting any
// argument that contains whitespace so systemd's unit-file argv splitting
// (which is whitespace-based) does not split a single path in two.
func quoteSystemdArgs(argv ...string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		if strings.ContainsAny(arg, " \t\"'") {
			escaped := strings.ReplaceAll(arg, `"`, `\"`)
			quoted = append(quoted, `"`+escaped+`"`)
			continue
		}
		quoted = append(quoted, arg)
	}
	return strings.Join(quoted, " ")
}
