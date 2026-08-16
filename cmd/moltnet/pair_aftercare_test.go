package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/app"
	authn "github.com/noopolis/moltnet/internal/auth"
)

// pairAftercareTestConfig builds a minimal app.Config for exercising the
// pair aftercare printers (printPairInviteAftercare, printPairJoinAftercare,
// printPairStatusBlock) directly, without a real config file load: only
// NetworkID and Auth.Mode feed their output, and Auth.Mode is pinned to
// "bearer" so the auth.mode note never fires and does not have to be
// accounted for in these narrative-shape assertions.
func pairAftercareTestConfig(networkID string) app.Config {
	return app.Config{NetworkID: networkID, Auth: authn.Config{Mode: "bearer"}}
}

// TestPrintPairInviteAftercareNarrativeShape pins the quiet-by-default
// `pair invite` aftercare's exact phase structure line by line: a single
// "pairing ready" checkmark, then the unconditional plain restart reminder
// (P1 fix — there is no hot-reload, so a pairing written without a restart
// simply does not work yet; this prints regardless of --verbose whenever no
// restart was actually performed, see printPairRestartLine), then a blank
// line, then the ONE copyable `moltnet pair '<code>'` command in its own
// paragraph (rule 4: no separate "they run ..." line, no bare code on its
// own — the command IS the invite), then a single "next:" line naming the
// membership command for the one declared room.
func TestPrintPairInviteAftercareNarrativeShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := home + "/.moltnet/alice-net/Moltnet"
	config := pairAftercareTestConfig("alice-net")
	code := "moltnet-invite:test-code-body"

	output := captureStdout(t, func() {
		if err := printPairInviteAftercare(context.Background(), config, path, code, "friend-4cfff025", pairAftercareOptions{
			roomIDs: []string{"chat"},
		}); err != nil {
			t.Fatalf("printPairInviteAftercare() error = %v", err)
		}
	})

	lines := strings.Split(output, "\n")
	if len(lines) < 7 {
		t.Fatalf("output has %d lines, want at least 7:\n%s", len(lines), output)
	}

	if lines[0] != "  ✓ pairing ready" {
		t.Fatalf("line 0 = %q, want the single pairing-ready checkmark", lines[0])
	}
	if lines[1] != "  restart the Moltnet server for this pairing to take effect" {
		t.Fatalf("line 1 = %q, want the unconditional plain restart reminder (no restart was requested)", lines[1])
	}
	if lines[2] != "" {
		t.Fatalf("line 2 = %q, want a blank line separating the restart reminder from the share paragraph", lines[2])
	}
	if !strings.Contains(lines[3], "share this with your friend") || !strings.Contains(lines[3], "expires in 7 days") {
		t.Fatalf("line 3 = %q, want the share-paragraph lead-in with the expiry", lines[3])
	}
	if lines[4] != "" {
		t.Fatalf("line 4 = %q, want a blank line before the copyable command", lines[4])
	}
	wantCommandLine := "    moltnet pair '" + code + "'"
	if lines[5] != wantCommandLine {
		t.Fatalf("line 5 = %q, want the one copyable command %q (single-quoted, paste-safe, nothing else sharing the line)", lines[5], wantCommandLine)
	}
	if lines[6] != "" {
		t.Fatalf("line 6 = %q, want a blank line before the next: block", lines[6])
	}
	if !strings.Contains(output, "next: "+pairMembershipCommand("chat", "<their-network-id>:<their-agent-id>", "alice-net")) {
		t.Fatalf("expected a single next: line naming the membership command, got %q", output)
	}
	if strings.Contains(output, "they run") || strings.Contains(output, "Then:") {
		t.Fatalf("expected no redundant \"they run\" step or numbered Then: menu, got %q", output)
	}
	if strings.Contains(output, "remote?") {
		t.Fatalf("expected the remote-admin aside to stay hidden without --verbose, got %q", output)
	}
}

// TestPrintPairInviteAftercareVerboseRestoresDetail covers --verbose parity:
// the quiet essentials (the checkmark, the copyable command, the next: line)
// must all still be present, plus the detail quiet mode hides — the
// wrote-pairing path line and the remote-admin aside.
func TestPrintPairInviteAftercareVerboseRestoresDetail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := home + "/.moltnet/alice-net/Moltnet"
	config := pairAftercareTestConfig("alice-net")
	code := "moltnet-invite:test-code-body"

	quiet := captureStdout(t, func() {
		if err := printPairInviteAftercare(context.Background(), config, path, code, "friend-4cfff025", pairAftercareOptions{
			roomIDs: []string{"chat"},
		}); err != nil {
			t.Fatalf("printPairInviteAftercare() error = %v", err)
		}
	})
	verbose := captureStdout(t, func() {
		if err := printPairInviteAftercare(context.Background(), config, path, code, "friend-4cfff025", pairAftercareOptions{
			roomIDs: []string{"chat"},
			verbose: true,
		}); err != nil {
			t.Fatalf("printPairInviteAftercare() --verbose error = %v", err)
		}
	})

	if !strings.Contains(verbose, "wrote pairing \"friend-4cfff025\"") {
		t.Fatalf("expected --verbose to restore the wrote-pairing line, got %q", verbose)
	}
	if !strings.Contains(verbose, "~/.moltnet/alice-net/Moltnet") {
		t.Fatalf("expected --verbose to restore the abbreviated config path, got %q", verbose)
	}
	if !strings.Contains(verbose, "remote?") {
		t.Fatalf("expected --verbose to restore the remote-admin aside, got %q", verbose)
	}

	// Superset check: every essential quiet line still appears in verbose
	// output (content-wise — exact spacing/position may differ).
	for _, essential := range []string{
		"✓ pairing ready",
		"moltnet pair '" + code + "'",
		pairMembershipCommand("chat", "<their-network-id>:<their-agent-id>", "alice-net"),
	} {
		if !strings.Contains(verbose, essential) {
			t.Fatalf("verbose output missing quiet essential %q:\n%s", essential, verbose)
		}
		if !strings.Contains(quiet, essential) {
			t.Fatalf("quiet output missing its own essential %q:\n%s", essential, quiet)
		}
	}
}

// TestPrintPairInviteAftercareNoRoomsSkipsMembershipStep covers the
// zero-shared-rooms case: the checkmark and the copyable command still
// print (the friend still needs to know what to do with it), but there is
// nothing to grant membership to, so no next: line ever appears.
func TestPrintPairInviteAftercareNoRoomsSkipsMembershipStep(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := home + "/.moltnet/alice-net/Moltnet"
	config := pairAftercareTestConfig("alice-net")
	code := "moltnet-invite:test-code-body"

	output := captureStdout(t, func() {
		if err := printPairInviteAftercare(context.Background(), config, path, code, "friend-4cfff025", pairAftercareOptions{}); err != nil {
			t.Fatalf("printPairInviteAftercare() error = %v", err)
		}
	})

	if !strings.Contains(output, "moltnet pair '"+code+"'") {
		t.Fatalf("expected the copyable pair command in output %q", output)
	}
	if strings.Contains(output, "admin room members add") {
		t.Fatalf("expected no membership step with zero shared rooms, got %q", output)
	}
	if strings.Contains(output, "next:") {
		t.Fatalf("expected no next: line with zero shared rooms, got %q", output)
	}
}

// TestPrintPairJoinAftercareNarrativeShape pins the mirrored joiner-side
// narrative: a single "paired with <network>" checkmark naming the sender's
// real network id, then a single next: line naming the fully filled-in
// membership command (real remote network id, placeholder agent id).
func TestPrintPairJoinAftercareNarrativeShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := home + "/.moltnet/bob-net/Moltnet"
	config := pairAftercareTestConfig("bob-net")

	output := captureStdout(t, func() {
		if err := printPairJoinAftercare(context.Background(), config, path, "friend-4cfff025", pairAftercareOptions{
			roomIDs:           []string{"chat"},
			remoteNetworkID:   "alice-net",
			remoteNetworkName: "Alice's Moltnet",
		}); err != nil {
			t.Fatalf("printPairJoinAftercare() error = %v", err)
		}
	})

	lines := strings.Split(output, "\n")
	if len(lines) < 3 {
		t.Fatalf("output has %d lines, want at least 3:\n%s", len(lines), output)
	}
	if lines[0] != "  ✓ paired with alice-net" {
		t.Fatalf("line 0 = %q, want the paired-with checkmark naming the real remote network id", lines[0])
	}

	wantCmd := pairMembershipCommand("chat", "alice-net:<their-agent-id>", "bob-net")
	if !strings.Contains(output, "next: "+wantCmd) {
		t.Fatalf("expected the fully filled in membership command as a next: line, got %q", output)
	}
	if strings.Contains(output, "swap <their-agent-id>") || strings.Contains(output, "ask whoever runs") {
		t.Fatalf("expected the verbose-only explainer asides to stay hidden without --verbose, got %q", output)
	}
}

// TestPrintPairJoinAftercareVerboseRestoresDetail covers --verbose parity on
// the joiner side: the wrote-pairing line and the placeholder/remote-admin
// explainer asides come back, and every quiet essential still appears.
func TestPrintPairJoinAftercareVerboseRestoresDetail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := home + "/.moltnet/bob-net/Moltnet"
	config := pairAftercareTestConfig("bob-net")

	verbose := captureStdout(t, func() {
		if err := printPairJoinAftercare(context.Background(), config, path, "friend-4cfff025", pairAftercareOptions{
			roomIDs:           []string{"chat"},
			verbose:           true,
			remoteNetworkID:   "alice-net",
			remoteNetworkName: "Alice's Moltnet",
		}); err != nil {
			t.Fatalf("printPairJoinAftercare() --verbose error = %v", err)
		}
	})

	if !strings.Contains(verbose, "wrote pairing \"friend-4cfff025\"") {
		t.Fatalf("expected --verbose to restore the wrote-pairing line, got %q", verbose)
	}
	if !strings.Contains(verbose, "swap <their-agent-id> for the agent id they'll post as") {
		t.Fatalf("expected --verbose to restore the placeholder explainer, got %q", verbose)
	}
	if !strings.Contains(verbose, "ask whoever runs Alice's Moltnet to run the mirrored command") {
		t.Fatalf("expected --verbose to restore the closing ask-them-to-mirror note naming the remote network's display name, got %q", verbose)
	}
	if !strings.Contains(verbose, "remote?") {
		t.Fatalf("expected --verbose to restore the remote-admin aside, got %q", verbose)
	}
	if !strings.Contains(verbose, "✓ paired with alice-net") {
		t.Fatalf("expected the quiet checkmark to still appear under --verbose, got %q", verbose)
	}
}

// TestPrintPairJoinAftercareNoRoomsPrintsPairedConfirmationOnly covers the
// zero-shared-rooms case on the joiner side: no membership step to run, but
// the checkmark still prints so the operator gets positive confirmation
// instead of silence.
func TestPrintPairJoinAftercareNoRoomsPrintsPairedConfirmationOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := home + "/.moltnet/bob-net/Moltnet"
	config := pairAftercareTestConfig("bob-net")

	output := captureStdout(t, func() {
		if err := printPairJoinAftercare(context.Background(), config, path, "friend-4cfff025", pairAftercareOptions{
			remoteNetworkID:   "alice-net",
			remoteNetworkName: "Alice's Moltnet",
		}); err != nil {
			t.Fatalf("printPairJoinAftercare() error = %v", err)
		}
	})

	if !strings.Contains(output, "✓ paired with alice-net") {
		t.Fatalf("expected the paired-with checkmark in output %q", output)
	}
	if strings.Contains(output, "admin room members add") {
		t.Fatalf("expected no membership step with zero shared rooms, got %q", output)
	}
}

// TestPairStatusBlockAlignmentIsPlainWidthInvariant is pair.go's own P2-1
// style regression, run under --verbose (the only mode printPairWroteLine's
// column-aligned path line prints at all): it forces the isOutputTerminal
// seam (style.go) true to take printPairWroteLine's styled path, captures
// printPairStatusBlock's output, strips every ANSI escape code
// (ansiEscapePattern, defined in style_test.go and shared across this test
// binary), and asserts the result is byte-identical to the same call's
// plain output (isOutputTerminal at its default false). printPairWroteLine
// computes its column padding from the plain (unstyled) prefix by design,
// exactly like printInitConfigCheckLine and formatNextStepWithPrefix, so
// this pins that invariant for pair's own aligned column rather than
// relying on it holding by construction. It also asserts the actual column
// value: the abbreviated path annotation must start at pairStatusColumn in
// the plain output.
func TestPairStatusBlockAlignmentIsPlainWidthInvariant(t *testing.T) {
	previousNoColor, hadNoColor := os.LookupEnv("NO_COLOR")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatalf("unset NO_COLOR: %v", err)
	}
	t.Cleanup(func() {
		if hadNoColor {
			os.Setenv("NO_COLOR", previousNoColor)
		} else {
			os.Unsetenv("NO_COLOR")
		}
	})
	t.Setenv("TERM", "xterm-256color")

	home := t.TempDir()
	t.Setenv("HOME", home)
	path := home + "/.moltnet/alice-net/Moltnet"
	config := pairAftercareTestConfig("alice-net")

	statusBlockCase := func(t *testing.T) string {
		t.Helper()
		return captureStdout(t, func() {
			printPairStatusBlock(context.Background(), config, path, "friend-4cfff025", false, true)
		})
	}

	previousTerminal := isOutputTerminal
	isOutputTerminal = func() bool { return true }
	styledOutput := statusBlockCase(t)
	isOutputTerminal = previousTerminal

	if !strings.ContainsRune(styledOutput, '\x1b') {
		t.Fatalf("styledOutput = %q, want at least one ANSI escape code from the TTY-styled path", styledOutput)
	}

	plainOutput := statusBlockCase(t)
	if strings.ContainsRune(plainOutput, '\x1b') {
		t.Fatalf("plainOutput = %q, want no ANSI escape codes from the non-TTY default path", plainOutput)
	}

	strippedStyled := ansiEscapePattern.ReplaceAllString(styledOutput, "")
	if strippedStyled != plainOutput {
		t.Fatalf("ANSI-stripped styled output does not byte-match plain output:\nstripped styled = %q\nplain           = %q", strippedStyled, plainOutput)
	}

	// P3: pin the actual column value, not just its invariance under
	// styling — the abbreviated path annotation must start exactly at
	// pairStatusColumn in the plain output, confirming printPairWroteLine's
	// padding lands where pairStatusColumn's own doc comment says it does.
	firstLine := strings.SplitN(plainOutput, "\n", 2)[0]
	firstLineRunes := []rune(firstLine)
	if len(firstLineRunes) < pairStatusColumn {
		t.Fatalf("first line %q is shorter than pairStatusColumn (%d)", firstLine, pairStatusColumn)
	}
	wantAnnotation := abbreviateHome(path)
	if gotAnnotation := string(firstLineRunes[pairStatusColumn:]); gotAnnotation != wantAnnotation {
		t.Fatalf("path annotation does not start at column %d: got %q, want %q\nfull line: %q", pairStatusColumn, gotAnnotation, wantAnnotation, firstLine)
	}
}
