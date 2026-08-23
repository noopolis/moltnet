package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
)

// writtenSetupConfigPathFor is writtenSetupConfigPath's counterpart for a
// non-default network id: `pair invite` refuses outright against the
// default network id (pair_invite.go), and planSetupPairOpenUp now refuses
// even earlier, before ever contacting Cloudflare (P1,
// errSetupPairDefaultNetworkID) — so every test in this file that actually
// runs the "open it up" branch, for real or under --print-commands, must
// type a distinct one at Q2.
// TestSetupQ7OpenUpDefaultNetworkIDRefusesBeforeMutation below is the one
// exception, and exercises the default id deliberately.
func writtenSetupConfigPathFor(home, id string) string {
	return filepath.Join(home, ".moltnet", id, "Moltnet")
}

// TestSetupQ7OpenUpUnclaimedEndToEnd is Unit 5's headline case: "open it up"
// against a Cloudflare account with no existing workers.dev claim, and a
// real local network (init, relay deploy, pair invite, pair invite show all
// run for real, in-process, against a real temp-dir config — only
// Cloudflare is faked). It proves the invite is displayed and that it came
// from `pair invite show`, not from `pair invite`'s own (suppressed)
// stdout.
func TestSetupQ7OpenUpUnclaimedEndToEnd(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLOUDFLARE_API_TOKEN", "cf-token-123")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "cf-token-123", "acme")
	withRelayDeployFakeCloudflare(t, fake)

	// Q1, Q2, Q4, Q5, Q6="2" (no service, avoids needing a health probe),
	// Q7="2" (open it up), Q7.2 subdomain name "acme", permanence confirm
	// "y". Q7.1 is never read: Q5's default single room skips it.
	withPromptAnswers(t, "", "acme-net", "", "", "2", "2", "acme", "y")

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("setup error = %v", err)
		}
	})

	if !strings.Contains(output, "moltnet pair '") {
		t.Fatalf("expected the retrieved invite code to be displayed, got %q", output)
	}
	if got := fake.claimAttemptCount(); got != 1 {
		t.Fatalf("claimAttemptCount() = %d, want exactly 1 (the non-interactive --subdomain claim)", got)
	}

	cfg, err := app.LoadFile(writtenSetupConfigPathFor(home, "acme-net"), "")
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if len(cfg.Pairings) != 1 {
		t.Fatalf("cfg.Pairings = %+v, want exactly one pairing written by `pair invite`", cfg.Pairings)
	}

	// The displayed code must be the exact one `pair invite show` returns
	// for this pairing id, not some other value the wizard reconstructed —
	// pull it out of the summary line and cross-check against the receipt
	// `pair invite` itself wrote to disk (app.LoadInviteReceipt), byte for
	// byte. A truncated or otherwise mangled display would still pass a bare
	// `code != ""` check, so this compares the full value.
	code := extractInviteCode(t, output)
	if code == "" {
		t.Fatalf("could not find a `moltnet pair '<code>'` line in output %q", output)
	}
	receipt, ok, err := app.LoadInviteReceipt(writtenSetupConfigPathFor(home, "acme-net"), cfg.Pairings[0].ID)
	if err != nil {
		t.Fatalf("LoadInviteReceipt: %v", err)
	}
	if !ok {
		t.Fatalf("expected an invite receipt for pairing %q, found none", cfg.Pairings[0].ID)
	}
	if code != receipt.Code {
		t.Fatalf("displayed invite code = %q, want byte-for-byte the stored receipt's code %q", code, receipt.Code)
	}

	// printSetupOpenUpSummary's membership command still names the friend's
	// agent id as a placeholder (SETUP.md point 6: `pair show` cannot be
	// called against a peer that has not answered the invite yet). Only
	// pair_aftercare_test.go / pair_restart_test.go cover this placeholder
	// today, and only for `pair invite`'s own aftercare — not the setup
	// wizard's completion screen, which renders it through a separate code
	// path (printSetupPairingSummary, setup_pair_summary.go).
	if !strings.Contains(output, "<their-agent-id>") {
		t.Fatalf("expected the completion screen's membership command to still show the <their-agent-id> placeholder, got %q", output)
	}
}

// TestSetupQ7OpenUpDefaultNetworkIDRefusesBeforeMutation is P1's headline
// regression test: Enter through Q1/Q2 (leaving the network id at its
// default), Q6 = no service, Q7 = "open it up", a workers.dev subdomain
// name, and the permanence confirm "y" — the exact live-verified repro that
// used to print "✓ relay deployed", permanently burn the Cloudflare
// account's one-ever workers.dev claim, write relay.json, and only then fail
// on `pair invite`'s own default-network-id refusal (pair_invite.go). It
// must now refuse before ever contacting Cloudflare: no claim attempt, no
// config written at all.
func TestSetupQ7OpenUpDefaultNetworkIDRefusesBeforeMutation(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLOUDFLARE_API_TOKEN", "cf-token-123")

	fake := startFakeCloudflareCLIServerUnclaimed(t, "cf-token-123", "acme")
	withRelayDeployFakeCloudflare(t, fake)

	// Identical to TestSetupQ7OpenUpUnclaimedEndToEnd's own answers, except
	// Q2 is left blank instead of typed as "acme-net" — the one dodge this
	// test exists to stop making.
	withPromptAnswers(t, "", "", "", "", "2", "2", "acme", "y")

	var runErr error
	output := captureSetupOutput(t, func() {
		runErr = run(context.Background(), []string{"setup"}, "test")
	})
	if runErr == nil {
		t.Fatal("expected the default-network-id refusal, got a successful run")
	}
	if !errors.Is(runErr, errSetupPairDefaultNetworkID) {
		t.Fatalf("run() error = %v, want it to wrap errSetupPairDefaultNetworkID", runErr)
	}
	if strings.Contains(output, "relay deployed") {
		t.Fatalf("expected the refusal to land before `relay deploy` ever ran, got %q", output)
	}
	if got := fake.claimAttemptCount(); got != 0 {
		t.Fatalf("claimAttemptCount() = %d, want 0 — the permanent claim must never be attempted", got)
	}
	if _, err := app.LoadFile(writtenSetupConfigPath(home), ""); err == nil {
		t.Fatal("expected no config to have been written before the refusal")
	}
}

// TestSetupQ7OpenUpAdoptsExistingClaimSilently covers "existing claim ->
// adopt silently": Q7.2 must never be asked at all when the Cloudflare
// account already has a workers.dev subdomain, and the resulting `relay
// deploy` invocation must never carry --subdomain.
func TestSetupQ7OpenUpAdoptsExistingClaimSilently(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLOUDFLARE_API_TOKEN", "cf-token-123")

	fake := startFakeCloudflareCLIServer(t, "cf-token-123") // already claimed "acme"
	withRelayDeployFakeCloudflare(t, fake)

	// No subdomain name / permanence-confirm answers are provided: if Q7.2
	// were asked anyway, the next read would consume the wrong answer and
	// this would fail with a validation error instead of succeeding.
	withPromptAnswers(t, "", "acme-net", "", "", "2", "2")

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("setup error = %v", err)
		}
	})

	if strings.Contains(output, "workers.dev subdomain") {
		t.Fatalf("expected Q7.2 to be skipped entirely for an already-claimed account, got %q", output)
	}
	if got := fake.claimAttemptCount(); got != 0 {
		t.Fatalf("claimAttemptCount() = %d, want 0 (an already-claimed account must never be re-claimed)", got)
	}
	if !strings.Contains(output, "moltnet pair '") {
		t.Fatalf("expected the invite to still be displayed, got %q", output)
	}
}

// TestSetupQ7OpenUpAsksShareRoomOnlyWithMultipleRooms covers Q7.1: it must
// be asked only when Q5 produced more than one room, and the chosen room —
// not the first one by default — is what `pair invite --room` names.
func TestSetupQ7OpenUpAsksShareRoomOnlyWithMultipleRooms(t *testing.T) {
	withInProcessRunChild(t)
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLOUDFLARE_API_TOKEN", "cf-token-123")

	fake := startFakeCloudflareCLIServer(t, "cf-token-123") // already claimed; Q7.2 skipped
	withRelayDeployFakeCloudflare(t, fake)

	// Q5="3" (add more rooms) + "dev" -> rooms = [general, dev]. Q6="2" (no
	// service). Q7="2" (open it up). Q7.1 then asks which room to share:
	// "2" picks "dev", the second option.
	withPromptAnswers(t, "", "acme-net", "", "3", "dev", "2", "2", "2")

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup"}, "test"); err != nil {
			t.Fatalf("setup error = %v", err)
		}
	})

	if !strings.Contains(output, "Which room to share?") {
		t.Fatalf("expected Q7.1 to be asked with more than one room, got %q", output)
	}

	cfg, err := app.LoadFile(writtenSetupConfigPathFor(home, "acme-net"), "")
	if err != nil {
		t.Fatalf("load written config: %v", err)
	}
	if len(cfg.Rooms) != 2 {
		t.Fatalf("cfg.Rooms = %+v, want both \"general\" and \"dev\" to still exist", cfg.Rooms)
	}
	if len(cfg.Pairings) != 1 {
		t.Fatalf("cfg.Pairings = %+v, want exactly one pairing", cfg.Pairings)
	}
	pairingID := cfg.Pairings[0].ID

	var generalFederated, devFederated bool
	for _, room := range cfg.Rooms {
		if room.Federation == nil {
			continue
		}
		for _, member := range room.Federation.Pairings {
			if member == pairingID {
				if room.ID == "general" {
					generalFederated = true
				}
				if room.ID == "dev" {
					devFederated = true
				}
			}
		}
	}
	if generalFederated {
		t.Fatal("expected \"general\" (not chosen at Q7.1) to carry no federation entry for this pairing")
	}
	if !devFederated {
		t.Fatal("expected \"dev\" (chosen at Q7.1) to carry a federation entry for this pairing")
	}
}

// TestSetupPairOpenUpPrintCommandsNeverLeaksToken covers "the Cloudflare
// token never appears in argv or in --print-commands output": the printed
// `relay deploy` command must carry a named placeholder, never the literal
// token, and the plan must still name the retrieval step.
func TestSetupPairOpenUpPrintCommandsNeverLeaksToken(t *testing.T) {
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLOUDFLARE_API_TOKEN", "super-secret-cf-token")

	fake := startFakeCloudflareCLIServer(t, "super-secret-cf-token") // already claimed; Q7.2 skipped
	withRelayDeployFakeCloudflare(t, fake)

	// Q2 must be a distinct id: planSetupPairOpenUp now refuses the default
	// id even under --print-commands (P1) — this test's own subject is the
	// token-redaction rule, not that refusal, so it types "acme-net" like
	// every other real "open it up" test in this file.
	withPromptAnswers(t, "", "acme-net", "", "", "2", "2")

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup", "--print-commands"}, "test"); err != nil {
			t.Fatalf("setup --print-commands error = %v", err)
		}
	})

	if strings.Contains(output, "super-secret-cf-token") {
		t.Fatalf("printed commands leaked the literal Cloudflare token: %q", output)
	}
	if !strings.Contains(output, "CLOUDFLARE_API_TOKEN=<value from $CLOUDFLARE_API_TOKEN>") {
		t.Fatalf("printed commands = %q, want a named placeholder for CLOUDFLARE_API_TOKEN", output)
	}
	if !strings.Contains(output, "moltnet relay deploy") {
		t.Fatalf("printed commands = %q, missing the relay deploy step", output)
	}
	if !strings.Contains(output, "moltnet pair invite show") {
		t.Fatalf("printed commands = %q, missing the invite-retrieval step", output)
	}
	if got := fake.claimAttemptCount(); got != 0 {
		t.Fatalf("claimAttemptCount() = %d, want 0 (--print-commands must never contact Cloudflare)", got)
	}
	// claimAttemptCount only ever sees PUT /workers/subdomain — the
	// VerifyToken/ResolveAccountID/WorkersDevSubdomain GETs
	// checkSetupWorkersDevClaimed would issue are otherwise invisible to
	// this test, so a regression that started making those calls under
	// --print-commands would leave the claim-only assertion above green.
	if got := fake.requestCount(); got != 0 {
		t.Fatalf("requestCount() = %d, want 0 (--print-commands must never contact Cloudflare at all)", got)
	}
	if _, err := app.LoadFile(writtenSetupConfigPathFor(home, "acme-net"), ""); err == nil {
		t.Fatal("expected --print-commands not to write a config, but it did")
	}
	// The already-claimed case's own known --print-commands inaccuracy
	// (setupPairOpenUpPlaceholderSubdomain always substitutes "<name>" for
	// --subdomain, but a real run against this same already-claimed account
	// would omit the flag — buildRelayDeployInvocation) must carry a visible
	// caveat directly under the printed block, not silently mislead a reader
	// who pastes the block verbatim.
	if !strings.Contains(output, "--subdomain is only needed if this Cloudflare account has never claimed a workers.dev subdomain") {
		t.Fatalf("printed commands = %q, missing the --subdomain caveat note", output)
	}
}

// TestSetupPairOpenUpPrintCommandsNeverPromptsWithoutToken covers
// "--print-commands must never contact Cloudflare or demand a secret" for
// the specific case that used to slip past every other test in this file:
// with no CLOUDFLARE_API_TOKEN in the environment (and nothing stored — a
// fresh HOME), a real "open it up" run would have to prompt for one
// (resolveSetupCloudflareToken); --print-commands must skip that prompt
// entirely rather than block on stdin for a secret the preview has no way
// to use.
func TestSetupPairOpenUpPrintCommandsNeverPromptsWithoutToken(t *testing.T) {
	withTestDefaultPort(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLOUDFLARE_API_TOKEN", "") // simulate no token available anywhere

	fake := startFakeCloudflareCLIServer(t, "cf-token-123")
	withRelayDeployFakeCloudflare(t, fake)

	withPromptAnswers(t, "", "acme-net", "", "", "2", "2")

	output := captureSetupOutput(t, func() {
		if err := run(context.Background(), []string{"setup", "--print-commands"}, "test"); err != nil {
			t.Fatalf("setup --print-commands error = %v", err)
		}
	})

	if strings.Contains(output, "Cloudflare API token") {
		t.Fatalf("expected no secret-prompt text with no token available, got %q", output)
	}
	if got := fake.requestCount(); got != 0 {
		t.Fatalf("requestCount() = %d, want 0 (--print-commands must never contact Cloudflare)", got)
	}
	if _, err := app.LoadFile(writtenSetupConfigPathFor(home, "acme-net"), ""); err == nil {
		t.Fatal("expected --print-commands not to write a config, but it did")
	}
}

// TestSetupPairInvocationsOrderingNeverInstallsServiceBeforePairing is the
// ordering-guarantee unit test: `service install` must never precede either
// pairing branch's config-mutating steps, matching SETUP.md's "all config
// mutations complete before the service starts".
func TestSetupPairInvocationsOrderingNeverInstallsServiceBeforePairing(t *testing.T) {
	answers := setupAnswers{
		scope:      setupScopeGlobal,
		id:         "acme",
		listenAddr: "127.0.0.1:8787",
		rooms:      []string{"general"},
		service:    true,
		connect:    setupConnectOpenUp,
		pairing: setupPairResolution{openUp: &setupPairOpenUpPlan{
			cloudflareToken: "tok",
			subdomain:       "acme",
			room:            "general",
			pairingID:       "friend-net",
		}},
	}
	invocations := setupPlanInvocations(answers, false)

	serviceIndex := -1
	lastPairingIndex := -1
	for i, invocation := range invocations {
		if len(invocation.Args) >= 2 && invocation.Args[0] == "service" && invocation.Args[1] == "install" {
			serviceIndex = i
		}
		if len(invocation.Args) >= 1 && (invocation.Args[0] == "pair" || invocation.Args[0] == "relay") {
			lastPairingIndex = i
		}
	}
	if serviceIndex == -1 {
		t.Fatal("expected a service install invocation")
	}
	if lastPairingIndex == -1 {
		t.Fatal("expected at least one pairing invocation")
	}
	if serviceIndex < lastPairingIndex {
		t.Fatalf("service install at index %d ran before the last pairing step at index %d: %+v", serviceIndex, lastPairingIndex, invocations)
	}
}
