package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// pairListShowTestConfigBody declares one pairing, "pending-friend", whose
// remote_base_url points at a port nothing is listening on (127.0.0.1:1,
// the lowest unprivileged-adjacent port, reserved and never bound in
// practice) -- a fast, deterministic stand-in for "this peer has never
// answered the invite yet, or is currently unreachable" without needing a
// real second server or relay.
func pairListShowTestConfigBody(addr string) string {
	return `version: moltnet.v1

network:
  id: acme
  name: "Acme"

server:
  listen_addr: "` + addr + `"

storage:
  kind: memory

auth:
  mode: bearer
  tokens:
    - id: operator
      value: operator-secret
      scopes: [observe, write, admin]

pairings:
  - id: pending-friend
    remote_network_id: friend-net
    remote_base_url: http://127.0.0.1:1
    token: pending-secret-token
`
}

// TestPairListPrintsConfiguredPairings covers 7B.4's "pair list": trivial
// today (GET /v1/pairings), but nothing before this unit exposed it from the
// CLI at all.
func TestPairListPrintsConfiguredPairings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, pairListShowTestConfigBody(addr))

	server := startRealMoltnetServer(t, configPath)
	defer server.Close()

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "list",
			"--network", "acme",
		}, "test"); err != nil {
			t.Fatalf("run() pair list error = %v", err)
		}
	})

	if !strings.Contains(output, "pending-friend") {
		t.Fatalf("pair list output = %q, want it to name the configured pairing", output)
	}
}

// TestPairShowUnreachablePeerProducesPlainMessage is 7B.4's required test:
// pair show against an unreachable/pending peer must produce the intended
// plain-language message, not a raw transport error (e.g. "connection
// refused", a redacted URL, or a bare "502 Bad Gateway").
func TestPairShowUnreachablePeerProducesPlainMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, pairListShowTestConfigBody(addr))

	server := startRealMoltnetServer(t, configPath)
	defer server.Close()

	err := run(context.Background(), []string{
		"pair", "show", "pending-friend",
		"--network", "acme",
	}, "test")
	if err == nil {
		t.Fatal("expected pair show against an unreachable peer to fail")
	}
	message := err.Error()
	if !strings.Contains(message, "pending-friend") || !strings.Contains(message, "unreachable") {
		t.Fatalf("error message = %q, want it to plainly name the pairing and say it is unreachable", message)
	}
	if strings.Contains(message, "connection refused") ||
		strings.Contains(message, "Bad Gateway") ||
		strings.Contains(message, "502") ||
		strings.Contains(message, "127.0.0.1:1") {
		t.Fatalf("error message = %q, leaks a raw transport/URL detail instead of the intended plain message", message)
	}
}

// TestPairShowRevokedPairingSaysRevokedNotUnknown is F5's required
// regression (review round 2, confirmed live): a pairing hand-marked
// `status: revoked` in config -- firstUniqueActivePairing's own doc comment
// (internal/rooms/pairings.go) calls this the manual stopgap an operator
// reaches for before `pair revoke` finishes -- fails closed identically to a
// pairing id that never existed, so `pair show` used to say "unknown
// pairing" for it. `pair list` shows the very same id in the same breath
// with `"status": "revoked"`, so that message actively contradicts what the
// operator can already see; `pair show` must say "revoked" instead.
func TestPairShowRevokedPairingSaysRevokedNotUnknown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, pairListShowTestConfigBody(addr)+`  - id: friend-revoked
    remote_network_id: friend-net
    remote_base_url: http://127.0.0.1:1
    token: revoked-secret-token
    status: revoked
`)

	server := startRealMoltnetServer(t, configPath)
	defer server.Close()

	listOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "list",
			"--network", "acme",
		}, "test"); err != nil {
			t.Fatalf("run() pair list error = %v", err)
		}
	})
	if !strings.Contains(listOutput, "friend-revoked") || !strings.Contains(listOutput, `"revoked"`) {
		t.Fatalf("pair list output = %q, want it to name friend-revoked with a revoked status", listOutput)
	}

	err := run(context.Background(), []string{
		"pair", "show", "friend-revoked",
		"--network", "acme",
	}, "test")
	if err == nil {
		t.Fatal("expected pair show against a revoked pairing to fail")
	}
	message := err.Error()
	if !strings.Contains(message, "friend-revoked") || !strings.Contains(message, "revoked") {
		t.Fatalf("error message = %q, want it to plainly name the pairing and say it is revoked", message)
	}
	if strings.Contains(message, "unknown pairing") {
		t.Fatalf("error message = %q, still says \"unknown pairing\" for a pairing `pair list` shows as revoked", message)
	}
}

// TestPairShowUnknownPairingIDContainingBadGatewayIsNotMisreported is the new
// P2 finding's required regression: isPairingUnreachableError used to
// substring-match the literal text "bad_gateway" against the *entire*
// formatted client error, which embeds the request URL and body -- both of
// which contain the caller-supplied pairing id. A pairing id that itself
// contains "bad_gateway" made a genuine 404 (unknown pairing) match that
// substring purely by coincidence and get reported as "peer is unreachable"
// instead of "unknown pairing". isPairingUnreachableError now checks
// moltnetclient.APIError's typed Status field, which the pairing id cannot
// influence.
func TestPairShowUnknownPairingIDContainingBadGatewayIsNotMisreported(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, pairListShowTestConfigBody(addr))

	server := startRealMoltnetServer(t, configPath)
	defer server.Close()

	err := run(context.Background(), []string{
		"pair", "show", "bad_gateway",
		"--network", "acme",
	}, "test")
	if err == nil {
		t.Fatal("expected pair show against an unknown pairing id to fail")
	}
	message := err.Error()
	if strings.Contains(message, "unreachable") {
		t.Fatalf("error message = %q, misreported a 404 unknown-pairing as an unreachable peer because the pairing id contains \"bad_gateway\"", message)
	}
	if !strings.Contains(message, "unknown pairing") {
		t.Fatalf("error message = %q, want it to say the pairing is unknown", message)
	}
}
