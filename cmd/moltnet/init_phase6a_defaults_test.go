package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunInitWritesPhase6aDefaults covers PLAN.md phase 6a, item 1: a fresh
// network — with or without --bearer — is written loopback-only and open to
// self-registration, never the old wildcard bind with no registration path.
func TestRunInitWritesPhase6aDefaults(t *testing.T) {
	t.Run("without --bearer", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		captureStdout(t, func() {
			if err := runInit(context.Background(), []string{"--id", "acme"}); err != nil {
				t.Fatalf("runInit() error = %v", err)
			}
		})

		contents, err := os.ReadFile(filepath.Join(home, ".moltnet", "acme", "Moltnet"))
		if err != nil {
			t.Fatalf("read written config: %v", err)
		}
		body := string(contents)
		for _, want := range []string{
			`listen_addr: "127.0.0.1:8787"`,
			"mode: open",
			"agent_registration: open",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("fresh config missing %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, `listen_addr: ":8787"`) {
			t.Fatalf("fresh config must not bind every interface by default:\n%s", body)
		}
	})

	// PLAN.md phase 6a review, P2-1: --bearer must keep agent registration
	// exactly as it was at HEAD (disabled) — an earlier draft of phase 6a
	// set agent_registration: open here too, which made --bearer the one
	// strictly *looser* change in the whole review (anonymous
	// POST /v1/agents/register went 401 -> 201-with-write-token on a
	// network an operator explicitly locked down with tokens). --bearer is
	// the token-controlled opt-in; it stays token-controlled.
	t.Run("with --bearer", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		captureStdout(t, func() {
			if err := runInit(context.Background(), []string{"--id", "acme", "--bearer"}); err != nil {
				t.Fatalf("runInit() --bearer error = %v", err)
			}
		})

		contents, err := os.ReadFile(filepath.Join(home, ".moltnet", "acme", "Moltnet"))
		if err != nil {
			t.Fatalf("read written config: %v", err)
		}
		body := string(contents)
		for _, want := range []string{
			`listen_addr: "127.0.0.1:8787"`,
			"mode: bearer",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("fresh --bearer config missing %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "agent_registration") {
			t.Fatalf("--bearer must not set agent_registration (stays disabled, matching HEAD):\n%s", body)
		}
	})
}

// TestRunInitNeverRewritesExistingConfig covers PLAN.md phase 6a's explicit
// non-goal: an existing config predating this change (wildcard bind, no
// auth section at all — the pre-phase-6a `moltnet init` shape) must be
// byte-for-byte untouched by a later `moltnet init` rerun against the same
// directory, never migrated onto the new defaults.
func TestRunInitNeverRewritesExistingConfig(t *testing.T) {
	directory := t.TempDir()
	serverPath := filepath.Join(directory, "Moltnet")
	preexisting := "version: moltnet.v1\n\nnetwork:\n  id: \"legacy\"\n  name: \"Legacy Moltnet\"\n\nserver:\n  listen_addr: \":8787\"\n  human_ingress: true\n  debug_events: false\n\nstorage:\n  kind: sqlite\n  sqlite:\n    path: .moltnet/moltnet.db\n\nrooms: []\npairings: []\n"
	writeNodeConfig(t, serverPath, preexisting)

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() rerun error = %v", err)
		}
	})

	contents, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatalf("read %q: %v", serverPath, err)
	}
	if string(contents) != preexisting {
		t.Fatalf("existing config was rewritten\n got:\n%s\nwant (unchanged):\n%s", contents, preexisting)
	}
}
