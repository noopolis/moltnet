package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
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
		"  ✓ operator token stored in Moltnet (0600) — local admin\n" +
		"    commands pick it up automatically\n" +
		"\n" +
		"  next: moltnet service install --id acme          run it as a service\n"
	if verbose != wantVerbose {
		t.Fatalf("init --id --bearer --verbose output mismatch\n got:\n%s\nwant:\n%s", verbose, wantVerbose)
	}
}

// TestRunInitGoldenIDWithoutBearer pins `moltnet init --id <id>` (no
// --bearer): quiet mode is the single checkmark plus next: line; --verbose
// restores auth: none and the actionable tip to rerun with --bearer.
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
		"  ✓ created ~/.moltnet/beta/\n" +
		"  ✓ wrote Moltnet       network: beta · auth: none\n" +
		"  ✓ wrote MoltnetNode\n" +
		"    tip: rerun with --bearer to generate an operator token for admin access\n" +
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
		"  ✓ created ~/customdir2/\n" +
		"  ✓ wrote Moltnet       network: local · auth: none\n" +
		"  ✓ wrote MoltnetNode\n" +
		"    tip: rerun with --bearer to generate an operator token for admin access\n" +
		"\n" +
		"  next: moltnet init --id <network-id>             re-init with a real network id before pairing\n"
	if verbose != wantVerbose {
		t.Fatalf("init --dir --verbose output mismatch\n got:\n%s\nwant:\n%s", verbose, wantVerbose)
	}
}

// TestRunInitGoldenBearerOnExistingConfig pins `moltnet init --id <id>
// --bearer` rerun against a config that already exists and has no
// auth.tokens: P1-2's real fix adds the operator token to it via the
// plaintext-preserving writeback (not the old no-op "no token was
// generated" note); quiet mode still collapses to the single checkmark,
// --verbose still names the "updated Moltnet" line truthfully.
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
		"  ✓ updated Moltnet     added operator token · auth: bearer\n" +
		"  · MoltnetNode already exists (unchanged)\n" +
		"  ✓ operator token added to ~/.moltnet/friend/Moltnet (0600) — local admin\n" +
		"    commands pick it up automatically\n" +
		"\n" +
		"  next: moltnet service install --id friend        run it as a service\n"
	if verbose != wantVerbose {
		t.Fatalf("init --bearer --verbose rerun output mismatch\n got:\n%s\nwant:\n%s", verbose, wantVerbose)
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
