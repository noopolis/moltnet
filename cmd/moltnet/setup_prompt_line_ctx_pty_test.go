package main

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

// TestSetupPTYSIGINTDuringNetworkIDPromptAbortsWithoutEnter is P1-2's own
// regression guard: before promptLineCtx existed, askSetupNetworkID's Q2
// read through the plain, non-ctx-aware promptLine, so a SIGINT while
// genuinely blocked waiting for the typed answer (runCLI's own
// signal.NotifyContext cancelling ctx, cli.go) had no effect at all until
// the operator pressed Enter — the process stayed blocked in ReadString
// regardless of ctx. This drives Q1 to completion over a real pty (so Q2's
// prompt is genuinely blocked on a read, not a scripted reader), sends a
// real SIGINT with no Enter ever following it, and asserts the wizard
// returns promptly with context.Canceled instead of hanging until an Enter
// that never comes.
func TestSetupPTYSIGINTDuringNetworkIDPromptAbortsWithoutEnter(t *testing.T) {
	withTestDefaultPort(t)
	t.Setenv("HOME", t.TempDir())

	master, slave := requireTestPTY(t)
	withSetupPTYStdio(t, slave)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runCLI([]string{"setup"}, "test", defaultSignalContext)
	}()

	readMasterUntil(t, master, "Where should this network live?", 5*time.Second)
	if _, err := master.Write([]byte("\r")); err != nil { // Q1: confirm default.
		t.Fatalf("confirm Q1: %v", err)
	}

	readMasterUntil(t, master, "Name it [local]:", 5*time.Second)

	start := time.Now()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("signal self with SIGINT: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runCLI() error = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed >= time.Second {
			t.Fatalf("runCLI() took %v to return after SIGINT with no Enter ever sent, want it to abort promptly rather than wait on a keypress that never comes", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("setup did not return within 3s of SIGINT at the typed Q2 prompt (no Enter was ever sent) — promptLineCtx did not honor ctx cancellation")
	}
}
