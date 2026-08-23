package main

// setup_header_pty2_test.go continues setup_header_pty_test.go, split
// purely to stay under this repo's 400-line limit: the mutation-check
// test, SIGWINCH, forbidden-escape, and Ctrl-C tests, sharing every helper
// (snapshotIdx, prepareSetupHeaderPTYTest, waitForSubstring, ...) defined
// in that file.

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestSetupHeaderSuspendResumeMutationCheck is the task's own mandated
// mutation check, expressed as a permanent regression test rather than a
// one-off manual edit: it calls promptSelect directly (bypassing
// setupPromptSelect's Suspend/Resume entirely, simulating "the suspension
// code deleted") and proves the ticker keeps right on ticking through a
// blocked read when nothing suspends it — the exact corruption hazard
// TestSetupHeaderTickerSuspendedDuringPromptRead exists to catch. If this
// test ever failed to detect that (i.e. passed with the index unchanged
// even with no suspension), it would mean the real property is being
// tested some other way and TestSetupHeaderTickerSuspendedDuringPromptRead
// could no longer be trusted as a regression guard for it.
func TestSetupHeaderSuspendResumeMutationCheck(t *testing.T) {
	master, slave := prepareSetupHeaderPTYTest(t, 80)
	shrinkSetupHeaderTickInterval(t, 5*time.Millisecond)

	previousStdin := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() { os.Stdin = previousStdin })
	previousReader := promptReader
	promptReader = slave
	t.Cleanup(func() { promptReader = previousReader })

	// Continuous drain from before the header's first paint — see the
	// identical comment in TestSetupHeaderTickerSuspendedDuringPromptRead
	// for why this must not be a one-shot readMasterUntil here.
	buf := drainMasterInBackground(master)

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil, want a live header")
	}
	defer h.Stop()

	options := []selectOption{{label: "alpha"}, {label: "beta"}}
	resultCh := make(chan error, 1)
	go func() {
		// Deliberately calling promptSelect directly, not
		// setupPromptSelect: no Suspend() ever runs, so the ticker is left
		// completely free to keep firing through this blocked read.
		_, _, err := promptSelect(context.Background(), "Pick one (unsuspended)", options, 0)
		resultCh <- err
	}()

	waitForSubstring(t, buf, "Pick one (unsuspended)", 5*time.Second)
	idxDuringPrompt := h.snapshotIdx()
	waitForIdxChange(t, h, idxDuringPrompt, 2*time.Second) // proves the ticker is NOT suspended here, unlike the real test above.

	if _, err := master.Write([]byte("\r")); err != nil {
		t.Fatalf("confirm select prompt: %v", err)
	}
	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("promptSelect() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("promptSelect did not return within 5s of confirming")
	}
}

// TestSetupHeaderSIGWINCHMarksAccountingStaleAndSkipsRepaint is SETUP.md
// constraint 4's test, updated for round 3's P2-1 fix: onResize used to
// blindly redraw using bodyLines figures accumulated under the terminal's
// *old* width, even though a real resize reflows already-printed wrapped
// lines and makes those figures describe a cursor position that no longer
// exists. The corrected contract is the opposite of what this test used to
// assert: SIGWINCH must mark the accounting stale and produce NO repaint at
// all (rather than one, painted with numbers that can no longer be trusted),
// while still never disturbing transcript content already printed below the
// header.
func TestSetupHeaderSIGWINCHMarksAccountingStaleAndSkipsRepaint(t *testing.T) {
	master, _ := prepareSetupHeaderPTYTest(t, 80)
	shrinkSetupHeaderTickInterval(t, time.Hour) // effectively disables natural ticking for this test's duration.

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil, want a live header")
	}
	defer h.Stop()

	if _, err := stdout.Write([]byte("  transcript line one\n")); err != nil {
		t.Fatalf("write transcript line: %v", err)
	}

	buf := drainMasterInBackground(master)
	marker := len(buf.String())
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("signal self with SIGWINCH: %v", err)
	}

	time.Sleep(200 * time.Millisecond) // give onResize's goroutine time to run, if it were going to (wrongly) repaint.
	if got := buf.String()[marker:]; strings.Contains(got, "\x1b[2K") {
		t.Fatalf("header repainted on SIGWINCH using bodyLines accounting from before the resize, want it to stay silent instead (P2-1): %q", got)
	}

	h.mu.Lock()
	stale := h.accountingStale
	h.mu.Unlock()
	if !stale {
		t.Fatal("accountingStale was not set by onResize")
	}

	if _, err := stdout.Write([]byte("  transcript line two\n")); err != nil {
		t.Fatalf("write second transcript line: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	full := stripANSI(buf.String())
	if !strings.Contains(full, "transcript line one") {
		t.Fatalf("transcript line one missing/corrupted after SIGWINCH: %q", full)
	}
	if !strings.Contains(full, "transcript line two") {
		t.Fatalf("transcript line two (written after the resize) missing: %q", full)
	}
}

// TestSetupHeaderRedrawSkipsWhileAccountingStale is P2-1's own latch test,
// direct and white-box: redraw must refuse to paint anything at all while
// accountingStale is set — not just avoid onResize's own particular call
// site — and it must clear the latch and resume painting once bodyLines
// genuinely returns to 0, the same "checked at the top, cleared by an
// explicit condition" shape scrolledOff (P1-2) already uses.
func TestSetupHeaderRedrawSkipsWhileAccountingStale(t *testing.T) {
	master, _ := prepareSetupHeaderPTYTest(t, 80)
	shrinkSetupHeaderTickInterval(t, time.Hour) // isolate from ticker noise; only the direct redraw() calls below should move anything.
	buf := drainMasterInBackground(master)

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil, want a live header")
	}
	defer h.Stop()

	if _, err := stdout.Write([]byte("  transcript line\n")); err != nil {
		t.Fatalf("write transcript line: %v", err)
	}

	h.mu.Lock()
	h.accountingStale = true
	h.mu.Unlock()

	marker := len(buf.String())
	h.mu.Lock()
	h.redraw(h.idx)
	h.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	if got := buf.String()[marker:]; strings.Contains(got, "\x1b[2K") {
		t.Fatalf("redraw painted while accountingStale was set: %q", got)
	}

	// Once bodyLines genuinely returns to 0 (never happens naturally past a
	// real run's first paint, but the mechanism itself must still work),
	// the latch clears and redraw resumes painting.
	h.mu.Lock()
	h.bodyLines = 0
	h.mu.Unlock()

	marker = len(buf.String())
	h.mu.Lock()
	h.redraw(h.idx)
	staleAfter := h.accountingStale
	h.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	if staleAfter {
		t.Fatal("accountingStale is still true after redraw ran with bodyLines back at 0")
	}
	if got := buf.String()[marker:]; !strings.Contains(got, "\x1b[2K") {
		t.Fatalf("redraw did not repaint once bodyLines returned to 0: %q", got)
	}
}

// TestSetupHeaderStopRacesSIGWINCHUnderRace is P2-2's own test: it fires
// SIGWINCH in a tight loop from a background goroutine while Stop() runs
// concurrently, maximizing the odds that some onResize call is genuinely
// in flight (reading the package stdout var via terminalWidth/terminalSize)
// at the exact moment Stop() writes to that same var (`stdout = h.file`).
// Without stopWatchingResize's <-done wait, `go test -race` catches this as
// a real data race; with it, Stop() cannot proceed until any in-flight
// onResize has actually returned. The assertion is just that Stop()
// returns at all (no hang) — -race is what actually verifies the property,
// so this test only earns its keep run with -race.
func TestSetupHeaderStopRacesSIGWINCHUnderRace(t *testing.T) {
	master, _ := prepareSetupHeaderPTYTest(t, 80)
	shrinkSetupHeaderTickInterval(t, time.Hour) // isolate from ticker noise; only SIGWINCH should be racing Stop() here.
	drainMasterInBackground(master)

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil, want a live header")
	}

	stopSignaling := make(chan struct{})
	signalDone := make(chan struct{})
	go func() {
		defer close(signalDone)
		for {
			select {
			case <-stopSignaling:
				return
			default:
				_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)
			}
		}
	}()

	stopReturned := make(chan struct{})
	go func() {
		h.Stop()
		close(stopReturned)
	}()

	select {
	case <-stopReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5s while racing SIGWINCH")
	}
	close(stopSignaling)
	<-signalDone
}

// TestSetupHeaderNeverEmitsAltScreenOrCursorHide is the header's own version
// of TestPlayBannerAnimatesOverRealPTY's forbidden-escape check: over
// several real redraws, the header must never touch the alternate screen or
// hide the cursor (SETUP.md constraint 3 — "Ctrl-C must need no cleanup").
// There is nothing signal-specific to test here: these sequences either
// appear in the header's output or they don't, regardless of what
// eventually interrupts the process.
func TestSetupHeaderNeverEmitsAltScreenOrCursorHide(t *testing.T) {
	master, _ := prepareSetupHeaderPTYTest(t, 80)
	shrinkSetupHeaderTickInterval(t, 5*time.Millisecond)
	buf := drainMasterInBackground(master)

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil, want a live header")
	}
	idx0 := h.snapshotIdx()
	waitForIdxChange(t, h, idx0, 2*time.Second)
	h.Stop()

	time.Sleep(50 * time.Millisecond)
	output := buf.String()
	for _, forbidden := range []string{"\x1b[s", "\x1b[u", "\x1b[?1049h", "\x1b[?25l", "\x1b7", "\x1b8"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output contains forbidden escape %q: %q", forbidden, output)
		}
	}
}

// TestSetupHeaderCtrlCMidAnimationLeavesTerminalUsable drives the real
// wizard (runCLI, exactly like TestSetupPTYSIGINTRestoresRawModeAtSelectPrompt
// but with the animated header actually eligible this time — a wide,
// color-capable pty) and confirms a SIGINT delivered while the header is
// live leaves the terminal exactly as usable as it was before: raw select
// mode restored, no alternate-screen/cursor-hide escapes anywhere in the
// full transcript, and the header's own goroutine gone (activeSetupHeader
// cleared by Stop's defer unwinding through runSetupInteractive).
func TestSetupHeaderCtrlCMidAnimationLeavesTerminalUsable(t *testing.T) {
	withTestDefaultPort(t)
	t.Setenv("HOME", t.TempDir())

	master, slave := requireTestPTY(t)
	setPTYWidth(t, master, 80)
	withNoColorUnset(t)
	t.Setenv("TERM", "xterm-256color")
	shrinkSetupHeaderTickInterval(t, 5*time.Millisecond)
	withSetupPTYStdio(t, slave)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runCLI([]string{"setup"}, "test", defaultSignalContext)
	}()

	readMasterUntil(t, master, "Where should this network live?", 5*time.Second)
	if !rawSelectModeActive(t, master) {
		t.Fatal("raw select mode is not active while Q1's prompt is showing")
	}
	if activeSetupHeader.Load() == nil {
		t.Fatal("activeSetupHeader is nil while the wizard is running over a wide, color-capable pty, want a live header")
	}

	trailing := drainMasterInBackground(master)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("signal self with SIGINT: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runCLI() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setup did not return within 5s of SIGINT")
	}

	if rawSelectModeActive(t, master) {
		t.Fatal("raw select mode is still active after SIGINT")
	}
	if activeSetupHeader.Load() != nil {
		t.Fatal("activeSetupHeader is still set after the wizard returned; header.Stop() should have cleared it")
	}

	time.Sleep(50 * time.Millisecond)
	output := trailing.String()
	for _, forbidden := range []string{"\x1b[?1049h", "\x1b[?25l"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output contains forbidden escape %q after Ctrl-C: %q", forbidden, output)
		}
	}
}

// TestSetupHeaderCancelledSelectExcludesTrailingBlankRow is P1-2's own test:
// a select prompt cancelled via ctx.Done() (the same path a real SIGINT
// mid-keypress hits, TestSetupHeaderCtrlCMidAnimationLeavesTerminalUsable
// above) never reaches promptSelectInteractive's confirm/EOF Fprintln, so
// its trailing blank line is never actually printed on screen —
// reconcileSelectBodyLines must not add a row for it anyway. This is the
// row-placement assertion escape-grepping alone (the Ctrl-C test above)
// could never catch: a wrong bodyLines value produces no forbidden escape
// sequence of its own, just a header that repaints one row off the very
// next time anything redraws it. Deliberately standalone — its own
// options, driven directly through setupPromptSelect rather than the real
// wizard's Q1 — so this stays correct regardless of what
// setup_questions.go's own option text says.
func TestSetupHeaderCancelledSelectExcludesTrailingBlankRow(t *testing.T) {
	master, slave := prepareSetupHeaderPTYTest(t, 80)
	shrinkSetupHeaderTickInterval(t, time.Hour) // isolate from ticker noise; only the select's own accounting should move bodyLines.

	previousStdin := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() { os.Stdin = previousStdin })
	previousReader := promptReader
	promptReader = slave
	t.Cleanup(func() { promptReader = previousReader })

	buf := drainMasterInBackground(master)

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil, want a live header")
	}
	defer h.Stop()

	preSelectBodyLines := h.snapshotBodyLines()
	options := []selectOption{{label: "alpha"}, {label: "beta"}, {label: "gamma"}}
	ctx, cancel := context.WithCancel(context.Background())

	type outcome struct {
		index int
		err   error
	}
	resultCh := make(chan outcome, 1)
	go func() {
		index, err := setupPromptSelect(ctx, "Pick one", options, 0)
		resultCh <- outcome{index, err}
	}()

	waitForSubstring(t, buf, "Pick one", 5*time.Second)
	cancel() // simulate the SIGINT -> ctx.Done() path without a real signal.

	select {
	case res := <-resultCh:
		if !errors.Is(res.err, context.Canceled) {
			t.Fatalf("setupPromptSelect() error = %v, want context.Canceled", res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setupPromptSelect did not return within 5s of cancellation")
	}

	// Question line + one row per (unwrapped, 80-column) option -- no
	// trailing blank row, since the cancel path never printed one. The old
	// formula (preSelectBodyLines + 1 + numOptions + 1) would claim one row
	// too many here.
	want := preSelectBodyLines + 1 + len(options)
	if got := h.snapshotBodyLines(); got != want {
		t.Fatalf("bodyLines = %d after a cancelled select, want %d (preSelectBodyLines=%d); the confirm-only trailing blank row must not be counted on the cancel path", got, want, preSelectBodyLines)
	}
}

// TestSetupHeaderResumeAfterStopStartsNothing is P2-2's own test: once
// Stop() has run, a later Resume() call must not resurrect the ticker --
// stdout has already been handed back to the bare file and
// activeSetupHeader cleared, so a ticker starting after that point would be
// writing header escapes into a terminal this header no longer owns.
func TestSetupHeaderResumeAfterStopStartsNothing(t *testing.T) {
	master, _ := prepareSetupHeaderPTYTest(t, 80)
	// Drained for the test's whole duration, same reason every other test in
	// this file does it: the still-running ticker (default interval, until
	// Stop() below) would otherwise block on a full pty buffer with no
	// master reader, wedging Stop()'s own stopTicker behind that write.
	drainMasterInBackground(master)

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil, want a live header")
	}

	h.Stop()
	if h.snapshotRunning() {
		t.Fatal("ticker is still running immediately after Stop()")
	}

	h.Resume()
	time.Sleep(50 * time.Millisecond) // give a wrongly-resurrected ticker goroutine a chance to flip running.
	if h.snapshotRunning() {
		t.Fatal("Resume() after Stop() started the ticker")
	}
}

// TestSetupHeaderStopMidLineSkipsRedraw is P2-3's own test: Stop() must not
// redraw at all when the last write left the cursor mid-line (lastNL
// false) -- matching the same guard tick() and onResize() already apply.
// Without it, CUU preserves whatever column the cursor is already sitting
// at, so the header's first row would land at that column instead of
// column 0.
func TestSetupHeaderStopMidLineSkipsRedraw(t *testing.T) {
	master, _ := prepareSetupHeaderPTYTest(t, 80)
	shrinkSetupHeaderTickInterval(t, time.Hour) // no natural ticks during the test.
	buf := drainMasterInBackground(master)

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil, want a live header")
	}

	if _, err := stdout.Write([]byte("partial line, no newline (21 chars)")); err != nil {
		t.Fatalf("write partial line: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	beforeStop := len(buf.String())

	h.Stop()
	time.Sleep(50 * time.Millisecond)

	afterStop := buf.String()[beforeStop:]
	if strings.Contains(afterStop, "\x1b[2K") {
		t.Fatalf("Stop() redrew while the cursor was mid-line (lastNL=false): %q", afterStop)
	}
}
