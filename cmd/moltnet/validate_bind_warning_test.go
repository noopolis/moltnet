package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRunValidateWarnsOnNonLoopbackOpenRegistration covers PLAN.md phase
// 6a, item 2: `moltnet validate` must warn when listen_addr resolves
// non-loopback while agent_registration is open — "any reachable host may
// register an agent."
func TestRunValidateWarnsOnNonLoopbackOpenRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Moltnet")
	contents := strings.Replace(
		defaultMoltnetConfig("acme", "Acme Moltnet", "test-operator-token"),
		`listen_addr: "127.0.0.1:8787"`,
		`listen_addr: "0.0.0.0:8787"`,
		1,
	)
	writeNodeConfig(t, path, contents)

	output := captureStdout(t, func() {
		if err := runValidate([]string{path}); err != nil {
			t.Fatalf("runValidate() error = %v", err)
		}
	})

	if !strings.Contains(output, "warning:") {
		t.Fatalf("expected a warning: line for a non-loopback bind with open registration, got %q", output)
	}
	if !strings.Contains(output, "0.0.0.0:8787") {
		t.Fatalf("expected the warning to name the listen_addr, got %q", output)
	}
}

// TestRunValidateSilentOnDefaultLoopbackConfig covers the negative space for
// the same check: the default `moltnet init` output (loopback bind, open
// registration) must never warn — nothing off-machine can reach it.
func TestRunValidateSilentOnDefaultLoopbackConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Moltnet")
	writeNodeConfig(t, path, defaultMoltnetConfig("acme", "Acme Moltnet", "test-operator-token"))

	output := captureStdout(t, func() {
		if err := runValidate([]string{path}); err != nil {
			t.Fatalf("runValidate() error = %v", err)
		}
	})

	if strings.Contains(output, "warning:") {
		t.Fatalf("expected no warning: line for the default loopback config, got %q", output)
	}
}

// TestRunValidateSilentOnNonLoopbackWithoutOpenRegistration covers the other
// half of the negative space: a non-loopback bind alone, with auth.mode:
// bearer and registration not open (bearerMoltnetConfig's default — see
// templates.go, PLAN.md phase 6a review P2-1), must not warn — this is the
// ordinary "securing remote agents" bearer-token shape (see
// website/src/content/docs/guides/securing-remote-agents.md), and it must
// stay quiet.
func TestRunValidateSilentOnNonLoopbackWithoutOpenRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Moltnet")
	contents := strings.Replace(
		bearerMoltnetConfig("acme", "Acme Moltnet", "operator-token-value", "console-token-value", "127.0.0.1:8787"),
		`listen_addr: "127.0.0.1:8787"`,
		`listen_addr: "0.0.0.0:8787"`,
		1,
	)
	writeNodeConfig(t, path, contents)

	output := captureStdout(t, func() {
		if err := runValidate([]string{path}); err != nil {
			t.Fatalf("runValidate() error = %v", err)
		}
	})

	if strings.Contains(output, "warning:") {
		t.Fatalf("expected no warning: line for a non-loopback bearer bind with registration disabled, got %q", output)
	}
}

// TestRunValidateWarnsOnNonLoopbackModeNone covers PLAN.md phase 6a review,
// P2-3: a non-loopback bind with auth.mode: none (every write and admin
// route anonymous, with no registration step needed at all — the
// pre-phase-6a default) must warn, even though agent_registration is not
// "open". The original check keyed only on agent_registration and stayed
// silent on exactly this shape.
func TestRunValidateWarnsOnNonLoopbackModeNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Moltnet")
	contents := "version: moltnet.v1\n\nnetwork:\n  id: \"acme\"\n  name: \"Acme Moltnet\"\n\nserver:\n  listen_addr: \"0.0.0.0:8787\"\n  human_ingress: true\n  debug_events: false\n\nstorage:\n  kind: sqlite\n  sqlite:\n    path: .moltnet/moltnet.db\n\nrooms: []\npairings: []\n"
	writeNodeConfig(t, path, contents)

	output := captureStdout(t, func() {
		if err := runValidate([]string{path}); err != nil {
			t.Fatalf("runValidate() error = %v", err)
		}
	})

	if !strings.Contains(output, "warning:") {
		t.Fatalf("expected a warning: line for a non-loopback bind with auth.mode: none, got %q", output)
	}
	if !strings.Contains(output, "0.0.0.0:8787") {
		t.Fatalf("expected the warning to name the listen_addr, got %q", output)
	}
}

// TestRunValidateSeesListenAddrEnvOverride covers PLAN.md phase 6a review,
// P3: `moltnet validate` used to load config with app.LoadFile, which never
// merges environment overrides, so `MOLTNET_LISTEN_ADDR=0.0.0.0 moltnet
// validate` against an otherwise-loopback config stayed silent about an
// exposure the server itself (which loads through the env-merged
// app.LoadConfigForPath) would warn about the moment it actually started.
func TestRunValidateSeesListenAddrEnvOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Moltnet")
	writeNodeConfig(t, path, defaultMoltnetConfig("acme", "Acme Moltnet", "test-operator-token"))

	t.Setenv("MOLTNET_LISTEN_ADDR", "0.0.0.0:8787")
	output := captureStdout(t, func() {
		if err := runValidate([]string{path}); err != nil {
			t.Fatalf("runValidate() error = %v", err)
		}
	})

	if !strings.Contains(output, "warning:") {
		t.Fatalf("expected MOLTNET_LISTEN_ADDR=0.0.0.0 to produce a warning: line, got %q", output)
	}
	if !strings.Contains(output, "0.0.0.0:8787") {
		t.Fatalf("expected the warning to name the env-overridden listen_addr, got %q", output)
	}
}
