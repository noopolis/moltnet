package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
)

func TestRunInitGlobalHomeRequiresIDNonInteractively(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := runInit(context.Background(), nil); err == nil {
		t.Fatal("expected an error when --id is omitted non-interactively with no --dir")
	}
}

func TestRunInitGlobalHomeWritesUnderNetworkID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// The per-file "wrote <label>" breakdown this test pins is --verbose-only
	// detail under the quiet-by-default redesign (init_summary.go); quiet
	// mode collapses it to a single "<id> ready" checkmark instead.
	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--verbose"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	serverPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	nodePath := filepath.Join(home, ".moltnet", "acme", "MoltnetNode")
	assertFileExists(t, serverPath)
	assertFileExists(t, nodePath)

	contents, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatalf("read %q: %v", serverPath, err)
	}
	if !strings.Contains(string(contents), `id: "acme"`) {
		t.Fatalf("expected network id acme in config, got:\n%s", contents)
	}
	if !strings.Contains(output, "wrote Moltnet") || !strings.Contains(output, "network: acme") {
		t.Fatalf("expected output to mention writing the acme network config, got %q", output)
	}
}

func TestRunInitGlobalHomeRejectsInvalidID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := runInit(context.Background(), []string{"--id", "not a valid id!"}); err == nil {
		t.Fatal("expected an error for an invalid --id")
	}
}

func TestRunInitCustomNameIsUsed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := runInit(context.Background(), []string{"--id", "acme", "--name", "Acme Friends"}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	serverPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	contents, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatalf("read %q: %v", serverPath, err)
	}
	if !strings.Contains(string(contents), `name: "Acme Friends"`) {
		t.Fatalf("expected custom network name in config, got:\n%s", contents)
	}
}

func TestRunInitBearerStoresTokenWithoutEverPrintingIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// --verbose: the "operator token stored" line is --verbose-only detail
	// under the quiet-by-default redesign.
	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--bearer", "--verbose"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	if !strings.Contains(output, "operator + console tokens stored in Moltnet") {
		t.Fatalf("expected a note that both tokens were stored, got %q", output)
	}
	if !strings.Contains(output, "auth: bearer") {
		t.Fatalf("expected the auth mode summary in output, got %q", output)
	}

	serverPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	contents, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatalf("read %q: %v", serverPath, err)
	}
	if !strings.Contains(string(contents), "mode: bearer") {
		t.Fatalf("expected auth.mode: bearer in config, got:\n%s", contents)
	}
	if !strings.Contains(string(contents), "scopes: [observe, write, admin, pair]") {
		t.Fatalf("expected operator token scopes in config, got:\n%s", contents)
	}
	if !strings.Contains(string(contents), "id: console") || !strings.Contains(string(contents), "scopes: [observe]") {
		t.Fatalf("expected a console token scoped to exactly [observe] in config, got:\n%s", contents)
	}

	// The reload check backs item 5's "reload asserts real plaintext
	// values, distinct, correct scopes": load the config the same way the
	// server does and inspect the two auth.tokens[] entries directly,
	// rather than only pattern-matching the raw YAML.
	reloaded, err := app.LoadConfigForPath(serverPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath(%q) error = %v", serverPath, err)
	}
	if len(reloaded.Auth.Tokens) != 2 {
		t.Fatalf("expected exactly 2 auth.tokens[], got %d: %+v", len(reloaded.Auth.Tokens), reloaded.Auth.Tokens)
	}
	var operatorValue, consoleValue string
	for _, token := range reloaded.Auth.Tokens {
		switch token.ID {
		case "operator":
			operatorValue = token.Value
			if len(token.Scopes) != 4 {
				t.Fatalf("expected the operator token to keep all 4 scopes, got %v", token.Scopes)
			}
		case "console":
			consoleValue = token.Value
			if len(token.Scopes) != 1 || string(token.Scopes[0]) != "observe" {
				t.Fatalf("expected the console token to carry exactly [observe], got %v", token.Scopes)
			}
		default:
			t.Fatalf("unexpected token id %q", token.ID)
		}
	}
	if operatorValue == "" || consoleValue == "" {
		t.Fatalf("expected both tokens to have non-empty values, got operator=%q console=%q", operatorValue, consoleValue)
	}
	if operatorValue == consoleValue {
		t.Fatalf("expected the operator and console tokens to be distinct values, both were %q", operatorValue)
	}

	// The token must never be printed (PLAN.md: "NEVER print the token
	// value"): extract every generated value from disk and confirm none of
	// them are present in the captured stdout.
	matches := regexp.MustCompile(`value: "([^"]+)"`).FindAllStringSubmatch(string(contents), -1)
	if len(matches) != 2 {
		t.Fatalf("expected 2 generated token values in config, got %d:\n%s", len(matches), contents)
	}
	for _, match := range matches {
		if strings.Contains(output, match[1]) {
			t.Fatalf("expected no token to ever be printed, but found one in output %q", output)
		}
	}
}

func TestRunInitWithoutBearerShowsOpenAuthAndBearerTip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// --verbose: "auth: open" (PLAN.md phase 6a's new non-bearer default —
	// templates.go's defaultMoltnetConfig now writes auth.mode: open) and the
	// --bearer tip are --verbose-only detail under the quiet-by-default
	// redesign.
	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--verbose"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	if !strings.Contains(output, "auth: open") {
		t.Fatalf("expected auth: open in output, got %q", output)
	}
	if !strings.Contains(output, "--bearer") {
		t.Fatalf("expected a tip suggesting --bearer, got %q", output)
	}
	if strings.Contains(output, "operator token stored") {
		t.Fatalf("expected no operator token note without --bearer, got %q", output)
	}
}

// TestRunInitBearerOnSymlinkedConfigDegradesGracefully covers `moltnet init
// --bearer` against an existing Moltnet config that is a symlink: it must
// not hard-fail with no summary (the old behavior, which also left
// MoltnetNode written but the command exiting non-zero with nothing printed
// to explain why), and it must never write through the symlink. It should
// instead degrade the same way the "auth.tokens already has entries" case
// does — skip adding the token, still print the full aftercare summary.
func TestRunInitBearerOnSymlinkedConfigDegradesGracefully(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	networkDir := filepath.Join(home, ".moltnet", "acme")
	if err := os.MkdirAll(networkDir, 0o700); err != nil {
		t.Fatalf("mkdir network dir: %v", err)
	}

	realConfigPath := filepath.Join(t.TempDir(), "real-Moltnet")
	realContents := "" +
		"version: moltnet.v1\n" +
		"network:\n" +
		"  id: acme\n" +
		"  name: Acme Moltnet\n"
	if err := os.WriteFile(realConfigPath, []byte(realContents), 0o600); err != nil {
		t.Fatalf("write real config: %v", err)
	}

	symlinkPath := filepath.Join(networkDir, "Moltnet")
	if err := os.Symlink(realConfigPath, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--bearer"}); err != nil {
			t.Fatalf("runInit() error = %v, want a graceful degrade instead of a hard failure", err)
		}
	})

	// The bearerAddErr note is a real, actionable failure — it prints
	// unconditionally, quiet or --verbose (init_summary.go).
	if !strings.Contains(output, "is a symlink") {
		t.Fatalf("expected a note about the symlinked config, got %q", output)
	}

	// The per-file "wrote MoltnetNode" checkmark is --verbose-only detail
	// under the quiet-by-default redesign; rerun with --verbose to confirm
	// MoltnetNode was still actually written.
	verboseOutput := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--bearer", "--verbose"}); err != nil {
			t.Fatalf("runInit() --verbose error = %v", err)
		}
	})
	if !strings.Contains(verboseOutput, "MoltnetNode already exists") {
		t.Fatalf("expected MoltnetNode to still exist from the first run, got %q", verboseOutput)
	}

	after, err := os.ReadFile(realConfigPath)
	if err != nil {
		t.Fatalf("read real config after: %v", err)
	}
	if string(after) != realContents {
		t.Fatalf("expected init to never write through the symlink, before:\n%s\nafter:\n%s", realContents, after)
	}
}

func TestRunInitDirOptsOutOfGlobalHomeAndIDPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	directory := t.TempDir()

	if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
		t.Fatalf("runInit() --dir error = %v", err)
	}

	assertFileExists(t, filepath.Join(directory, "Moltnet"))
	// The global home must stay untouched.
	if _, err := os.Stat(filepath.Join(home, ".moltnet")); err == nil {
		t.Fatal("expected ~/.moltnet to remain unwritten when --dir is given")
	}
}

func TestRunInitPositionalPathIsDeprecatedButWorks(t *testing.T) {
	directory := t.TempDir()

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{directory}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	if !strings.Contains(output, "deprecated") {
		t.Fatalf("expected a deprecation note, got %q", output)
	}
	assertFileExists(t, filepath.Join(directory, "Moltnet"))
}

func TestRunInitRejectsPositionalAndDirTogether(t *testing.T) {
	directory := t.TempDir()

	if err := runInit(context.Background(), []string{directory, "--dir", directory}); err == nil {
		t.Fatal("expected an error when both a positional path and --dir are given")
	}
}

func TestRunInitWarnsOnCheckoutMarkers(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	if !strings.Contains(output, "warning") || !strings.Contains(output, "go.mod") {
		t.Fatalf("expected a checkout warning mentioning go.mod, got %q", output)
	}
}

func TestCheckoutWarningEmptyForCleanDirectory(t *testing.T) {
	directory := t.TempDir()
	if warning := checkoutWarning(directory); warning != "" {
		t.Fatalf("checkoutWarning() = %q, want empty", warning)
	}
}

func TestDefaultNetworkNameForID(t *testing.T) {
	if got := defaultNetworkNameForID("acme-friends"); got != "Acme Friends Moltnet" {
		t.Fatalf("defaultNetworkNameForID() = %q, want %q", got, "Acme Friends Moltnet")
	}
}
