package main

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestPromptHiddenPTYSignalsRestoreEchoAndExit covers the P1-1 fix: SIGINT,
// SIGTERM, and SIGHUP arriving while promptHidden is blocked reading must
// each (a) make the process exit promptly, with the conventional
// 128+signal exit code, and (b) leave the terminal's ECHO bit restored —
// not left off, which is what happened before this fix (see prompt_hidden.go's
// disableTerminalEcho doc comment: runCLI's own
// signal.NotifyContext(SIGINT, SIGTERM) means the process does not die on
// these by default once this package also registers for them, so the
// handler itself must actually exit). Split out from prompt_hidden_pty_test.go
// to keep both files under the repo's 400-line-per-file rule; it shares that
// file's helper process (TestPromptHiddenHelperProcess), startPromptHiddenHelper,
// readMasterUntil, masterEcho, and waitForMasterEchoOff.
func TestPromptHiddenPTYSignalsRestoreEchoAndExit(t *testing.T) {
	cases := []struct {
		name     string
		signal   syscall.Signal
		wantCode int
	}{
		{"SIGHUP", syscall.SIGHUP, 128 + 1},
		{"SIGINT", syscall.SIGINT, 128 + 2},
		{"SIGTERM", syscall.SIGTERM, 128 + 15},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			master, slave := requireTestPTY(t)

			cmd, stderr := startPromptHiddenHelper(t, "block-on-read", slave)

			readMasterUntil(t, master, "paste token (input hidden): ", 5*time.Second)
			// promptHidden prints its (short, non-secret) question before
			// disabling echo, so seeing the question alone does not yet
			// prove disableTerminalEcho — and its signal.Notify
			// registration, which runs right after — has completed; poll
			// instead of asserting on a single snapshot.
			waitForMasterEchoOff(t, master, 2*time.Second)
			// signal.Notify runs immediately after the termios ioctl inside
			// disableTerminalEcho, in the same goroutine with no further
			// I/O in between (prompt_hidden.go) — a short grace period
			// closes that last sliver of scheduling race before signaling.
			time.Sleep(20 * time.Millisecond)

			if err := cmd.Process.Signal(tc.signal); err != nil {
				t.Fatalf("send %s to helper process: %v", tc.name, err)
			}

			waitErr := make(chan error, 1)
			go func() { waitErr <- cmd.Wait() }()

			select {
			case err := <-waitErr:
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("helper process wait error = %v (%T), want *exec.ExitError; stderr: %s", err, err, stderr.String())
				}
				if got := exitErr.ExitCode(); got != tc.wantCode {
					t.Fatalf("helper process exit code = %d, want %d (128+%s); stderr: %s", got, tc.wantCode, tc.name, stderr.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("helper process did not exit within 5s of receiving %s", tc.name)
			}

			if !masterEcho(t, master) {
				t.Fatalf("ECHO is still off after %s killed the helper process; the signal handler did not restore it", tc.name)
			}
		})
	}
}
