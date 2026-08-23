package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
)

// TestRunPairInviteShowAcceptsNetworkIDAlias pins the fix for a real naming
// collision: `pair invite --id X` (runPairInvite) is the *pairing* id, but
// `pair invite show X --id Y` is the *network* id -- `pair invite` itself
// already offers --network-id as a spelled-out alias for that meaning, and
// `show` must accept the same alias rather than only the overloaded --id.
//
// Two networks are set up under a fake ~/.moltnet so id-based disambiguation
// (app.ResolveConfigPath, not just --config) is what actually resolves the
// path: --network-id selecting the wrong network must fail to find the
// receipt (proving the flag was not silently ignored), and selecting the
// right one must recover the exact code.
func TestRunPairInviteShowAcceptsNetworkIDAlias(t *testing.T) {
	t.Setenv("TEST_RELAY_TOKEN", "relay-secret-value")
	home := t.TempDir()
	t.Setenv("HOME", home)

	netADir := filepath.Join(home, ".moltnet", "net-a")
	netBDir := filepath.Join(home, ".moltnet", "net-b")
	if err := os.MkdirAll(netADir, 0o700); err != nil {
		t.Fatalf("mkdir net-a: %v", err)
	}
	if err := os.MkdirAll(netBDir, 0o700); err != nil {
		t.Fatalf("mkdir net-b: %v", err)
	}
	pathA := writeMoltnetConfig(t, netADir, "net-a", "Net A")
	writeMoltnetConfig(t, netBDir, "net-b", "Net B")

	inviteOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite",
			"--relay-url", "wss://moltnet-relay.acme.workers.dev",
			"--relay-token-env", "TEST_RELAY_TOKEN",
			"--id", "friend-net",
			"--network-id", "net-a",
		}, "test"); err != nil {
			t.Fatalf("run() pair invite error = %v", err)
		}
	})
	code := extractInviteCode(t, inviteOutput)

	// Selecting the wrong network via --network-id must not find net-a's
	// receipt: if the flag were silently ignored (parsed but never wired to
	// networkID), this would instead resolve some other path and either
	// error differently or -- worse -- accidentally succeed against net-a's
	// receipt via a fallback this test needs to rule out.
	if err := run(context.Background(), []string{
		"pair", "invite", "show", "friend-net",
		"--network-id", "net-b",
	}, "test"); err == nil {
		t.Fatal("run() pair invite show --network-id net-b error = nil, want an error: net-b never received this invite")
	}

	showOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "invite", "show", "friend-net",
			"--network-id", "net-a",
		}, "test"); err != nil {
			t.Fatalf("run() pair invite show --network-id net-a error = %v", err)
		}
	})

	recovered := extractInviteCode(t, showOutput)
	if recovered != code {
		t.Fatalf("pair invite show --network-id output = %q, want byte-identical %q", recovered, code)
	}

	// Sanity: the receipt genuinely lives under net-a's config, confirming
	// the --id invocation above and the --network-id one resolved the same
	// path (not merely two different flags that happened to both work by
	// accident).
	if _, _, err := app.LoadInviteReceipt(pathA, "friend-net"); err != nil {
		t.Fatalf("LoadInviteReceipt(pathA) error = %v", err)
	}
}
