package main

import (
	"context"
	"path/filepath"
	"testing"
)

// The tests in this file pin the full `moltnet init` output — including the
// "Next:" block — for the scenarios PLAN.md's output-UX review called out
// (P3-4): a regression in any printed command (a dropped --id, a
// resurrected `pair invite --network-id local`, a stale aftercare note)
// must fail one of these, not just change prose someone reads past.
//
// Every printed path goes through abbreviateHome, so as long as each test
// sets HOME via t.Setenv and writes under it (or under --dir under it), the
// expected strings below are stable across machines and CI.

// TestRunInitGoldenIDBearerFresh pins `moltnet init --id <id> --bearer`
// against a fresh global-home config: the operator token is generated and
// embedded in one atomic write, so the aftercare says "stored", and the
// Next block's `pair invite` line carries the real, non-default id.
func TestRunInitGoldenIDBearerFresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme", "--bearer"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	want := "" +
		"  Initializing acme\n" +
		"\n" +
		"  ✓ created ~/.moltnet/acme/\n" +
		"  ✓ wrote Moltnet       network: acme · auth: bearer\n" +
		"  ✓ wrote MoltnetNode\n" +
		"  ✓ operator token stored in Moltnet (0600) — local admin\n" +
		"    commands pick it up automatically\n" +
		"\n" +
		"  Next:\n" +
		"    1. moltnet service install --id acme           run it as a service\n" +
		"    2. moltnet relay deploy --id acme              relay on Cloudflare (pair across NAT)\n" +
		"    3. moltnet pair invite --network-id acme --room chat\n" +
		"                                                   invite a friend\n"

	if output != want {
		t.Fatalf("init --id --bearer output mismatch\n got:\n%s\nwant:\n%s", output, want)
	}
}

// TestRunInitGoldenIDWithoutBearer pins `moltnet init --id <id>` (no
// --bearer): auth stays "none", the tip to rerun with --bearer is now
// actually actionable (P1-2), and the Next block still carries the real id.
func TestRunInitGoldenIDWithoutBearer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "beta"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})

	want := "" +
		"  Initializing beta\n" +
		"\n" +
		"  ✓ created ~/.moltnet/beta/\n" +
		"  ✓ wrote Moltnet       network: beta · auth: none\n" +
		"  ✓ wrote MoltnetNode\n" +
		"    tip: rerun with --bearer to generate an operator token for admin access\n" +
		"\n" +
		"  Next:\n" +
		"    1. moltnet service install --id beta           run it as a service\n" +
		"    2. moltnet relay deploy --id beta              relay on Cloudflare (pair across NAT)\n" +
		"    3. moltnet pair invite --network-id beta --room chat\n" +
		"                                                   invite a friend\n"

	if output != want {
		t.Fatalf("init --id output mismatch\n got:\n%s\nwant:\n%s", output, want)
	}
}

// TestRunInitGoldenDirLocalID pins `moltnet init --dir <path>`: the network
// id defaults to "local", so the Next block must NOT print
// `pair invite --network-id local` (P1-4 — it can never succeed) and prints
// re-init guidance instead.
func TestRunInitGoldenDirLocalID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	directory := filepath.Join(home, "customdir")

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--dir", directory}); err != nil {
			t.Fatalf("runInit() --dir error = %v", err)
		}
	})

	want := "" +
		"  Initializing local\n" +
		"\n" +
		"  ✓ created ~/customdir/\n" +
		"  ✓ wrote Moltnet       network: local · auth: none\n" +
		"  ✓ wrote MoltnetNode\n" +
		"    tip: rerun with --bearer to generate an operator token for admin access\n" +
		"\n" +
		"  Next:\n" +
		"    1. moltnet service install --id local          run it as a service\n" +
		"    2. moltnet relay deploy --id local             relay on Cloudflare (pair across NAT)\n" +
		"    3. moltnet init --id <network-id>              re-init with a real network id before pairing\n"

	if output != want {
		t.Fatalf("init --dir output mismatch\n got:\n%s\nwant:\n%s", output, want)
	}
}

// TestRunInitGoldenBearerOnExistingConfig pins `moltnet init --id <id>
// --bearer` rerun against a config that already exists and has no
// auth.tokens: P1-2's real fix adds the operator token to it via the
// plaintext-preserving writeback (not the old no-op "no token was
// generated" note), and the Next block's `pair invite` line still carries
// the real id.
func TestRunInitGoldenBearerOnExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "friend"}); err != nil {
			t.Fatalf("runInit() setup error = %v", err)
		}
	})

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "friend", "--bearer"}); err != nil {
			t.Fatalf("runInit() --bearer rerun error = %v", err)
		}
	})

	want := "" +
		"  Initializing friend\n" +
		"\n" +
		"  ✓ using ~/.moltnet/friend/\n" +
		"  ✓ updated Moltnet     added operator token · auth: bearer\n" +
		"  · MoltnetNode already exists (unchanged)\n" +
		"  ✓ operator token added to ~/.moltnet/friend/Moltnet (0600) — local admin\n" +
		"    commands pick it up automatically\n" +
		"\n" +
		"  Next:\n" +
		"    1. moltnet service install --id friend         run it as a service\n" +
		"    2. moltnet relay deploy --id friend            relay on Cloudflare (pair across NAT)\n" +
		"    3. moltnet pair invite --network-id friend --room chat\n" +
		"                                                   invite a friend\n"

	if output != want {
		t.Fatalf("init --bearer rerun output mismatch\n got:\n%s\nwant:\n%s", output, want)
	}
}
