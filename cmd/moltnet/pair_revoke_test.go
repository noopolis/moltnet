package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// pairRevokeTestConfigBody is a completed pairing exactly as `pair invite`
// plus `pair <code>` would have left it: a pairings[] entry, the peer's
// inbound auth.tokens[] credential under the same id, and a room whose
// federation names that pairing. This is the fixture `TestPairRevoke...`
// below revokes.
func pairRevokeTestConfigBody(addr string) string {
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
    - id: friend-1234
      value: peer-secret-token
      scopes: [pair]

rooms:
  - id: chat
    federation: [friend-1234]
    write_policy: members
    members: [remote-net:member]

pairings:
  - id: friend-1234
    remote_network_id: remote-net
    token: peer-secret-token
    relay:
      url: wss://relay.example/friend-1234
      room: relay-room-friend-1234
      token: relay-secret-token
`
}

func peerMessageRequest(id string) protocol.SendMessageRequest {
	return protocol.SendMessageRequest{
		ID:     id,
		Origin: protocol.MessageOrigin{NetworkID: "remote-net", MessageID: id},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "chat"},
		From:   protocol.Actor{Type: "agent", ID: "member", NetworkID: "remote-net"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello from the peer"}},
	}
}

// TestPairRevokeWithServerDownDoesNotSuggestARestart is F5's required
// regression (review round 2, confirmed live): printPairRevokeAftercare used
// to print "restart the Moltnet server for this pairing to take effect"
// unconditionally, even in the branch reached only when probeStatusHealth
// has just confirmed no server is running for this network at all -- an
// operator cannot restart a server that was never started, and this instructs
// them to attempt a no-op.
func TestPairRevokeWithServerDownDoesNotSuggestARestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, pairRevokeTestConfigBody(addr))

	// Never started: this is the "was never running at all" case, not
	// "was running and stopped" -- probeStatusHealth's connection-refused
	// classification (F3) treats both identically, so either reproduces it.

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "revoke", "friend-1234",
			"--config", configPath,
		}, "test"); err != nil {
			t.Fatalf("run() pair revoke (server down) error = %v, want nil", err)
		}
	})

	if strings.Contains(output, "restart the Moltnet server for this pairing to take effect") {
		t.Fatalf("pair revoke output = %q, told the operator to restart a server that was never running", output)
	}
	if !strings.Contains(output, "revoked pairing") {
		t.Fatalf("pair revoke output = %q, want it to still confirm the revoke", output)
	}
}

// TestPairRevokeRemovesTokenAndFederationThenServerRejectsPeer is 7B.3's
// required test: after revoke, the peer's former credential is rejected by
// the server -- not merely marked with some "revoked" status -- and both the
// pairings[] entry and the matching auth.tokens[] entry are gone from
// config, and the room's federation grant for this pairing is gone too.
func TestPairRevokeRemovesTokenAndFederationThenServerRejectsPeer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, pairRevokeTestConfigBody(addr))

	peerClient, err := moltnetclient.New(clientconfig.AttachmentConfig{
		BaseURL: "http://" + addr,
		Auth:    clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeBearer, Token: "peer-secret-token"},
	})
	if err != nil {
		t.Fatalf("build peer client: %v", err)
	}

	server, instance := startRestartableMoltnetServer(t, configPath)
	if _, err := peerClient.SendMessage(context.Background(), peerMessageRequest("pre-revoke")); err != nil {
		t.Fatalf("peer send before revoke: %v (fixture is supposed to accept it)", err)
	}
	server.Close()
	if err := instance.Close(); err != nil {
		t.Fatalf("instance.Close() error = %v", err)
	}

	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"pair", "revoke", "friend-1234",
			"--config", configPath,
		}, "test"); err != nil {
			t.Fatalf("run() pair revoke error = %v", err)
		}
	})

	config, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() after revoke error = %v", err)
	}
	for _, pairing := range config.Pairings {
		if pairing.ID == "friend-1234" {
			t.Fatalf("pairings[] still has revoked pairing %#v", pairing)
		}
	}
	for _, token := range config.Auth.Tokens {
		if token.ID == "friend-1234" {
			t.Fatalf("auth.tokens[] still has the revoked peer's credential %#v", token)
		}
	}
	for _, room := range config.Rooms {
		if room.ID != "chat" {
			continue
		}
		if room.Federation != nil && protocol.RoomFederationAllows(room.Federation, "friend-1234") {
			t.Fatalf("room %q federation still allows the revoked pairing: %#v", room.ID, room.Federation)
		}
	}

	server2, instance2 := startRestartableMoltnetServer(t, configPath)
	defer server2.Close()
	defer func() { _ = instance2.Close() }()

	_, sendErr := peerClient.SendMessage(context.Background(), peerMessageRequest("post-revoke"))
	if sendErr == nil {
		t.Fatal("expected revoked peer credential to be rejected by the server, got nil error")
	}
	if !strings.Contains(sendErr.Error(), "401") {
		t.Fatalf("expected a 401 Unauthorized rejection for the revoked credential, got: %v", sendErr)
	}
}

// TestPairRevokeWithoutRestartFailsWhileServerStillRunning is F2's required
// regression: unlike the test above, the server here is never stopped
// across the revoke. There is no in-process hot-reload of auth policy or
// pairing state anywhere in this codebase, so the running server keeps
// trusting the just-revoked credential regardless of what was written to
// disk -- `pair revoke` without --restart must say so and fail, not print a
// bare "revoked" success line while the peer keeps writing right now.
func TestPairRevokeWithoutRestartFailsWhileServerStillRunning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	addr := freeLoopbackAddr(t)
	writeRoomCreateTestConfig(t, configPath, pairRevokeTestConfigBody(addr))

	peerClient, err := moltnetclient.New(clientconfig.AttachmentConfig{
		BaseURL: "http://" + addr,
		Auth:    clientconfig.AuthConfig{Mode: bridgeconfig.AuthModeBearer, Token: "peer-secret-token"},
	})
	if err != nil {
		t.Fatalf("build peer client: %v", err)
	}

	server, instance := startRestartableMoltnetServer(t, configPath)
	defer server.Close()
	defer func() { _ = instance.Close() }()

	if _, err := peerClient.SendMessage(context.Background(), peerMessageRequest("pre-revoke")); err != nil {
		t.Fatalf("peer send before revoke: %v (fixture is supposed to accept it)", err)
	}

	// Deliberately no --restart, and the server started above is never
	// stopped or replaced: this is exactly the gap F2 flagged.
	output := captureStdout(t, func() {
		err := run(context.Background(), []string{
			"pair", "revoke", "friend-1234",
			"--config", configPath,
		}, "test")
		if err == nil {
			t.Fatal("run() pair revoke error = nil, want a non-nil error: the server is still running the old policy")
		}
	})
	if !strings.Contains(output, "restart") {
		t.Fatalf("pair revoke output = %q, want it to say the server needs a restart", output)
	}

	// The point of the finding: the config was rewritten, but the live
	// server never reloaded it, so the "revoked" peer can still write right
	// now. A bare success message would have lied about this.
	if _, err := peerClient.SendMessage(context.Background(), peerMessageRequest("post-revoke-no-restart")); err != nil {
		t.Fatalf("peer send after config-only revoke (server never restarted) error = %v, want nil -- "+
			"the credential is still live in the running server's memory, which is the gap being reported", err)
	}

	config, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath() after revoke error = %v", err)
	}
	for _, pairing := range config.Pairings {
		if pairing.ID == "friend-1234" {
			t.Fatalf("pairings[] still has revoked pairing %#v", pairing)
		}
	}
	for _, token := range config.Auth.Tokens {
		if token.ID == "friend-1234" {
			t.Fatalf("auth.tokens[] still has the revoked peer's credential %#v", token)
		}
	}
}
