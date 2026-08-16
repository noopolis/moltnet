package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// ansiEscapePattern matches any ANSI SGR escape sequence this CLI emits
// (e.g. "\x1b[1m", "\x1b[0m"), used by TestAlignmentIsPlainWidthInvariant to
// strip styling before comparing against plain output.
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// TestStyleColorsWhenTerminalAndNoColorUnset forces the isOutputTerminal
// seam (the same override pattern uninstall_test.go uses for
// isInteractive) to simulate a TTY without a real terminal attached to the
// captured-stdout pipe, and checks that styling actually emits ANSI escape
// codes: the banner is dimmed, and a "Next:" line's command is bolded while
// its description is dimmed.
func TestStyleColorsWhenTerminalAndNoColorUnset(t *testing.T) {
	previousTerminal := isOutputTerminal
	isOutputTerminal = func() bool { return true }
	t.Cleanup(func() { isOutputTerminal = previousTerminal })
	// t.Setenv can only set a value, not unset one, so save/restore
	// NO_COLOR by hand to guarantee it is absent for this test regardless
	// of the ambient environment.
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

	output := captureStdout(t, func() {
		printBanner()
		printNextStep(nextStep{command: "moltnet service install --id acme", description: "run it as a service"})
	})

	if !strings.Contains(output, ansiDim) {
		t.Fatalf("output = %q, want a dim escape code from the banner", output)
	}
	if !strings.Contains(output, ansiBold) {
		t.Fatalf("output = %q, want a bold escape code from the next: command", output)
	}
	if !strings.Contains(output, bannerText) {
		t.Fatalf("output = %q, want the banner wordmark to appear on a terminal", output)
	}
	if !strings.Contains(output, ansiReset) {
		t.Fatalf("output = %q, want at least one reset escape code", output)
	}
}

// TestStyleNoColorEnvKeepsOutputPlain forces the terminal seam the same way,
// but sets NO_COLOR (any non-empty value per https://no-color.org), and
// checks the banner still appears — TTY gates whether it prints at all, not
// its color — but with no ANSI escape codes anywhere in the output.
func TestStyleNoColorEnvKeepsOutputPlain(t *testing.T) {
	previousTerminal := isOutputTerminal
	isOutputTerminal = func() bool { return true }
	t.Cleanup(func() { isOutputTerminal = previousTerminal })
	t.Setenv("NO_COLOR", "1")

	output := captureStdout(t, func() {
		printBanner()
		printNextStep(nextStep{command: "moltnet service install --id acme", description: "run it as a service"})
	})

	if !strings.Contains(output, bannerText) {
		t.Fatalf("output = %q, want the banner wordmark to still appear under NO_COLOR", output)
	}
	if strings.ContainsRune(output, '\x1b') {
		t.Fatalf("output = %q, want no ANSI escape codes under NO_COLOR", output)
	}
}

// TestStyleBannerHiddenWhenNotTerminal is the non-TTY control: with
// isOutputTerminal at its default (false against the test's os.Pipe
// stdout), printBanner is a no-op — the same path the golden init tests
// exercise, pinned here directly against the banner.
func TestStyleBannerHiddenWhenNotTerminal(t *testing.T) {
	output := captureStdout(t, func() {
		printBanner()
	})
	if output != "" {
		t.Fatalf("output = %q, want no banner output when stdout is not a terminal", output)
	}
}

// alignmentInvariantCase renders printInitSummary (under --verbose, so its
// per-file "wrote <label>" checkmark lines actually print) followed by
// printNextStepList against a fixed initSummary and nextStep set chosen to
// exercise both column-aligned helpers: printInitConfigCheckLine's "wrote
// <label>" extra column (init_summary.go) and formatNextStepWithPrefix's
// command/description column (nextsteps.go), including one command long
// enough to force the wrapped-description continuation line.
func alignmentInvariantCase(t *testing.T) string {
	t.Helper()
	return captureStdout(t, func() {
		printInitSummary(initSummary{
			id:            "acme-friends",
			root:          "/home/example/.moltnet/acme-friends",
			dirExisted:    false,
			serverPath:    "/home/example/.moltnet/acme-friends/Moltnet",
			serverCreated: true,
			nodeCreated:   true,
			bearer:        true,
			bearerAdded:   false,
			verbose:       true,
		})
		printNextStepList([]nextStep{
			{
				command:     "moltnet pair invite --network-id acme-friends --room a-room-id-long-enough-to-force-the-description-to-wrap",
				description: "invite a friend",
			},
		})
	})
}

// relayDeploySummaryAlignmentInvariantCase renders the two `relay deploy`
// success-path checkmarks (printInitConfigCheckLine) plus a "Next:" block.
//
// P2-1: an earlier version of this case also hand-wrote the relay url/note:/
// warning: lines `relay deploy` itself prints (relay_deploy.go), styled with
// dim()/yellow() directly rather than through any production helper. That
// pinned nothing about alignment — those lines carry no column computation
// at all, just a fixed indent — and, being copied literals rather than a
// call into relay_deploy.go, silently drifted out of sync with the real
// wording whenever that file changed (see the P2-5 hostname-in-the-DNS-note
// fix, which would have needed a matching hand-edit here forever). Trimmed
// down to just the two printInitConfigCheckLine calls: real production
// code, not a copy of it, and the second call's long "what" string is
// deliberately chosen to land on printInitConfigCheckLine's single-space
// fallback column (extra starting at a bare space, not padded to
// configLineColumn) — a branch alignmentInvariantCase above never reaches,
// since every "what" string it uses is short enough to hit the padded
// branch instead.
func relayDeploySummaryAlignmentInvariantCase(t *testing.T) string {
	t.Helper()
	return captureStdout(t, func() {
		printInitConfigCheckLine(`deployed relay Worker "moltnet-relay"`, "")
		printInitConfigCheckLine("saved relay credentials", "/home/example/.moltnet/acme-friends/.moltnet/relay.json")
		printNextStep(nextStep{command: "moltnet pair invite --network-id acme-friends --room chat", description: "invite a friend over this relay"})
	})
}

// TestAlignmentIsPlainWidthInvariant is the P2-1 regression test: it forces
// the isOutputTerminal seam to true so printInitConfigCheckLine and
// formatNextStepWithPrefix take their styled path (NO_COLOR unset), captures their
// combined output, strips every ANSI escape code, and asserts the result is
// byte-identical to the same calls' plain output (isOutputTerminal at its
// default false, i.e. NO_COLOR/non-TTY) — i.e. that ANSI styling never
// changes where padding lands, and that stripping it back out is lossless.
// Column widths in both helpers are computed from the plain (unstyled)
// prefix by design — see printInitConfigCheckLine's and formatNextStepWithPrefix's doc
// comments — so ANSI codes must never shift where padding lands; this pins
// that invariant directly rather than relying on it holding by construction.
// It checks both the `init` summary shape (alignmentInvariantCase) and
// `relay deploy`'s own checkmark lines (relayDeploySummaryAlignmentInvariantCase,
// see its doc comment for why that case is narrower than its name once
// implied).
func TestAlignmentIsPlainWidthInvariant(t *testing.T) {
	// Guarantee the styled path is actually styled regardless of the
	// ambient environment: NO_COLOR must be absent and TERM must be a
	// color-capable value, otherwise colorEnabled() would return false
	// even with isOutputTerminal forced true, making the ANSI-stripping
	// comparison below vacuous (stripping nothing from output that was
	// never styled in the first place).
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

	for name, run := range map[string]func(t *testing.T) string{
		"init":        alignmentInvariantCase,
		"relayDeploy": relayDeploySummaryAlignmentInvariantCase,
	} {
		t.Run(name, func(t *testing.T) {
			previousTerminal := isOutputTerminal
			isOutputTerminal = func() bool { return true }
			styledOutput := run(t)
			isOutputTerminal = previousTerminal

			if !strings.ContainsRune(styledOutput, '\x1b') {
				t.Fatalf("styledOutput = %q, want at least one ANSI escape code from the TTY-styled path", styledOutput)
			}

			plainOutput := run(t)
			if strings.ContainsRune(plainOutput, '\x1b') {
				t.Fatalf("plainOutput = %q, want no ANSI escape codes from the non-TTY default path", plainOutput)
			}

			strippedStyled := ansiEscapePattern.ReplaceAllString(styledOutput, "")
			if strippedStyled != plainOutput {
				t.Fatalf("ANSI-stripped styled output does not byte-match plain output:\nstripped styled = %q\nplain           = %q", strippedStyled, plainOutput)
			}
		})
	}
}
