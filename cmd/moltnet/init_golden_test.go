package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
)

// The tests in this file pin `moltnet init`'s full output for the scenarios
// PLAN.md's output-UX review called out (P3-4): a regression in any printed
// command (a dropped --id, a resurrected `pair invite --network-id local`,
// a stale aftercare note) must fail one of these, not just change prose
// someone reads past. Each scenario is pinned twice — quiet (the default)
// and --verbose — so a --verbose regression that silently stops restoring
// detail fails here too, not just informally.
//
// Every printed path goes through abbreviateHome, so as long as each test
// sets HOME via t.Setenv and writes under it (or under --dir under it), the
// expected strings below are stable across machines and CI.

// TestRunInitGoldenIDBearerFresh pins `moltnet init --id <id> --bearer`
// against a fresh global-home config: quiet mode collapses everything to a
// single "<id> ready" checkmark and one next: line; --verbose restores the
// full per-file breakdown, including the operator-token-stored note.
func TestRunInitGoldenIDBearerFresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	quiet := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--bearer"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})
	wantQuiet := "" +
		"  Initializing acme\n" +
		"\n" +
		"  ✓ acme ready          ~/.moltnet/acme/\n" +
		"\n" +
		"  next: moltnet service install --id acme          run it as a service\n"
	if quiet != wantQuiet {
		t.Fatalf("init --id --bearer quiet output mismatch\n got:\n%s\nwant:\n%s", quiet, wantQuiet)
	}

	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	verbose := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--bearer", "--verbose"}); err != nil {
			t.Fatalf("runInit() --verbose error = %v", err)
		}
	})
	wantVerbose := "" +
		"  Initializing acme\n" +
		"\n" +
		"  ✓ acme ready          ~/.moltnet/acme/\n" +
		"  ✓ created ~/.moltnet/acme/\n" +
		"  ✓ wrote Moltnet       network: acme · auth: bearer\n" +
		"  ✓ wrote MoltnetNode\n" +
		"  ✓ operator + console tokens stored in Moltnet (0600) — full access + read-only console\n" +
		"    commands pick it up automatically\n" +
		"\n" +
		"  next: moltnet service install --id acme          run it as a service\n"
	if verbose != wantVerbose {
		t.Fatalf("init --id --bearer --verbose output mismatch\n got:\n%s\nwant:\n%s", verbose, wantVerbose)
	}
}

// TestRunInitGoldenIDWithoutBearer pins `moltnet init --id <id>` (no
// --bearer): quiet mode is the single checkmark plus next: line; --verbose
// restores auth: open and the actionable tip to rerun with --bearer.
func TestRunInitGoldenIDWithoutBearer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	quiet := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "beta"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})
	wantQuiet := "" +
		"  Initializing beta\n" +
		"\n" +
		"  ✓ beta ready          ~/.moltnet/beta/\n" +
		"  note: local agent self-registration is open — any local agent can claim its own id and receive its own token\n" +
		"\n" +
		"  next: moltnet service install --id beta          run it as a service\n"
	if quiet != wantQuiet {
		t.Fatalf("init --id quiet output mismatch\n got:\n%s\nwant:\n%s", quiet, wantQuiet)
	}

	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	verbose := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "beta", "--verbose"}); err != nil {
			t.Fatalf("runInit() --verbose error = %v", err)
		}
	})
	wantVerbose := "" +
		"  Initializing beta\n" +
		"\n" +
		"  ✓ beta ready          ~/.moltnet/beta/\n" +
		"  note: local agent self-registration is open — any local agent can claim its own id and receive its own token\n" +
		"  ✓ created ~/.moltnet/beta/\n" +
		"  ✓ wrote Moltnet       network: beta · auth: open\n" +
		"  ✓ wrote MoltnetNode\n" +
		"    tip: rerun with --bearer to close open registration and add a console token\n" +
		"\n" +
		"  next: moltnet service install --id beta          run it as a service\n"
	if verbose != wantVerbose {
		t.Fatalf("init --id --verbose output mismatch\n got:\n%s\nwant:\n%s", verbose, wantVerbose)
	}
}

// TestRunInitGoldenDirLocalID pins `moltnet init --dir <path>`: the network
// id defaults to "local", so the next: line must NOT print
// `pair invite --network-id local` (P1-4 — it can never succeed) and prints
// re-init guidance instead, both quiet and --verbose.
func TestRunInitGoldenDirLocalID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	directory := filepath.Join(home, "customdir")

	quiet := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() --dir error = %v", err)
		}
	})
	wantQuiet := "" +
		"  Initializing local\n" +
		"\n" +
		"  ✓ local ready         ~/customdir/\n" +
		"  note: local agent self-registration is open — any local agent can claim its own id and receive its own token\n" +
		"\n" +
		"  next: moltnet init --id <network-id>             re-init with a real network id before pairing\n"
	if quiet != wantQuiet {
		t.Fatalf("init --dir quiet output mismatch\n got:\n%s\nwant:\n%s", quiet, wantQuiet)
	}

	directory2 := filepath.Join(home, "customdir2")
	verbose := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory2, "--verbose"}); err != nil {
			t.Fatalf("runInit() --dir --verbose error = %v", err)
		}
	})
	wantVerbose := "" +
		"  Initializing local\n" +
		"\n" +
		"  ✓ local ready         ~/customdir2/\n" +
		"  note: local agent self-registration is open — any local agent can claim its own id and receive its own token\n" +
		"  ✓ created ~/customdir2/\n" +
		"  ✓ wrote Moltnet       network: local · auth: open\n" +
		"  ✓ wrote MoltnetNode\n" +
		"    tip: rerun with --bearer to close open registration and add a console token\n" +
		"\n" +
		"  next: moltnet init --id <network-id>             re-init with a real network id before pairing\n"
	if verbose != wantVerbose {
		t.Fatalf("init --dir --verbose output mismatch\n got:\n%s\nwant:\n%s", verbose, wantVerbose)
	}
}

// TestRunInitGoldenBearerOnExistingConfig pins `moltnet init --id <id>
// --bearer` rerun against a config that already exists. Since PLAN.md phase
// 7.0, plain `init`'s own setup call below already mints the operator-only
// open config (auth.mode: open, one "operator" token, no "pair" scope), so
// this --bearer rerun exercises 7.0's AddOperatorToken upgrade case
// (config_writeback_tokens.go), not the old "genuinely empty auth.tokens"
// path: it must leave the existing operator token completely untouched —
// same secret, same 3 scopes — and only add the console token and flip
// auth.mode to bearer. Quiet mode still collapses to the single checkmark;
// --verbose names the "updated Moltnet" line and the token-added line
// truthfully for this upgrade shape specifically (added console token, not
// added operator token).
func TestRunInitGoldenBearerOnExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "friend"}); err != nil {
			t.Fatalf("runInit() setup error = %v", err)
		}
	})

	quiet := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "friend", "--bearer"}); err != nil {
			t.Fatalf("runInit() --bearer rerun error = %v", err)
		}
	})
	wantQuiet := "" +
		"  Initializing friend\n" +
		"\n" +
		"  ✓ friend ready        ~/.moltnet/friend/ (existing)\n" +
		"\n" +
		"  next: moltnet service install --id friend        run it as a service\n"
	if quiet != wantQuiet {
		t.Fatalf("init --bearer rerun quiet output mismatch\n got:\n%s\nwant:\n%s", quiet, wantQuiet)
	}

	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "friend"}); err != nil {
			t.Fatalf("runInit() setup error = %v", err)
		}
	})

	configPath := filepath.Join(home2, ".moltnet", "friend", "Moltnet")
	beforeBearer, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath(%q) error = %v", configPath, err)
	}
	if len(beforeBearer.Auth.Tokens) != 1 || beforeBearer.Auth.Tokens[0].ID != "operator" {
		t.Fatalf("expected plain init to have already minted a lone operator token, got %+v", beforeBearer.Auth.Tokens)
	}
	operatorValueBeforeBearer := beforeBearer.Auth.Tokens[0].Value

	verbose := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "friend", "--bearer", "--verbose"}); err != nil {
			t.Fatalf("runInit() --bearer --verbose rerun error = %v", err)
		}
	})
	wantVerbose := "" +
		"  Initializing friend\n" +
		"\n" +
		"  ✓ friend ready        ~/.moltnet/friend/ (existing)\n" +
		"  ✓ using ~/.moltnet/friend/\n" +
		"  ✓ updated Moltnet     added console token · auth: bearer\n" +
		"  · MoltnetNode already exists (unchanged)\n" +
		"  ✓ added a console token to ~/.moltnet/friend/Moltnet (0600) and switched auth.mode to bearer — full access + read-only console\n" +
		"    commands pick it up automatically\n" +
		"\n" +
		"  next: moltnet service install --id friend        run it as a service\n"
	if verbose != wantVerbose {
		t.Fatalf("init --bearer --verbose rerun output mismatch\n got:\n%s\nwant:\n%s", verbose, wantVerbose)
	}

	// Item 5 (now PLAN.md phase 7.0's upgrade guarantee): pin the console
	// token's scopes on this existing-config path specifically
	// (addOperatorTokenWithRollback's "extra" console token, init.go) --
	// `moltnet console` trusts consoleTokenScopes to be exactly [observe]
	// wherever a console token comes from, so a regression that widened it
	// on this path specifically would otherwise go unnoticed by any test.
	// The operator token's scope count (3, never "pair") and secret value
	// must also survive byte-for-byte: this --bearer rerun is an upgrade of
	// a pre-existing operator token, not a re-mint of a new one.
	reloaded, err := app.LoadConfigForPath(configPath, "")
	if err != nil {
		t.Fatalf("LoadConfigForPath(%q) error = %v", configPath, err)
	}
	if len(reloaded.Auth.Tokens) != 2 {
		t.Fatalf("expected exactly 2 auth.tokens[] on the existing-config path, got %d: %+v", len(reloaded.Auth.Tokens), reloaded.Auth.Tokens)
	}
	for _, token := range reloaded.Auth.Tokens {
		switch token.ID {
		case "operator":
			if len(token.Scopes) != 3 {
				t.Fatalf("expected the operator token to keep exactly its original 3 scopes (never gaining \"pair\") on the existing-config path, got %v", token.Scopes)
			}
			if token.Value != operatorValueBeforeBearer {
				t.Fatalf("expected the --bearer upgrade to leave the operator token's secret untouched, got a different value")
			}
		case "console":
			if len(token.Scopes) != 1 || string(token.Scopes[0]) != "observe" {
				t.Fatalf("expected the console token to carry exactly [observe] on the existing-config path, got %v", token.Scopes)
			}
		default:
			t.Fatalf("unexpected token id %q on the existing-config path", token.ID)
		}
	}
}

// TestRunInitGoldenVerboseIsSupersetOfQuiet is the --verbose-parity test the
// redesign calls for: for a representative scenario, every line quiet mode
// prints (the checkmark's outcome text and the next: command) must also
// appear, content-wise, in --verbose's output — --verbose only ever adds
// detail, it never drops or changes the essentials.
func TestRunInitGoldenVerboseIsSupersetOfQuiet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	quiet := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--bearer"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	verbose := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--bearer", "--verbose"}); err != nil {
			t.Fatalf("runInit() --verbose error = %v", err)
		}
	})

	for _, essential := range []string{"acme", "next: moltnet service install --id acme"} {
		if !strings.Contains(quiet, essential) {
			t.Fatalf("quiet output missing its own essential %q:\n%s", essential, quiet)
		}
		if !strings.Contains(verbose, essential) {
			t.Fatalf("verbose output missing quiet essential %q:\n%s", essential, verbose)
		}
	}
	if len(verbose) <= len(quiet) {
		t.Fatalf("expected --verbose output to be strictly longer than quiet output; quiet=%d verbose=%d", len(quiet), len(verbose))
	}
}
