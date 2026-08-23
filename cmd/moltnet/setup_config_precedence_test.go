package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/app"
)

// setup_config_precedence_test.go is the regression coverage for the P1
// cross-unit defect this phase's final gate found: `moltnet setup` decides
// on a target network, runs `init` to create it, then drove every remaining
// child (`service install`, `relay deploy`, `pair invite`, `pair invite
// show`, `pair <code>`) by `--id` alone. Two independent things can make an
// `--id`-only child resolve a *different* config than the one setup just
// decided on and printed to the operator:
//
//  1. MOLTNET_CONFIG in the operator's inherited environment wins outright
//     over --id (discoverExplicitConfigPath, internal/app/config_load.go) --
//     TestSetupQ7HaveCodeIgnoresAmbientMOLTNETConfigEnv below.
//  2. Even with no env var at all, a non-empty --id resolves
//     ~/.moltnet/<id>/Moltnet *before* it ever looks at cwd
//     (resolveHomeNetworkConfig, internal/app/config_resolve.go), so a
//     project-scope run whose id also names an existing global network
//     routes every pairing mutation into that global network instead --
//     TestSetupProjectScopeIgnoresSameNamedGlobalNetwork below.
//
// Both are closed by the same fix: every post-init child is now threaded
// the exact --config path this run resolved (setupAnswers.configPath),
// which discoverExplicitConfigPath checks before either --id or
// MOLTNET_CONFIG are ever consulted.

// TestSetupQ7HaveCodeIgnoresAmbientMOLTNETConfigEnv is regression coverage
// for defect (1) above. With MOLTNET_CONFIG exported to an unrelated
// config's path -- exactly the shape a leftover export from a different
// project would take -- a full "have an invite code" setup run must still
// create and pair only the intended (global, id "local") network, and must
// leave the config MOLTNET_CONFIG names completely untouched.
func TestSetupQ7HaveCodeIgnoresAmbientMOLTNETConfigEnv(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// A config for a totally unrelated network, living outside ~/.moltnet
	// entirely.
	decoyPath := filepath.Join(t.TempDir(), "Moltnet")
	writeServiceTestConfig(t, decoyPath)
	decoyBefore, err := os.ReadFile(decoyPath)
	if err != nil {
		t.Fatalf("read decoy config: %v", err)
	}

	t.Setenv("MOLTNET_CONFIG", decoyPath)

	code := buildSetupTestInviteCode(t, time.Now().Add(48*time.Hour))

	// Q1, Q2, Q4, Q5, Q6="2" (no service), Q7="3" (I have a code), then the
	// pasted code.
	withPromptAnswers(t, "", "", "", "", "2", "3", code)

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("setup error = %v", err)
		}
	})
	if !strings.Contains(output, "paired with acme-net") {
		t.Fatalf("expected the completion screen to name the remote network, got %q", output)
	}

	cfg, err := app.LoadFile(writtenSetupConfigPath(home), "")
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if len(cfg.Pairings) != 1 || cfg.Pairings[0].ID != "friend-net" {
		t.Fatalf("cfg.Pairings = %+v, want exactly one pairing named %q -- the pairing landed somewhere else (the MOLTNET_CONFIG-named decoy?) if this is empty", cfg.Pairings, "friend-net")
	}

	decoyAfter, err := os.ReadFile(decoyPath)
	if err != nil {
		t.Fatalf("read decoy config after setup: %v", err)
	}
	if !bytes.Equal(decoyBefore, decoyAfter) {
		t.Fatalf("the config named by ambient MOLTNET_CONFIG was mutated by setup; before=%q after=%q", decoyBefore, decoyAfter)
	}
}

// TestSetupProjectScopeIgnoresSameNamedGlobalNetwork is regression coverage
// for defect (2) above: no environment variable involved at all, just an
// id an existing global network already happens to use. Q2's own default
// answer is "local" (app.DefaultNetworkID), so this is reachable on the
// plain Enter-through path the moment an operator has ever run global setup
// before -- not a contrived edge case.
func TestSetupProjectScopeIgnoresSameNamedGlobalNetwork(t *testing.T) {
	withInProcessRunChild(t)
	withFakeServiceManager(t, "linux")
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// First, a real global network named "local" (Q1 default: on this
	// machine; Q6="2": no service, to keep this setup step trivial; Q7
	// default: not now).
	withPromptAnswers(t, "", "", "", "", "2", "")
	captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("global setup error = %v", err)
		}
	})
	globalPath := writtenSetupConfigPath(home)
	globalBefore, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}

	// Now a *project*-scope run, from an unrelated, unrelated-to-git
	// directory, whose own Q2 default ("local") collides by name with the
	// global network created above. The project's own ./Moltnet does not
	// exist yet, so this is a fresh `init --dir .`, never an adoption of the
	// global one.
	projectDir := t.TempDir()
	t.Chdir(projectDir)

	code := buildSetupTestInviteCode(t, time.Now().Add(48*time.Hour))
	// --project preselects Q1; remaining reads are Q2, Q4, Q5, Q7 (Q6 is
	// skipped entirely for project scope), then the pasted code.
	withPromptAnswers(t, "", "", "", "3", code)

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup", "--project"}, "test"); err != nil {
			t.Fatalf("project setup error = %v", err)
		}
	})
	if !strings.Contains(output, "paired with acme-net") {
		t.Fatalf("expected the completion screen to name the remote network, got %q", output)
	}

	projectPath := filepath.Join(projectDir, "Moltnet")
	projectCfg, err := app.LoadFile(projectPath, "")
	if err != nil {
		t.Fatalf("load project config: %v", err)
	}
	if len(projectCfg.Pairings) != 1 || projectCfg.Pairings[0].ID != "friend-net" {
		t.Fatalf("projectCfg.Pairings = %+v, want exactly one pairing named %q in the *project* config -- it landed in the global \"local\" network instead if this is empty", projectCfg.Pairings, "friend-net")
	}

	globalAfter, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read global config after project setup: %v", err)
	}
	if !bytes.Equal(globalBefore, globalAfter) {
		t.Fatalf("the pre-existing global \"local\" network was mutated by a same-named project-scope setup run; before=%q after=%q", globalBefore, globalAfter)
	}
}
