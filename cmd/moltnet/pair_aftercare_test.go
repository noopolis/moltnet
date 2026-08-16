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

// TestPrintPairInviteAftercareNarrativeShape pins the redesigned `pair
// invite` aftercare's exact phase structure line by line: the two status
// lines with no blank line between them, then a blank line, then the invite
// code alone in its own paragraph (nothing else sharing its line — the P0
// fix, see TestRunPairInviteRestartPrintsCodeAndRestartConfirmation in
// pair_restart_test.go for the ordering half of that fix), then the
// numbered "Then:" sequence with its later, explained membership step.
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
	if len(lines) < 18 {
		t.Fatalf("output has %d lines, want at least 18:\n%s", len(lines), output)
	}

	if !strings.HasPrefix(lines[0], "  ✓ wrote pairing \"friend-4cfff025\"") {
		t.Fatalf("line 0 = %q, want the wrote-pairing status line", lines[0])
	}
	if !strings.Contains(lines[0], "~/.moltnet/alice-net/Moltnet") {
		t.Fatalf("line 0 = %q, want the config path abbreviated to ~", lines[0])
	}
	if lines[1] != "  restart the Moltnet server for this pairing to take effect" {
		t.Fatalf("line 1 = %q, want the restart reminder (no --restart, no service)", lines[1])
	}
	if lines[2] != "" {
		t.Fatalf("line 2 = %q, want a blank line separating status from the invite paragraph", lines[2])
	}
	if !strings.Contains(lines[3], "Send this invite to your friend") {
		t.Fatalf("line 3 = %q, want the invite-paragraph lead-in", lines[3])
	}
	if !strings.Contains(lines[4], "expires in 7 days") {
		t.Fatalf("line 4 = %q, want the credential/expiry line", lines[4])
	}
	if lines[5] != "" {
		t.Fatalf("line 5 = %q, want a blank line before the code paragraph", lines[5])
	}
	if strings.TrimSpace(lines[6]) != code {
		t.Fatalf("line 6 = %q, want the invite code alone on its own line, nothing else sharing it", lines[6])
	}
	if lines[7] != "" {
		t.Fatalf("line 7 = %q, want a blank line after the code paragraph", lines[7])
	}
	if lines[8] != "  Then:" {
		t.Fatalf("line 8 = %q, want the Then: header", lines[8])
	}
	if !strings.Contains(lines[9], "1. they run") || !strings.Contains(lines[9], "moltnet pair '<the invite above>'") {
		t.Fatalf("line 9 = %q, want step 1 naming the friend's pair command", lines[9])
	}
	if strings.Contains(lines[9], "--restart") {
		t.Fatalf("line 9 = %q, want --restart NOT recommended unconditionally in step 1 itself", lines[9])
	}
	if !strings.Contains(lines[10], "add --restart if they run it as a service") {
		t.Fatalf("line 10 = %q, want --restart mentioned only as a conditional add-on", lines[10])
	}
	if !strings.Contains(lines[11], "2. after they've paired, grant their agent access to room \"chat\"") {
		t.Fatalf("line 11 = %q, want step 2's lead-in naming room chat", lines[11])
	}
	if !strings.Contains(lines[12], "room writes need membership") {
		t.Fatalf("line 12 = %q, want the why-membership clause", lines[12])
	}
	wantCmd := "moltnet admin room members add --room chat --member <their-network-id>:<their-agent-id> --network alice-net"
	if !strings.Contains(lines[13], wantCmd) {
		t.Fatalf("line 13 = %q, want %q", lines[13], wantCmd)
	}
	if !strings.Contains(lines[14], "you'll know both ids once they've paired") {
		t.Fatalf("line 14 = %q, want the why-placeholders explanation", lines[14])
	}
	if !strings.Contains(lines[16], "remote?") {
		t.Fatalf("line 16 = %q, want the dim remote-admin aside, demoted off the numbered steps", lines[16])
	}
}

// TestPrintPairInviteAftercareNoRoomsSkipsMembershipStep covers the
// zero-shared-rooms case: the invite paragraph and the "run `moltnet pair`"
// step still print (the friend still needs to know what to do with the
// code), but there is nothing to grant membership to, so step 2 and its
// commands never appear.
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

	if !strings.Contains(output, "1. they run") {
		t.Fatalf("expected step 1 in output %q", output)
	}
	if strings.Contains(output, "admin room members add") {
		t.Fatalf("expected no membership step with zero shared rooms, got %q", output)
	}
}

// TestPrintPairJoinAftercareNarrativeShape pins the mirrored joiner-side
// narrative: the same two status lines, then a blank line, then "you're
// paired with <network>" naming the sender's real network id, then the
// fully filled in membership command (real remote network id, placeholder
// agent id, `<their-agent-id>` — the same placeholder vocabulary the
// invite side uses — since the agent id is never carried by the invite),
// its one-line swap explainer, and the friendly-name closing note.
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
	if len(lines) < 10 {
		t.Fatalf("output has %d lines, want at least 10:\n%s", len(lines), output)
	}

	if !strings.HasPrefix(lines[0], "  ✓ wrote pairing \"friend-4cfff025\"") {
		t.Fatalf("line 0 = %q, want the wrote-pairing status line", lines[0])
	}
	if lines[1] != "  restart the Moltnet server for this pairing to take effect" {
		t.Fatalf("line 1 = %q, want the restart reminder (no --restart, no service)", lines[1])
	}
	if lines[2] != "" {
		t.Fatalf("line 2 = %q, want a blank line before the paired-with paragraph", lines[2])
	}
	if !strings.Contains(lines[3], "You're paired with alice-net; last step:") {
		t.Fatalf("line 3 = %q, want the paired-with sentence naming the real remote network id", lines[3])
	}
	wantCmd := "moltnet admin room members add --room chat --member alice-net:<their-agent-id> --network bob-net"
	if !strings.Contains(output, wantCmd) {
		t.Fatalf("expected the fully filled in membership command %q in output %q", wantCmd, output)
	}
	if !strings.Contains(output, "(swap <their-agent-id> for the agent id they'll post as)") {
		t.Fatalf("expected the placeholder's one-line explainer in output %q", output)
	}
	if !strings.Contains(output, "ask whoever runs Alice's Moltnet to run the mirrored command") {
		t.Fatalf("expected the closing ask-them-to-mirror note naming the remote network's display name in %q", output)
	}
}

// TestPrintPairJoinAftercareNoRoomsPrintsPairedConfirmationOnly covers the
// zero-shared-rooms case on the joiner side: no membership step to run, but
// the plain "you're paired with <network>" fact still prints so the
// operator gets positive confirmation instead of silence.
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

	if !strings.Contains(output, "You're paired with alice-net.") {
		t.Fatalf("expected the paired-with confirmation in output %q", output)
	}
	if strings.Contains(output, "admin room members add") {
		t.Fatalf("expected no membership step with zero shared rooms, got %q", output)
	}
}

// TestPairStatusBlockAlignmentIsPlainWidthInvariant is pair.go's own P2-1
// style regression: it forces the isOutputTerminal seam (style.go) true to
// take printPairWroteLine's styled path, captures printPairStatusBlock's
// output, strips every ANSI escape code (ansiEscapePattern, defined in
// style_test.go and shared across this test binary), and asserts the
// result is byte-identical to the same call's plain output (isOutputTerminal
// at its default false). printPairWroteLine computes its column padding
// from the plain (unstyled) prefix by design, exactly like
// printInitConfigCheckLine and formatNextStep, so this pins that invariant
// for pair's own aligned column rather than relying on it holding by
// construction. It also asserts the actual column value: the abbreviated
// path annotation must start at pairStatusColumn in the plain output.
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
			printPairStatusBlock(context.Background(), config, path, "friend-4cfff025", false)
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
