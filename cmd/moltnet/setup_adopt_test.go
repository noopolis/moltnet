package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	authn "github.com/noopolis/moltnet/internal/auth"
)

// TestSetupAdoptionSkipsInitOnRerun is SETUP.md's adoption headline case:
// re-running setup with the same answers against an already-configured
// network must adopt (skip `init` entirely) rather than re-running it as a
// no-op, and must present the existing rooms read-only.
func TestSetupAdoptionSkipsInitOnRerun(t *testing.T) {
	withInProcessRunChild(t)
	withFakeServiceManager(t, "linux")
	withTestDefaultPort(t)
	shrinkSetupHealthProbeTimeouts(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	withPromptAnswers(t, "", "", "", "", "", "")
	captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("first setup run error = %v", err)
		}
	})

	withPromptAnswers(t, "", "", "", "", "", "")
	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("second (adopting) setup run error = %v", err)
		}
	})
	if strings.Contains(output, "network initialized") {
		t.Fatalf("expected the second run to adopt (skip init entirely), got %q", output)
	}
	if !strings.Contains(output, "existing; unchanged") {
		t.Fatalf("expected the existing rooms to be shown read-only, got %q", output)
	}
	// The service step is idempotent and always re-runs on adopt (SETUP.md:
	// "Service → always re-run the idempotent service install ... never
	// trust IsInstalled").
	if !strings.Contains(output, "service installed") {
		t.Fatalf("expected the service-install step to re-run on adopt, got %q", output)
	}
}

// TestSetupAdoptionReprintsNonLoopbackWarningForWideBoundNetwork pins the
// fix for a real gap: adopting an already wide-bound network used to never
// reprint the non-loopback plaintext-credential warning at all -- SETUP.md's
// adoption table still treats that network as wide-bound, and the exposure
// is exactly as live on the second run as it was on the first, but before
// this fix the operator only ever saw the warning on the run that
// originally created the network.
func TestSetupAdoptionReprintsNonLoopbackWarningForWideBoundNetwork(t *testing.T) {
	withInProcessRunChild(t)
	withFakeServiceManager(t, "linux")
	withTestDefaultPort(t)
	shrinkSetupHealthProbeTimeouts(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// The exact substring NonLoopbackAnonymousWriteWarning/
	// initNonLoopbackWarning both use for a non-loopback bind (bind_warning.go)
	// -- checked specifically rather than a bare "warning:", since the
	// service-install health probe can also print its own, unrelated
	// "warning:" line (a slow-to-answer service) that would otherwise make
	// this assertion pass without the fix this test exists to pin.
	const nonLoopbackWarningSubstring = "is reachable from outside this machine"

	withPromptAnswers(t, "", "", "2", "", "", "") // Q4="2": all network interfaces.
	firstOutput := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("first setup run error = %v", err)
		}
	})
	if !strings.Contains(firstOutput, nonLoopbackWarningSubstring) {
		t.Fatalf("expected the non-loopback warning on the fresh wide-bound run, got %q", firstOutput)
	}

	withPromptAnswers(t, "", "", "2", "", "", "") // same wideBind answer: adopts, does not conflict.
	secondOutput := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("second (adopting) setup run error = %v", err)
		}
	})
	if strings.Contains(secondOutput, "network initialized") {
		t.Fatalf("expected the second run to adopt (skip init entirely), got %q", secondOutput)
	}
	if !strings.Contains(secondOutput, nonLoopbackWarningSubstring) {
		t.Fatalf("expected the non-loopback warning reprinted on the adopting run too, got %q", secondOutput)
	}
}

// TestSetupImmutableConflictRefusesBeforeMutation is SETUP.md's other
// adoption gate: an answer that would change an existing network's bind is
// refused outright, before init or service install ever run again.
func TestSetupImmutableConflictRefusesBeforeMutation(t *testing.T) {
	withInProcessRunChild(t)
	withFakeServiceManager(t, "linux")
	withTestDefaultPort(t)
	shrinkSetupHealthProbeTimeouts(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	withPromptAnswers(t, "", "", "", "", "", "") // creates a loopback-bound network.
	captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("first setup run error = %v", err)
		}
	})

	withPromptAnswers(t, "", "", "2", "", "", "") // Q4="2": all network interfaces, conflicting with the existing loopback bind.
	var runErr error
	output := captureSetupOutput(t, func() {
		runErr = run(context.Background(), []string{"setup"}, "test")
	})
	if runErr == nil {
		t.Fatal("expected an immutable-answer conflict error")
	}
	if !errors.Is(runErr, errSetupImmutableConflict) {
		t.Fatalf("run() error = %v, want it to wrap errSetupImmutableConflict", runErr)
	}
	if strings.Contains(output, "service installed") || strings.Contains(output, "network initialized") {
		t.Fatalf("expected no mutation to have run before the refusal, got %q", output)
	}
}

// TestSetupNonInteractiveRefusesWithOneLiner is SETUP.md's non-TTY rule:
// setup must refuse immediately, with the equivalent commands, rather than
// block on a prompt nothing will ever answer.
func TestSetupNonInteractiveRefusesWithOneLiner(t *testing.T) {
	withTestDefaultPort(t)
	previousInteractive := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = previousInteractive })

	err := run(context.Background(), []string{"setup"}, "test")
	if err == nil {
		t.Fatal("expected setup to refuse when non-interactive")
	}
	if !strings.Contains(err.Error(), "moltnet init") {
		t.Fatalf("error = %q, want it to name the equivalent `moltnet init` command", err.Error())
	}
	if !strings.Contains(err.Error(), "moltnet service install") {
		t.Fatalf("error = %q, want it to name the equivalent `moltnet service install` command", err.Error())
	}
}

// TestSetupNonInteractiveRefusalRespectsProjectFlag proves --project
// changes which one-liner the non-TTY refusal names (SETUP.md: "the scope
// flags are kept, because without them the non-TTY one-liner cannot
// express Q1").
func TestSetupNonInteractiveRefusalRespectsProjectFlag(t *testing.T) {
	withTestDefaultPort(t)
	// Outside any git checkout: this test is about the printed one-liner's
	// shape, not the git-checkout gate (checkSetupProjectGitCheckoutGate,
	// setup.go) — without this, the whole suite already runs with a cwd
	// under cmd/moltnet/, itself inside this very repo's own git checkout,
	// which the gate would otherwise (correctly) refuse first.
	t.Chdir(t.TempDir())
	previousInteractive := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = previousInteractive })

	err := run(context.Background(), []string{"setup", "--project"}, "test")
	if err == nil {
		t.Fatal("expected setup to refuse when non-interactive")
	}
	if !strings.Contains(err.Error(), "--dir .") {
		t.Fatalf("error = %q, want the project-scope `init --dir .` form", err.Error())
	}
	if strings.Contains(err.Error(), "service install") {
		t.Fatalf("error = %q, project scope must never suggest a service install", err.Error())
	}
}

// TestSetupPrintCommandsDoesNotExecute is --print-commands' core contract:
// it prints the resolved argv and writes nothing at all.
func TestSetupPrintCommandsDoesNotExecute(t *testing.T) {
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	withPromptAnswers(t, "", "", "", "", "", "")

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup", "--print-commands"}, "test"); err != nil {
			t.Fatalf("setup --print-commands error = %v", err)
		}
	})

	if _, err := os.Stat(writtenSetupConfigPath(home)); err == nil {
		t.Fatal("expected --print-commands not to write a config, but it did")
	}
	wantInit := fmt.Sprintf("moltnet init --id local --listen 127.0.0.1:%d --room general", setupDefaultPort)
	if !strings.Contains(output, wantInit) {
		t.Fatalf("printed commands = %q, missing %q", output, wantInit)
	}
	// `service install` (like every other post-init child) is threaded the
	// exact resolved --config path, not --id: an --id-based child would
	// resolve ~/.moltnet/<id>/Moltnet itself, which happens to be the same
	// path here, but only because this run's own --id-based discovery agrees
	// with it by coincidence, not because the child was told explicitly.
	wantServiceInstall := "moltnet service install --config " + writtenSetupConfigPath(home)
	if !strings.Contains(output, wantServiceInstall) {
		t.Fatalf("printed commands = %q, missing %q", output, wantServiceInstall)
	}
}

// Q7's two connect branches ("open it up", "I have an invite code") are
// implemented and tested in setup_pair_open_test.go / setup_pair_code_test.go —
// this file used to pin them as "not yet implemented" refusals; that premise
// no longer holds.

func TestSetupHelpPrintsUsage(t *testing.T) {
	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup", "--help"}, "test"); err != nil {
			t.Fatalf("setup --help error = %v", err)
		}
	})
	if !strings.Contains(output, "moltnet setup") {
		t.Fatalf("expected usage output, got %q", output)
	}
}

func TestSetupRejectsBothScopeFlags(t *testing.T) {
	err := run(context.Background(), []string{"setup", "--global", "--project"}, "test")
	if err == nil {
		t.Fatal("expected an error when both --global and --project are given")
	}
}

func TestSetupRejectsPositionalArgs(t *testing.T) {
	err := run(context.Background(), []string{"setup", "extra"}, "test")
	if err == nil {
		t.Fatal("expected an error for a positional argument")
	}
}

// writeSetupBearerTestConfig writes a minimal, valid Moltnet config for
// networkID in auth.mode bearer at path, mirroring
// writeConsoleBearerTestConfig's (console_url_test.go) fixture shape —
// P2-4's adoption-posture gate needs a real, loadable non-open config to
// refuse against, not just a bare "mode: bearer" line.
func writeSetupBearerTestConfig(t *testing.T, path, networkID, listenAddr string) {
	t.Helper()
	body := fmt.Sprintf(`version: moltnet.v1

network:
  id: %q
  name: %q

server:
  listen_addr: %q
  human_ingress: true
  debug_events: false

storage:
  kind: sqlite
  sqlite:
    path: .moltnet/moltnet.db

auth:
  mode: %s
  tokens:
    - id: existing-token
      value: existing-secret-value
      scopes: [observe]

rooms: []
pairings: []
`, networkID, networkID+" Moltnet", listenAddr, authn.ModeBearer)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// TestSetupAdoptionGateBlocksNonOpenPostureBeforeQ4 is P2-4's mutation-proven
// gap closed: checkSetupAdoptionPosture (setup_adopt.go) must actually run
// and refuse the moment an existing bearer-mode config is found — before
// Q4, Q5, Q6, or Q7 are ever asked, let alone any mutation. Deleting the
// `if existing.found { checkSetupAdoptionPosture... }` call at setup.go
// used to leave this fully untested; this drives the real Q1/Q2 answers
// against a real bearer config on disk and asserts the gate's own sentinel.
func TestSetupAdoptionGateBlocksNonOpenPostureBeforeQ4(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := writtenSetupConfigPath(home)
	writeSetupBearerTestConfig(t, configPath, app.DefaultNetworkID, "127.0.0.1:8787")

	withPromptAnswers(t, "", "") // Q1, Q2 only -- nothing past Q2 should ever be read.
	var runErr error
	output := captureSetupOutput(t, func() {
		runErr = run(context.Background(), []string{"setup"}, "test")
	})
	if runErr == nil {
		t.Fatal("expected the adoption posture gate to refuse")
	}
	if !errors.Is(runErr, errSetupNotOpenPosture) {
		t.Fatalf("run() error = %v, want it to wrap errSetupNotOpenPosture", runErr)
	}
	if strings.Contains(output, "network initialized") || strings.Contains(output, "Reachable from?") {
		t.Fatalf("expected the gate to land before Q4 and before any mutation, got %q", output)
	}
}

// TestSetupNonInteractiveRefusalMatchesInteractiveEnterThrough and its
// parseSetupPrintedCommandLines helper live in setup_adopt_refusal_test.go
// -- split out to keep this file under the repo's 400-line limit.
