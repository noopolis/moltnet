package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/pkg/clientconfig"
)

// TestReadCursorIsolationBetweenAgentIdentities is PLAN.md 7C.2's core
// correctness requirement: two different agent identities (same network,
// different member id) sharing one cursor file must never read or advance
// each other's entry.
func TestReadCursorIsolationBetweenAgentIdentities(t *testing.T) {
	path := filepath.Join(t.TempDir(), readCursorFileName)
	target := targetRef{kind: "room", id: "general"}

	alpha := readCursorIdentity{path: path, keyPrefix: "agent:local_lab:alpha"}
	beta := readCursorIdentity{path: path, keyPrefix: "agent:local_lab:beta"}

	if err := advanceReadCursor(path, readCursorKey(alpha, target), "msg_alpha_1"); err != nil {
		t.Fatalf("advanceReadCursor(alpha) error = %v", err)
	}

	// beta has never read this target: its cursor must still be empty, not
	// alpha's, even though both entries now live in the same file.
	betaCursor, err := lookupReadCursor(path, readCursorKey(beta, target))
	if err != nil {
		t.Fatalf("lookupReadCursor(beta) error = %v", err)
	}
	if betaCursor != "" {
		t.Fatalf("beta's cursor = %q, want empty (isolated from alpha's entry)", betaCursor)
	}

	if err := advanceReadCursor(path, readCursorKey(beta, target), "msg_beta_1"); err != nil {
		t.Fatalf("advanceReadCursor(beta) error = %v", err)
	}

	// Advancing beta must not have clobbered alpha's own entry.
	alphaCursor, err := lookupReadCursor(path, readCursorKey(alpha, target))
	if err != nil {
		t.Fatalf("lookupReadCursor(alpha) error = %v", err)
	}
	if alphaCursor != "msg_alpha_1" {
		t.Fatalf("alpha's cursor = %q after beta advanced, want unchanged %q", alphaCursor, "msg_alpha_1")
	}
	betaCursor, err = lookupReadCursor(path, readCursorKey(beta, target))
	if err != nil {
		t.Fatalf("lookupReadCursor(beta) error = %v", err)
	}
	if betaCursor != "msg_beta_1" {
		t.Fatalf("beta's cursor = %q, want %q", betaCursor, "msg_beta_1")
	}
}

// TestReadCursorIsolationBetweenAgentAndOperator covers the degenerate case
// PLAN.md 7C.2's decision explicitly calls out: an agent's cursor file and
// the operator fallback's cursor file could, in principle, coincide (same
// directory). The "operator:<network>" key prefix never carries a member id,
// so it can never collide with any "agent:<network>:<member>" key, even for
// the same network.
func TestReadCursorIsolationBetweenAgentAndOperator(t *testing.T) {
	path := filepath.Join(t.TempDir(), readCursorFileName)
	target := targetRef{kind: "room", id: "general"}

	agent := readCursorIdentity{path: path, keyPrefix: "agent:fixture-net:alpha"}
	operator := readCursorIdentity{path: path, keyPrefix: "operator:fixture-net"}

	if agent.keyPrefix == operator.keyPrefix {
		t.Fatalf("agent and operator key prefixes must never be equal, got both %q", agent.keyPrefix)
	}

	if err := advanceReadCursor(path, readCursorKey(agent, target), "msg_agent_1"); err != nil {
		t.Fatalf("advanceReadCursor(agent) error = %v", err)
	}

	operatorCursor, err := lookupReadCursor(path, readCursorKey(operator, target))
	if err != nil {
		t.Fatalf("lookupReadCursor(operator) error = %v", err)
	}
	if operatorCursor != "" {
		t.Fatalf("operator's cursor = %q, want empty (isolated from the agent's entry)", operatorCursor)
	}

	if err := advanceReadCursor(path, readCursorKey(operator, target), "msg_operator_1"); err != nil {
		t.Fatalf("advanceReadCursor(operator) error = %v", err)
	}

	agentCursor, err := lookupReadCursor(path, readCursorKey(agent, target))
	if err != nil {
		t.Fatalf("lookupReadCursor(agent) error = %v", err)
	}
	if agentCursor != "msg_agent_1" {
		t.Fatalf("agent's cursor = %q after operator advanced, want unchanged %q", agentCursor, "msg_agent_1")
	}
}

// TestReadCursorIsolationBetweenTargets covers the other axis: the same
// identity reading two different targets must not have one target's cursor
// bleed into the other's -- message ids are only meaningful within their own
// stream.
func TestReadCursorIsolationBetweenTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), readCursorFileName)
	identity := readCursorIdentity{path: path, keyPrefix: "agent:local_lab:alpha"}
	general := targetRef{kind: "room", id: "general"}
	random := targetRef{kind: "room", id: "random"}

	if err := advanceReadCursor(path, readCursorKey(identity, general), "msg_general_1"); err != nil {
		t.Fatalf("advanceReadCursor(general) error = %v", err)
	}

	randomCursor, err := lookupReadCursor(path, readCursorKey(identity, random))
	if err != nil {
		t.Fatalf("lookupReadCursor(random) error = %v", err)
	}
	if randomCursor != "" {
		t.Fatalf("random room's cursor = %q, want empty (isolated from general's entry)", randomCursor)
	}
}

// TestLookupReadCursorFirstUseReturnsEmpty documents PLAN.md 7C.2's first-use
// behavior directly: no stored entry means an empty cursor, never an error.
func TestLookupReadCursorFirstUseReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), readCursorFileName)
	identity := readCursorIdentity{path: path, keyPrefix: "agent:local_lab:alpha"}
	target := targetRef{kind: "room", id: "general"}

	cursor, err := lookupReadCursor(path, readCursorKey(identity, target))
	if err != nil {
		t.Fatalf("lookupReadCursor() error = %v, want nil on a missing file", err)
	}
	if cursor != "" {
		t.Fatalf("lookupReadCursor() = %q, want empty on first use", cursor)
	}
}

// TestReadCursorIsolationDrivenThroughResolveReadCursorIdentity is F4's fix:
// unlike TestReadCursorIsolationBetweenAgentIdentities above (which
// hand-writes keyPrefix literals and so only ever exercises readCursorKey
// plus the read-modify-write, never resolveReadCursorIdentity itself), this
// drives both identities through the real resolver with two real
// AttachmentConfig values. Mutation-checked against the reviewer's finding:
// dropping attachment.MemberID from resolveReadCursorIdentity's agent-path
// keyPrefix (read_cursor.go) collapses alpha and beta onto the same prefix,
// and this test -- unlike the hand-written one -- catches that.
func TestReadCursorIsolationDrivenThroughResolveReadCursorIdentity(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{Auth: clientconfig.AuthConfig{Mode: "none"}, BaseURL: "http://127.0.0.1:8787", MemberID: "alpha", NetworkID: "local_lab"},
		},
	})

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	alphaAttachment := clientconfig.AttachmentConfig{MemberID: "alpha", NetworkID: "local_lab"}
	betaAttachment := clientconfig.AttachmentConfig{MemberID: "beta", NetworkID: "local_lab"}

	alphaIdentity, err := resolveReadCursorIdentity("", "", alphaAttachment, false, false)
	if err != nil {
		t.Fatalf("resolveReadCursorIdentity(alpha) error = %v", err)
	}
	betaIdentity, err := resolveReadCursorIdentity("", "", betaAttachment, false, false)
	if err != nil {
		t.Fatalf("resolveReadCursorIdentity(beta) error = %v", err)
	}

	if alphaIdentity.keyPrefix == betaIdentity.keyPrefix {
		t.Fatalf("resolveReadCursorIdentity produced the same keyPrefix %q for two different member ids", alphaIdentity.keyPrefix)
	}

	target := targetRef{kind: "room", id: "general"}
	if err := advanceReadCursor(alphaIdentity.path, readCursorKey(alphaIdentity, target), "msg_alpha_1"); err != nil {
		t.Fatalf("advanceReadCursor(alpha) error = %v", err)
	}

	betaCursor, err := lookupReadCursor(betaIdentity.path, readCursorKey(betaIdentity, target))
	if err != nil {
		t.Fatalf("lookupReadCursor(beta) error = %v", err)
	}
	if betaCursor != "" {
		t.Fatalf("beta's cursor (via resolved identity) = %q, want empty (isolated from alpha's entry)", betaCursor)
	}
}

// TestReadCursorKeyInjectiveAgainstColonBearingIDs is F1's regression test.
// pkg/protocol/validate.go's ValidateMessageID pattern
// ([A-Za-z0-9][A-Za-z0-9._:-]{0,127}) legally allows ":" in ids, and
// ValidateMemberID explicitly blesses scoped "network:agent" member ids, so
// a colon-bearing room id and a colon-bearing member id can naively
// concatenate to the exact same raw string. This reproduces the reviewer's
// live-verified collision -- agent "bot" reading room "q:room:z" versus
// agent "bot:room:q" reading room "z", both of which used to key to
// "agent:n:bot:room:q:room:z" -- driven through the real production
// pipeline end to end (resolveReadCursorIdentity + parseTarget, exactly as
// runRead calls them), not through hand-pre-escaped test fixtures, so this
// actually exercises readCursorKey's own url.QueryEscape fix rather than
// baking the fix into the test setup.
func TestReadCursorKeyInjectiveAgainstColonBearingIDs(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{Auth: clientconfig.AuthConfig{Mode: "none"}, BaseURL: "http://127.0.0.1:8787", MemberID: "bot", NetworkID: "n"},
		},
	})

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Agent "bot" reading room "q:room:z".
	identityA, err := resolveReadCursorIdentity("", "", clientconfig.AttachmentConfig{MemberID: "bot", NetworkID: "n"}, false, false)
	if err != nil {
		t.Fatalf("resolveReadCursorIdentity(bot) error = %v", err)
	}
	targetA, err := parseTarget("room:q:room:z")
	if err != nil {
		t.Fatalf("parseTarget(room:q:room:z) error = %v", err)
	}

	// Agent "bot:room:q" reading room "z" -- a legal, scoped member id
	// (ValidateMemberID) landing in the same cursor file as the identity
	// above.
	identityB, err := resolveReadCursorIdentity("", "", clientconfig.AttachmentConfig{MemberID: "bot:room:q", NetworkID: "n"}, false, false)
	if err != nil {
		t.Fatalf("resolveReadCursorIdentity(bot:room:q) error = %v", err)
	}
	targetB, err := parseTarget("room:z")
	if err != nil {
		t.Fatalf("parseTarget(room:z) error = %v", err)
	}

	keyA := readCursorKey(identityA, targetA)
	keyB := readCursorKey(identityB, targetB)
	if keyA == keyB {
		t.Fatalf("cursor keys collide: agent %q on target %q and agent %q on target %q both key to %q", "bot", targetA.id, "bot:room:q", targetB.id, keyA)
	}

	if err := advanceReadCursor(identityA.path, keyA, "msg_for_agent_a"); err != nil {
		t.Fatalf("advanceReadCursor(A) error = %v", err)
	}
	if err := advanceReadCursor(identityB.path, keyB, "msg_for_agent_b"); err != nil {
		t.Fatalf("advanceReadCursor(B) error = %v", err)
	}

	gotA, err := lookupReadCursor(identityA.path, keyA)
	if err != nil {
		t.Fatalf("lookupReadCursor(A) error = %v", err)
	}
	if gotA != "msg_for_agent_a" {
		t.Fatalf("A's cursor = %q after B advanced, want unchanged %q (collision: B silently overwrote A)", gotA, "msg_for_agent_a")
	}

	gotB, err := lookupReadCursor(identityB.path, keyB)
	if err != nil {
		t.Fatalf("lookupReadCursor(B) error = %v", err)
	}
	if gotB != "msg_for_agent_b" {
		t.Fatalf("B's cursor = %q, want %q", gotB, "msg_for_agent_b")
	}
}

// TestResolveReadCursorIdentityAgentSitsBesideConfig checks the agent-path
// directory derivation: the cursor file must sit in the same directory
// clientconfig.DiscoverPath resolved config.json from, mirroring where
// register-agent already writes identity.json.
func TestResolveReadCursorIdentityAgentSitsBesideConfig(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
			},
		},
	})

	attachment := clientconfig.AttachmentConfig{MemberID: "alpha", NetworkID: "local_lab"}

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	identity, err := resolveReadCursorIdentity("", "", attachment, false, false)
	if err != nil {
		t.Fatalf("resolveReadCursorIdentity() error = %v", err)
	}

	// clientconfig.DiscoverPath resolves relative to cwd and returns a
	// relative path when given no explicit --config, so identity.path is
	// relative too here; resolve both sides to absolute (via the actual
	// post-chdir cwd, since t.TempDir() on macOS can differ from
	// os.Getwd()'s resolved form by a /var vs /private/var symlink) before
	// comparing.
	gotAbs, err := filepath.Abs(identity.path)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", identity.path, err)
	}
	postChdirCwd := mustGetwd(t)
	want := filepath.Join(postChdirCwd, ".moltnet", readCursorFileName)
	if gotAbs != want {
		t.Fatalf("identity.path = %q (abs %q), want %q", identity.path, gotAbs, want)
	}
	if identity.keyPrefix != "agent:local_lab:alpha" {
		t.Fatalf("identity.keyPrefix = %q, want %q", identity.keyPrefix, "agent:local_lab:alpha")
	}
}

// TestResolveReadCursorIdentityRefusesBaseURLOverride documents the one
// --since-last refusal PLAN.md 7C.2 leaves implicit but must not be silently
// wrong: a raw --base-url override names no local Moltnet config at all, so
// there is nowhere durable to anchor a cursor file.
func TestResolveReadCursorIdentityRefusesBaseURLOverride(t *testing.T) {
	_, err := resolveReadCursorIdentity("", "", clientconfig.AttachmentConfig{}, true, true)
	if err == nil {
		t.Fatal("resolveReadCursorIdentity() with a --base-url override = nil error, want a refusal")
	}
}
