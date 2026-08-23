package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunInitListenWritesAddress covers `moltnet init --listen`'s core job
// (PLAN.md's setup-wizard spec, "The init gap"): before this flag existed,
// nothing set server.listen_addr away from its 127.0.0.1:8787 default, and
// no writeback touched it after the fact.
func TestRunInitListenWritesAddress(t *testing.T) {
	directory := t.TempDir()

	if err := runInit(context.Background(), []string{"--dir", directory, "--listen", "127.0.0.1:8788"}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(directory, "Moltnet"))
	if err != nil {
		t.Fatalf("read Moltnet: %v", err)
	}
	if !strings.Contains(string(contents), `listen_addr: "127.0.0.1:8788"`) {
		t.Fatalf("expected listen_addr 127.0.0.1:8788 in config, got:\n%s", contents)
	}
}

// TestRunInitListenDefaultsToLoopback covers the negative space: omitting
// --listen must keep writing the original loopback-only default, unchanged.
func TestRunInitListenDefaultsToLoopback(t *testing.T) {
	directory := t.TempDir()

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	contents, err := os.ReadFile(filepath.Join(directory, "Moltnet"))
	if err != nil {
		t.Fatalf("read Moltnet: %v", err)
	}
	if !strings.Contains(string(contents), `listen_addr: "127.0.0.1:8787"`) {
		t.Fatalf("expected the default loopback listen_addr, got:\n%s", contents)
	}
	if strings.Contains(output, "warning:") {
		t.Fatalf("expected no bind warning for the default loopback bind, got %q", output)
	}
}

// TestRunInitListenRejectsMalformedAddress covers syntax validation: before
// this, config_file.go validated no listen_addr syntax at all, so a
// malformed value would have ridden straight into the config with no
// complaint until some later `moltnet start` failed to bind.
func TestRunInitListenRejectsMalformedAddress(t *testing.T) {
	directory := t.TempDir()

	err := runInit(context.Background(), []string{"--dir", directory, "--listen", "not-an-address"})
	if err == nil {
		t.Fatal("expected an error for a malformed --listen value")
	}
	if !strings.Contains(err.Error(), "--listen") {
		t.Fatalf("expected the error to name --listen, got %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(directory, "Moltnet")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no Moltnet to be written on a rejected --listen, stat error = %v", statErr)
	}
}

// TestRunInitListenWarnsAtAuthoringTime covers PLAN.md's "Q4 — the warning
// is computed, never static" and its authoring-time amendment: before this,
// a non-loopback bind was only ever flagged at server start or `moltnet
// validate`, neither of which a scripter driving --listen non-interactively
// ever sees.
func TestRunInitListenWarnsAtAuthoringTime(t *testing.T) {
	directory := t.TempDir()

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory, "--listen", "0.0.0.0:8787"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	if !strings.Contains(output, "warning:") {
		t.Fatalf("expected a warning: line for a non-loopback --listen, got %q", output)
	}
	if !strings.Contains(output, "0.0.0.0:8787") {
		t.Fatalf("expected the warning to name the listen_addr, got %q", output)
	}
}

// TestRunInitListenWarningReplacesInertAdvice is the direct regression
// guard for the one negative requirement in PLAN.md's spec: the
// authoring-time warning must not repeat
// app.NonLoopbackAnonymousWriteWarning's stock "Set auth.agent_registration
// to \"token\" or \"disabled\"" sentence, since config_load.go's
// mergeFileConfig makes that advice inert whenever auth.mode is "open" --
// the only mode plain `init` ever writes. It must instead say to bind
// loopback or change auth.mode and configure credentials and room
// membership together.
func TestRunInitListenWarningReplacesInertAdvice(t *testing.T) {
	directory := t.TempDir()

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory, "--listen", "0.0.0.0:8787"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	if strings.Contains(output, `Set auth.agent_registration to "token" or "disabled"`) {
		t.Fatalf("expected the inert agent_registration advice to be stripped, got %q", output)
	}
	if !strings.Contains(output, "change auth.mode and configure credentials and room membership together") {
		t.Fatalf("expected the corrected authoring-time advice, got %q", output)
	}
}

// TestRunInitListenBearerNoWarning covers the bearer half of the same
// authoring-time check: app.NonLoopbackAnonymousWriteWarning never fires
// for a bearer config (auth.mode: bearer sets neither mode: none nor
// agent_registration: open), so a non-loopback --bearer --listen must stay
// quiet, exactly like it already does at server start and `moltnet
// validate`.
func TestRunInitListenBearerNoWarning(t *testing.T) {
	directory := t.TempDir()

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory, "--bearer", "--listen", "0.0.0.0:8787"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	if strings.Contains(output, "warning:") {
		t.Fatalf("expected no bind warning for a non-loopback --bearer config, got %q", output)
	}
}

// TestRunInitListenThreadsIntoNodeScaffold covers PLAN.md's "port selection"
// consequence: defaultMoltnetNodeConfig used to hardcode
// base_url: http://127.0.0.1:8787 regardless of --listen, so a network
// started on a non-default port (the exact scenario the port-preflight
// exists for -- 8787 already taken) got a MoltnetNode scaffold pointed at
// whatever else owns 8787.
func TestRunInitListenThreadsIntoNodeScaffold(t *testing.T) {
	directory := t.TempDir()

	if err := runInit(context.Background(), []string{"--dir", directory, "--listen", "127.0.0.1:8788"}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(directory, "MoltnetNode"))
	if err != nil {
		t.Fatalf("read MoltnetNode: %v", err)
	}
	if !strings.Contains(string(contents), `base_url: "http://127.0.0.1:8788"`) {
		t.Fatalf("expected MoltnetNode base_url to follow --listen, got:\n%s", contents)
	}
}

// TestRunInitListenWildcardThreadsAsLoopback covers the other half of the
// same consequence: a wildcard bind (":8787", "0.0.0.0:8787") is not itself
// dialable, so the scaffold must translate it to a loopback address while
// keeping the chosen port -- exactly what consoleBaseURL already does for
// `moltnet console`, reused here rather than duplicated.
func TestRunInitListenWildcardThreadsAsLoopback(t *testing.T) {
	directory := t.TempDir()

	if err := runInit(context.Background(), []string{"--dir", directory, "--listen", "0.0.0.0:8788"}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(directory, "MoltnetNode"))
	if err != nil {
		t.Fatalf("read MoltnetNode: %v", err)
	}
	if !strings.Contains(string(contents), `base_url: "http://127.0.0.1:8788"`) {
		t.Fatalf("expected a wildcard --listen to translate to a loopback base_url keeping the port, got:\n%s", contents)
	}
}

// TestRunInitListenAndRoomIgnoredNoteOnExistingConfig pins finding 2:
// --listen/--room silently no-op once serverPath already exists
// (writeFileIfMissing never renders or writes anything on that path), and
// before this fix nothing told the operator so -- exit 0, no note, no
// warning, config unchanged. A second `init --listen ... --room ...` against
// an already-initialized directory must now print a note naming both
// ignored flags, the config path, and `moltnet room create` as the actual
// way to add a room -- and the config on disk must be provably unchanged.
func TestRunInitListenAndRoomIgnoredNoteOnExistingConfig(t *testing.T) {
	directory := t.TempDir()

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() setup error = %v", err)
		}
	})

	configPath := filepath.Join(directory, "Moltnet")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read Moltnet before rerun: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory, "--listen", "0.0.0.0:9922", "--room", "ops"}); err != nil {
			t.Fatalf("runInit() rerun error = %v", err)
		}
	})

	if !strings.Contains(output, "note:") {
		t.Fatalf("expected a note: line reporting the ignored flags, got %q", output)
	}
	if !strings.Contains(output, "--listen") || !strings.Contains(output, "--room") {
		t.Fatalf("expected the note to name both --listen and --room, got %q", output)
	}
	if !strings.Contains(output, "moltnet room create") {
		t.Fatalf("expected the note to point at `moltnet room create` for rooms, got %q", output)
	}
	// Tightened to configPath alone (round 3's fix for a mutation-proven
	// gap): "Moltnet" appears in nearly all of this command's output
	// (banner, summary lines) regardless of what this note itself says, so
	// an OR against that bare substring passed even when configPath was
	// never mentioned anywhere in the note.
	if !strings.Contains(output, configPath) {
		t.Fatalf("expected the note to name the config path, got %q", output)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read Moltnet after rerun: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("expected the existing config to be left byte-for-byte unchanged\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !strings.Contains(string(after), `listen_addr: "127.0.0.1:8787"`) {
		t.Fatalf("expected listen_addr to remain the original default, got:\n%s", after)
	}
	if strings.Contains(string(after), `id: ops`) {
		t.Fatalf("expected no \"ops\" room to have been authored, got:\n%s", after)
	}
}

// TestRunInitListenNoNoteWithoutFlagsOnExistingConfig is the negative half:
// a plain rerun with neither --listen nor --room must stay exactly as quiet
// as before this fix -- the note only fires when one of those two flags was
// actually given against an existing config.
func TestRunInitListenNoNoteWithoutFlagsOnExistingConfig(t *testing.T) {
	directory := t.TempDir()

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() setup error = %v", err)
		}
	})

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() rerun error = %v", err)
		}
	})
	if strings.Contains(output, "note:") {
		t.Fatalf("expected no ignored-flags note on a plain rerun with no --listen/--room, got %q", output)
	}
}

// TestRunInitNodeBaseURLFollowsExistingConfigOnResume pins finding 3: the
// MoltnetNode scaffold's base_url must be derived from the *existing*
// config's own listen_addr when one is already there, not from --listen's
// resolved flag value (defaultInitListenAddr when --listen is not given on
// the resume run). Reproduction: a network is authored on a non-default
// port, its MoltnetNode scaffold is deleted (as if lost or never written),
// and `moltnet init` is rerun with no --listen at all -- the regenerated
// scaffold must still point at the network's real port, not silently fall
// back to 127.0.0.1:8787.
func TestRunInitNodeBaseURLFollowsExistingConfigOnResume(t *testing.T) {
	directory := t.TempDir()

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory, "--listen", "127.0.0.1:9911"}); err != nil {
			t.Fatalf("runInit() setup error = %v", err)
		}
	})

	nodePath := filepath.Join(directory, "MoltnetNode")
	if err := os.Remove(nodePath); err != nil {
		t.Fatalf("remove MoltnetNode: %v", err)
	}

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() resume error = %v", err)
		}
	})

	contents, err := os.ReadFile(nodePath)
	if err != nil {
		t.Fatalf("read regenerated MoltnetNode: %v", err)
	}
	if !strings.Contains(string(contents), `base_url: "http://127.0.0.1:9911"`) {
		t.Fatalf("expected the regenerated MoltnetNode to follow the existing config's own listen_addr (9911), got:\n%s", contents)
	}
	if strings.Contains(string(contents), "8787") {
		t.Fatalf("expected no trace of the unrelated --listen default (8787), got:\n%s", contents)
	}
}

// TestRunInitDoesNotWriteWrongNodeScaffoldOnUndecodableExistingConfig pins
// round 3's fix: applyExistingServerConfigTokens (the call that actually
// surfaces a load error against an existing, undecodable config) now runs
// before runInit ever writes MoltnetNode. Before this fix, an undecodable
// existing config with a deleted MoltnetNode wrote a fresh scaffold using
// existingServerListenAddr's silent fallback (defaultInitListenAddr, not
// whatever listen_addr the broken config actually names) *before* runInit
// ever failed on the same undecodable config -- and a later, repaired
// rerun would never rewrite it, since writeFileIfMissing no-ops once
// MoltnetNode already exists. This reproduces the live-verified repro:
// corrupt the config, delete MoltnetNode, rerun, and require both that
// runInit fails and that no wrong scaffold was left behind for it to fail
// alongside.
func TestRunInitDoesNotWriteWrongNodeScaffoldOnUndecodableExistingConfig(t *testing.T) {
	directory := t.TempDir()

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory, "--listen", "127.0.0.1:9911"}); err != nil {
			t.Fatalf("runInit() setup error = %v", err)
		}
	})

	configPath := filepath.Join(directory, "Moltnet")
	if err := os.WriteFile(configPath, []byte("not: valid: yaml: : : broken"), 0o600); err != nil {
		t.Fatalf("corrupt Moltnet config: %v", err)
	}
	nodePath := filepath.Join(directory, "MoltnetNode")
	if err := os.Remove(nodePath); err != nil {
		t.Fatalf("remove MoltnetNode: %v", err)
	}

	var runErr error
	captureStdout(t, func() {
		runErr = runInit(context.Background(), []string{"--dir", directory})
	})
	if runErr == nil {
		t.Fatal("runInit() error = nil, want an error for an undecodable existing config")
	}

	if _, statErr := os.Stat(nodePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no MoltnetNode scaffold to have been written before the failure, stat err = %v", statErr)
	}
}
