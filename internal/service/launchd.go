package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// launchdAgentsDir returns ~/Library/LaunchAgents, the directory every
// network's LaunchAgent plist lives in.
func launchdAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// LaunchdPlistPath returns ~/Library/LaunchAgents/dev.moltnet.<network-id>.plist.
func LaunchdPlistPath(networkID string) (string, error) {
	dir, err := launchdAgentsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, LaunchdLabel(networkID)+".plist"), nil
}

// installedLaunchdNetworkIDs lists the network ids with a
// dev.moltnet.<id>.plist under ~/Library/LaunchAgents, by scanning the
// directory and parsing ids back out of the LaunchdLabel naming convention.
// `moltnet uninstall` unions this with the ~/.moltnet/<id>/ directory
// listing so a dangling LaunchAgent (its network directory already removed
// by hand) still gets stopped and unloaded. A missing directory is not an
// error: it just means no LaunchAgents have ever been installed.
func installedLaunchdNetworkIDs() ([]string, error) {
	dir, err := launchdAgentsDir()
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

	const prefix, suffix = "dev.moltnet.", ".plist"
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

// RenderLaunchdPlist renders the LaunchAgent plist for spec. It is a pure
// function (no filesystem or launchctl access) so it can be golden-tested.
//
// KeepAlive + RunAtLoad restart the server on crash and at login; stdout and
// stderr are redirected to fixed log files under spec.LogDir() so `moltnet
// service status` has somewhere to point operators at. Every path value is
// XML-escaped via plistString so an unusual (but valid) filesystem path
// never produces malformed XML.
func RenderLaunchdPlist(spec Spec) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	buf.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	buf.WriteString("<plist version=\"1.0\">\n<dict>\n")

	writeKV(&buf, "Label", LaunchdLabel(spec.NetworkID))

	buf.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range []string{spec.BinaryPath, "start", "--config", spec.ConfigPath} {
		buf.WriteString("\t\t")
		writeString(&buf, arg)
		buf.WriteString("\n")
	}
	buf.WriteString("\t</array>\n")

	writeKV(&buf, "WorkingDirectory", spec.NetworkDir)
	writeKV(&buf, "StandardOutPath", spec.StdoutLogPath())
	writeKV(&buf, "StandardErrorPath", spec.StderrLogPath())
	buf.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	buf.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	writeKV(&buf, "ProcessType", "Background")

	buf.WriteString("</dict>\n</plist>\n")

	return buf.String(), nil
}

// writeKV writes one <key>/<string> pair, each indented one level and
// XML-escaped.
func writeKV(buf *bytes.Buffer, key, value string) {
	buf.WriteString("\t<key>")
	buf.WriteString(escapeXML(key))
	buf.WriteString("</key>\n\t")
	writeString(buf, value)
	buf.WriteString("\n")
}

func writeString(buf *bytes.Buffer, value string) {
	buf.WriteString("<string>")
	buf.WriteString(escapeXML(value))
	buf.WriteString("</string>")
}

func escapeXML(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}
