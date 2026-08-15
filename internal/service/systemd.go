package service

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemdUnitPath returns ~/.config/systemd/user/moltnet-<network-id>.service.
func SystemdUnitPath(networkID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user", SystemdUnitName(networkID)), nil
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
