package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
)

// This file covers P0-2 (final-gate review): AddOperatorToken's
// agent_registration cleanup used to live only inside the
// canUpgradeOperatorOnlyOpenConfig branch, guarded by hasExistingTokens.
// Split out of config_writeback_tokens_upgrade_test.go (which already
// covers the sibling operator-only-open case) to keep both files under the
// repo's 400-line limit.

// TestAddOperatorTokenLegacyTokenlessConfigClosesOpenAgentRegistration is
// P0-2's fix. A pre-7.0 legacy config has no auth.tokens[] at all, so
// AddOperatorToken's hasExistingTokens branch -- the only place the
// delete(auth, "agent_registration") call used to live -- never runs for
// it. Before this fix, `init --bearer` against exactly this shape flipped
// auth.mode to "bearer" while an explicit auth.agent_registration: open
// survived verbatim, leaving anonymous POST /v1/agents/register open on a
// network whose owner just believed they had locked it down. This is the
// same defect TestAddOperatorTokenUpgradeClosesOpenAgentRegistration
// (config_writeback_tokens_upgrade_test.go) already covers for the
// operator-only-open branch; that earlier fix was applied per-branch and
// this sibling, tokenless branch was never checked. The fix now lives in
// writeAuthTokensToDoc itself, so every caller that flips auth.mode to
// bearer gets it, not just the one branch a reviewer happened to look at.
//
// Checked functionally against a real internal/app.App on both sides of
// the upgrade (assertAnonymousRegisterStatus, defined alongside the
// sibling test), not just against the decoded Config struct.
func TestAddOperatorTokenLegacyTokenlessConfigClosesOpenAgentRegistration(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "Moltnet")
	fixture := "" +
		"version: moltnet.v1\n" +
		"network:\n  id: acme\n  name: Acme Moltnet\n" +
		"auth:\n" +
		"  mode: open\n" +
		"  agent_registration: open\n" +
		"storage:\n  kind: memory\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	beforeConfig, err := LoadConfigForPath(path, "test")
	if err != nil {
		t.Fatalf("LoadConfigForPath() before upgrade error = %v", err)
	}
	if beforeConfig.Auth.AgentRegistration != authn.AgentRegistrationOpen {
		t.Fatalf("sanity check failed: agent_registration = %q before the upgrade, want open", beforeConfig.Auth.AgentRegistration)
	}
	assertAnonymousRegisterStatus(t, beforeConfig, http.StatusCreated)

	if err := AddOperatorToken(path, newOperatorToken(), newConsoleToken()); err != nil {
		t.Fatalf("AddOperatorToken() error = %v", err)
	}

	afterConfig, err := LoadConfigForPath(path, "test")
	if err != nil {
		t.Fatalf("LoadConfigForPath() after upgrade error = %v", err)
	}
	if afterConfig.Auth.Mode != "bearer" {
		t.Fatalf("auth.mode = %q, want bearer", afterConfig.Auth.Mode)
	}
	if afterConfig.Auth.AgentRegistration == authn.AgentRegistrationOpen {
		t.Fatalf("expected the --bearer upgrade to close agent_registration, got %q", afterConfig.Auth.AgentRegistration)
	}
	assertAnonymousRegisterStatus(t, afterConfig, http.StatusUnauthorized)
}
