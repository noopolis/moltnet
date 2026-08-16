package main

import (
	"strings"
	"testing"
	"time"
)

// These two tests pin the round-3 P0 fixes directly against a real
// pseudo-terminal, sharing prompt_hidden_pty_test.go's helper process
// (TestPromptHiddenHelperProcess, scenarios "flush-then-confirm" and
// "echo-unavailable"), startPromptHiddenHelper, and readMasterUntil. Split
// into their own file to keep prompt_hidden_pty_test.go under the repo's
// 400-line-per-file rule.

// TestPromptHiddenPTYMidDeployTypeaheadDoesNotAnswerSavePrompt covers the
// P0 mid-deploy-typeahead fix: even with the P0 input-flush fix in place
// (which only discards input queued before a read *starts*), anything
// typed during a slow operation that happens *before* the save prompt is
// even reached — modeled here by the helper's "flush-then-confirm"
// scenario sleeping 300ms before calling flushPendingTerminalInput and
// promptYesNoDefaultYes — must never silently answer that prompt. A stray
// "n\n" (a real no) is written to the pty while the helper is still
// "deploying"; if flushPendingTerminalInput did not discard it, the
// prompt's read would immediately consume that stale "n" and return
// false, without ever waiting for the real answer this test sends after
// the deploy delay ("y\n"). Only if the flush actually ran does the
// helper still be waiting when this test checks it and does the eventual
// real answer take effect.
func TestPromptHiddenPTYMidDeployTypeaheadDoesNotAnswerSavePrompt(t *testing.T) {
	master, slave := requireTestPTY(t)

	cmd, stderr := startPromptHiddenHelper(t, "flush-then-confirm", slave)

	// This scenario never disables echo, so every byte written to master
	// below (both the stray typeahead and the real answer) gets echoed
	// straight back into master's own read buffer by the kernel. Drain
	// continuously in the background from the start — exactly what a real
	// terminal emulator does — since the P0 input-flush fix made
	// flushPendingTerminalInput's write use the flush ioctl variant
	// (TIOCSETAF / TCSETSF), which blocks until that echoed output has
	// actually been read from the far end; without a reader running, the
	// flush call itself would stall forever instead of ever reaching the
	// prompt. See drainMasterInBackground's doc comment
	// (prompt_hidden_pty_test.go).
	_ = drainMasterInBackground(master)

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	// Written immediately, well before the helper's 300ms simulated-deploy
	// sleep elapses and it ever calls flushPendingTerminalInput/reads —
	// exactly the "typed during the deploy" scenario this fix closes.
	if _, err := master.Write([]byte("n\n")); err != nil {
		t.Fatalf("write stray typeahead to pty master: %v", err)
	}

	// The helper's sleep alone is 300ms; give it time to wake, flush, print
	// its question, and start blocking on the real read, then confirm it
	// is still waiting — proof the stray "n" typed above was discarded,
	// not consumed as the answer.
	time.Sleep(450 * time.Millisecond)
	select {
	case err := <-waitErr:
		t.Fatalf("helper process exited before a real answer was sent (err=%v) — the stray typeahead answered the prompt; stderr: %s", err, stderr.String())
	default:
	}

	if _, err := master.Write([]byte("y\n")); err != nil {
		t.Fatalf("write real answer to pty master: %v", err)
	}

	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("helper process exited with error: %v (stderr: %s)", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper process did not exit within 5s after the real answer was written")
	}

	if got := stderr.String(); !strings.Contains(got, "HELPER-RESULT:true") {
		t.Fatalf("helper stderr = %q, want HELPER-RESULT:true (the real \"y\" answer, not the stray typeahead \"n\")", got)
	}
}

// TestPromptHiddenPTYEchoDisableFailsClosed covers the P0 fail-open-echo
// fix: when the termios ioctl seam itself fails (stubbed here via the
// "echo-unavailable" scenario overriding ioctlGetTermios), the real
// maybePromptForCloudflareToken flow must refuse to print the interactive
// paste prompt or read anything at all — falling back to the guidance path
// with a clear warning instead of ever risking a secret echoing to the
// screen under an "(input hidden)" label.
func TestPromptHiddenPTYEchoDisableFailsClosed(t *testing.T) {
	master, slave := requireTestPTY(t)

	cmd, stderr := startPromptHiddenHelper(t, "echo-unavailable", slave)

	got := readMasterUntil(t, master, "cannot disable terminal echo", 5*time.Second)
	if strings.Contains(got, "paste token (input hidden)") {
		t.Fatalf("the paste prompt was printed despite the echo ioctl failing: %q", got)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper process exited with error: %v (stderr: %s)", err, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "HELPER-RESULT:err=cannot disable terminal echo") {
		t.Fatalf("helper stderr = %q, want the errTerminalEchoUnavailable error reported", got)
	}
	if got := stderr.String(); !strings.Contains(got, `token=""`) {
		t.Fatalf("helper stderr = %q, want an empty token — nothing was ever read", got)
	}
}
