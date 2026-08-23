package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// setPTYSize mirrors setPTYWidth (banner_player_test.go) but also controls
// the row count, needed for TestSetupHeaderStopsRepaintingOnceViewportReached
// (P1-2): setPTYWidth's own hardcoded Row:24 is fine for every test that
// only cares about width.
func setPTYSize(t *testing.T, master *os.File, cols, rows int) {
	t.Helper()
	ws := &unix.Winsize{Row: uint16(rows), Col: uint16(cols)}
	if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		t.Fatalf("set pty size to %dx%d: %v", cols, rows, err)
	}
}

// snapshotIdx is a test-only accessor into setupHeader's animation state,
// synchronized through the same mu every real reader/writer of these
// fields uses — so reading it concurrently with the ticker goroutine (or
// Suspend/Resume racing it) is race-detector clean, not just convenient.
func (h *setupHeader) snapshotIdx() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.idx
}

// snapshotRunning mirrors snapshotIdx for the ticker's running flag.
func (h *setupHeader) snapshotRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

// prepareSetupHeaderPTYTest allocates a fresh pty (requireTestPTY,
// openpty_test.go), sets its width to cols, points stdout at the slave
// side, and forces the styling gates setupHeaderEligible needs (NO_COLOR
// unset, TERM capable) — the same recipe preparePTYBannerTest
// (banner_player_test.go) uses for playBanner's own pty tests, minus the
// drain: callers here mostly want to read specific bytes with
// readMasterUntil rather than passively drain everything.
func prepareSetupHeaderPTYTest(t *testing.T, cols int) (master, slave *os.File) {
	t.Helper()
	master, slave = requireTestPTY(t)
	setPTYWidth(t, master, cols)

	previousStdout := stdout
	stdout = slave
	t.Cleanup(func() { stdout = previousStdout })

	withNoColorUnset(t)
	t.Setenv("TERM", "xterm-256color")

	if !stdoutIsRealTTY() {
		t.Fatal("stdoutIsRealTTY() = false against a real pty slave, want true")
	}
	return master, slave
}

// shrinkSetupHeaderTickInterval speeds up the ticker for a test's duration.
func shrinkSetupHeaderTickInterval(t *testing.T, d time.Duration) {
	t.Helper()
	previous := setupHeaderTickInterval
	setupHeaderTickInterval = d
	t.Cleanup(func() { setupHeaderTickInterval = previous })
}

// TestSetupHeaderAnimatesAndFrameChangesOverTime is this unit's headline
// animation test: over a real, wide-enough, color-capable pty,
// startSetupHeader must return a live header whose frame index actually
// advances over time, and whose redraws are visible on the wire as real
// escape sequences — not just a single static paint.
func TestSetupHeaderAnimatesAndFrameChangesOverTime(t *testing.T) {
	master, _ := prepareSetupHeaderPTYTest(t, 80)
	shrinkSetupHeaderTickInterval(t, 5*time.Millisecond)
	buf := drainMasterInBackground(master)

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil over a wide, color-capable real pty, want a live header")
	}
	defer h.Stop()

	idx0 := h.snapshotIdx()
	deadline := time.Now().Add(2 * time.Second)
	for h.snapshotIdx() == idx0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := h.snapshotIdx(); got == idx0 {
		t.Fatalf("frame index did not change from %d within 2s of ticking every %s", idx0, setupHeaderTickInterval)
	}

	time.Sleep(50 * time.Millisecond) // let the last redraw's bytes land on the master.
	output := buf.String()
	if !strings.Contains(output, "\x1b[2K") {
		t.Fatalf("output has no line-clear redraw escape at all: %q", output)
	}
	if strings.Count(output, "\x1b[2K") < len(h.frames[0].rows) {
		t.Fatalf("output has fewer line-clear escapes than one frame's worth of rows: %q", output)
	}
}

// TestSetupHeaderTickerSuspendedDuringPromptRead is SETUP.md constraint 1's
// own test, and the property the task brief calls "the single most
// important correctness property in this unit": while setupPromptSelect's
// underlying read is genuinely blocked waiting on a keypress, the ticker
// must not fire at all — no frame-index change, and no bytes written to the
// pty — for as long as the prompt is outstanding. Once the prompt is
// confirmed, the ticker must resume and the frame index must move again.
func TestSetupHeaderTickerSuspendedDuringPromptRead(t *testing.T) {
	master, slave := prepareSetupHeaderPTYTest(t, 80)
	shrinkSetupHeaderTickInterval(t, 5*time.Millisecond)

	previousStdin := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() { os.Stdin = previousStdin })
	previousReader := promptReader
	promptReader = slave
	t.Cleanup(func() { promptReader = previousReader })

	// Drained continuously for the test's whole duration, started before
	// the header even paints its first frame: a real terminal always keeps
	// reading its pty master, and at a 5ms tick interval this test runs
	// long enough that anything less than continuous draining fills the
	// pty's kernel buffer and makes the ticker's own write() block forever
	// — a self-inflicted deadlock that has nothing to do with the property
	// under test. See waitForSubstring's doc comment for why this rules out
	// readMasterUntil here.
	buf := drainMasterInBackground(master)

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil, want a live header")
	}
	defer h.Stop()

	// Let a couple of idle ticks happen first, so we know the ticker is
	// genuinely alive before we ever try to observe it going quiet.
	idxBeforePrompt := h.snapshotIdx()
	waitForIdxChange(t, h, idxBeforePrompt, 2*time.Second)

	options := []selectOption{{label: "alpha"}, {label: "beta"}}
	resultCh := make(chan struct {
		index int
		err   error
	}, 1)
	go func() {
		index, err := setupPromptSelect(context.Background(), "Pick one", options, 0)
		resultCh <- struct {
			index int
			err   error
		}{index, err}
	}()

	waitForSubstring(t, buf, "Pick one", 5*time.Second)

	// The prompt's own Suspend() call happens synchronously before it ever
	// writes "Pick one" (setupPromptSelect's program order), so the ticker
	// is already stopped by the time this text is visible.
	if h.snapshotRunning() {
		t.Fatal("ticker is still running once the select prompt's question text is visible; Suspend() did not run before the read")
	}

	idxDuringPrompt := h.snapshotIdx()
	lenBeforePause := len(buf.String())
	time.Sleep(10 * setupHeaderTickInterval) // several tick intervals' worth of "nothing should happen".

	if got := h.snapshotIdx(); got != idxDuringPrompt {
		t.Fatalf("frame index changed from %d to %d while the select prompt was blocked reading; the ticker must stay suspended", idxDuringPrompt, got)
	}
	if h.snapshotRunning() {
		t.Fatal("ticker is running while the select prompt is blocked reading a keypress")
	}
	if got := buf.String(); len(got) != lenBeforePause {
		t.Fatalf("the header wrote %d more byte(s) (%q) while the select prompt was blocked reading a keypress; the ticker must make no writes during a suspended prompt", len(got)-lenBeforePause, got[lenBeforePause:])
	}

	if _, err := master.Write([]byte("\r")); err != nil { // confirm the default option.
		t.Fatalf("confirm select prompt: %v", err)
	}
	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("setupPromptSelect() error = %v", res.err)
		}
		if res.index != 0 {
			t.Fatalf("setupPromptSelect() index = %d, want 0 (default)", res.index)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setupPromptSelect did not return within 5s of confirming")
	}

	if !h.snapshotRunning() {
		t.Fatal("ticker did not resume after the select prompt returned")
	}
	waitForIdxChange(t, h, h.snapshotIdx(), 2*time.Second)
}

// waitForIdxChange polls h's frame index until it differs from from, or
// fails the test after timeout.
func waitForIdxChange(t *testing.T, h *setupHeader, from int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h.snapshotIdx() != from {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("frame index did not change from %d within %s", from, timeout)
}

// waitForSubstring polls buf (a drainMasterInBackground syncBuffer) until it
// contains substr, or fails the test after timeout. Used instead of
// readMasterUntil whenever a test also needs a *continuous* background
// drain of the same master for its whole duration (readMasterUntil does its
// own one-shot master.Read loop and would race a concurrent drain goroutine
// for the same bytes) — continuous draining matters here because these
// tests run the ticker at a very short interval for several seconds, and a
// real terminal always keeps reading its pty master, so a test that stops
// draining mid-way is what would deadlock the ticker's own writes, not
// anything under test.
func waitForSubstring(t *testing.T, buf *syncBuffer, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), substr) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %q; got %q", timeout, substr, buf.String())
}

// TestSetupHeaderArrowKeysDoNotInflateBodyLines is P1-1's own test: driving
// a real select prompt through several arrow presses (its own in-place,
// net-zero-rows redraw) must leave bodyLines at exactly the select region's
// true final size — preSelectBodyLines + 1 (question) + len(options) +
// 1 (confirm's trailing blank line) — not inflated by len(options) per
// keypress the way counting each redraw's newlines did before the fix.
func TestSetupHeaderArrowKeysDoNotInflateBodyLines(t *testing.T) {
	master, slave := prepareSetupHeaderPTYTest(t, 80)
	shrinkSetupHeaderTickInterval(t, time.Hour) // isolate from ticker noise; only the select's own redraws should move bodyLines.

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
	resultCh := make(chan struct {
		index int
		err   error
	}, 1)
	go func() {
		index, err := setupPromptSelect(context.Background(), "Pick one", options, 0)
		resultCh <- struct {
			index int
			err   error
		}{index, err}
	}()

	waitForSubstring(t, buf, "Pick one", 5*time.Second)

	for i := 0; i < 4; i++ { // 4 down-arrow presses, exactly the reviewer's own repro.
		if _, err := master.Write([]byte("\x1b[B")); err != nil {
			t.Fatalf("write down-arrow: %v", err)
		}
		time.Sleep(20 * time.Millisecond) // let each redraw round-trip before the next press.
	}
	if _, err := master.Write([]byte("\r")); err != nil {
		t.Fatalf("confirm select prompt: %v", err)
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("setupPromptSelect() error = %v", res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setupPromptSelect did not return within 5s of confirming")
	}

	want := preSelectBodyLines + 1 + len(options) + 1
	if got := h.snapshotBodyLines(); got != want {
		t.Fatalf("bodyLines = %d after 4 arrow presses on a %d-option select, want %d (preSelectBodyLines=%d)", got, len(options), want, preSelectBodyLines)
	}
}

// TestSetupHeaderStopsRepaintingOnceViewportReached is P1-2's own test: once
// rows+bodyLines reaches the pty's actual row count, redraw must stop
// emitting anything at all — no more "\x1b[2K" line-clear escapes — for the
// rest of the run, rather than continuing to redraw into cursor-up
// sequences the terminal silently clamps at its top margin.
func TestSetupHeaderStopsRepaintingOnceViewportReached(t *testing.T) {
	master, _ := prepareSetupHeaderPTYTest(t, 80)
	const viewportRows = 12
	setPTYSize(t, master, 80, viewportRows)
	shrinkSetupHeaderTickInterval(t, 5*time.Millisecond)
	buf := drainMasterInBackground(master)

	h := startSetupHeader(false)
	if h == nil {
		t.Fatal("startSetupHeader(false) = nil, want a live header")
	}
	defer h.Stop()

	// Confirm the ticker is genuinely redrawing before the viewport fills --
	// otherwise "no escapes after the threshold" below would be vacuously
	// true for the wrong reason (the ticker never running at all).
	waitForIdxChange(t, h, h.snapshotIdx(), 2*time.Second)
	if !strings.Contains(buf.String(), "\x1b[2K") {
		t.Fatalf("no redraw observed before the viewport fills: %q", buf.String())
	}

	headerRows := len(h.frames[0].rows)
	for h.snapshotBodyLines()+headerRows < viewportRows {
		if _, err := stdout.Write([]byte("x\n")); err != nil {
			t.Fatalf("write transcript line: %v", err)
		}
	}

	time.Sleep(20 * time.Millisecond)
	marker := len(buf.String())
	time.Sleep(10 * setupHeaderTickInterval) // several tick intervals' worth of "nothing should happen".

	after := buf.String()[marker:]
	if strings.Contains(after, "\x1b[2K") {
		t.Fatalf("header kept repainting after rows+bodyLines reached the %d-row viewport: %q", viewportRows, after)
	}
}

// TestTerminalSizeZeroRowDoesNotInvalidateCols is P3-1's own test: a real
// pty can legitimately report Row:0 with Col perfectly good (a real
// IoctlSetWinsize{Row:0,Col:80} round-trips exactly as set — no kernel
// validation rejects it), and the pre-fix terminalSize folded both into one
// combined ok, so a zero Row silently took a good Col down with it. Every
// `moltnet init` banner and the setup header would then go static on such a
// terminal, since terminalWidth's own COLUMNS fallback is never actually
// exported by shells and so never rescues it.
func TestTerminalSizeZeroRowDoesNotInvalidateCols(t *testing.T) {
	master, _ := prepareSetupHeaderPTYTest(t, 80)
	setPTYSize(t, master, 80, 0)

	cols, rows, colsOK, rowsOK := terminalSize()
	if !colsOK || cols != 80 {
		t.Fatalf("terminalSize() cols = (%d, %v), want (80, true) even with Row:0", cols, colsOK)
	}
	if rowsOK {
		t.Fatalf("terminalSize() rows = (%d, %v), want rowsOK = false for a genuine Row:0", rows, rowsOK)
	}

	width, ok := terminalWidth()
	if !ok || width != 80 {
		t.Fatalf("terminalWidth() = (%d, %v), want (80, true) -- a zero Row must not take a good Col down with it", width, ok)
	}
}
