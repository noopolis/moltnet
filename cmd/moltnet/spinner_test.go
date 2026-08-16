package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestStartSpinnerDisabledOverNonTTYStdout covers the default, and by far
// the most common, path: captureStdout's os.Pipe is never a real terminal
// (stdoutIsRealTTY checks the literal fd, not the mockable isOutputTerminal
// seam other tests force true), so startSpinner must return nil and write
// nothing at all — the "no spinner frames in piped output" requirement.
func TestStartSpinnerDisabledOverNonTTYStdout(t *testing.T) {
	output := captureStdout(t, func() {
		sp := startSpinner("deploying relay…")
		if sp != nil {
			t.Fatal("startSpinner() != nil over a non-TTY stdout, want nil")
		}
		sp.Stop() // must be a safe no-op on nil
	})
	if output != "" {
		t.Fatalf("output = %q, want no spinner output over a non-TTY stdout", output)
	}
}

// TestStartSpinnerDisabledUnderNoColorOverRealPTY covers the other half of
// spinnerEnabled's gate: even over a genuine pty, NO_COLOR must still
// suppress the spinner, matching colorEnabled's own NO_COLOR contract every
// other styled line in this package follows.
func TestStartSpinnerDisabledUnderNoColorOverRealPTY(t *testing.T) {
	_, slave := requireTestPTY(t)
	previous := stdout
	stdout = slave
	t.Cleanup(func() { stdout = previous })
	t.Setenv("NO_COLOR", "1")

	sp := startSpinner("deploying relay…")
	if sp != nil {
		t.Fatal("startSpinner() != nil under NO_COLOR, want nil even over a real pty")
	}
}

// TestSpinnerRendersAndClearsOverRealPTY drives a real spinner over a
// genuine pty (requireTestPTY, openpty_test.go): points the package's stdout
// var directly at the pty's slave file — the same swap captureStdout does
// with an os.Pipe, just with a real terminal-backed fd this time, so
// stdoutIsRealTTY's isatty check is genuinely true — and proves frames
// actually render, then that Stop() clears the line before the caller's own
// next write lands.
func TestSpinnerRendersAndClearsOverRealPTY(t *testing.T) {
	master, slave := requireTestPTY(t)
	previous := stdout
	stdout = slave
	t.Cleanup(func() { stdout = previous })

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

	previousInterval := spinnerInterval
	spinnerInterval = 5 * time.Millisecond
	t.Cleanup(func() { spinnerInterval = previousInterval })

	sp := startSpinner("deploying relay…")
	if sp == nil {
		t.Fatal("startSpinner() = nil, want a running spinner over a real, color-capable pty")
	}
	time.Sleep(60 * time.Millisecond)
	sp.Stop()
	if _, err := slave.Write([]byte("done\n")); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	got := readMasterUntil(t, master, "done", 2*time.Second)
	if !strings.Contains(got, "deploying relay") {
		t.Fatalf("expected the spinner label in output, got %q", got)
	}
	if !strings.ContainsAny(got, strings.Join(spinnerFrames, "")) {
		t.Fatalf("expected at least one spinner frame character, got %q", got)
	}
	// Stop's own clear ("\r\x1b[2K") must be the last thing written before
	// the caller's "done\n" — proof the line was actually cleared, not left
	// with a stray frame sitting in front of the next write.
	if !strings.Contains(got, "\x1b[2Kdone") {
		t.Fatalf("expected the clear-line escape immediately before the next write, got %q", got)
	}
}

// TestSpinnerStopIsIdempotentAndSafeOnNil covers Stop's two safety
// properties directly: calling it twice on a real spinner never panics
// (double-close), and calling it on a nil *spinner (the disabled-path
// return value every caller gets on piped/non-TTY output) is a no-op, not a
// nil-pointer panic.
func TestSpinnerStopIsIdempotentAndSafeOnNil(t *testing.T) {
	var nilSpinner *spinner
	nilSpinner.Stop()
	nilSpinner.Stop()

	_, slave := requireTestPTY(t)
	previous := stdout
	stdout = slave
	t.Cleanup(func() { stdout = previous })
	t.Setenv("TERM", "xterm-256color")
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

	sp := startSpinner("working…")
	if sp == nil {
		t.Fatal("startSpinner() = nil, want a running spinner over a real pty")
	}
	sp.Stop()
	sp.Stop()
}
