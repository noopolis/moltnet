package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// rawSelectModeActive reports whether the terminal behind master currently
// has ECHO and ICANON both cleared -- the exact bits enableRawSelectMode
// toggles (setup_prompt_select_raw_unix.go). Mirrors masterEcho's own
// pattern (prompt_hidden_pty_test.go): querying termios through a pty's
// master fd reads back the same state the slave side sees.
func rawSelectModeActive(t *testing.T, master *os.File) bool {
	t.Helper()
	state, err := unix.IoctlGetTermios(int(master.Fd()), ioctlReadTermios)
	if err != nil {
		t.Fatalf("IoctlGetTermios(master): %v", err)
	}
	return state.Lflag&(unix.ECHO|unix.ICANON) == 0
}

// withSetupPTYStdio points os.Stdin, promptReader, and stdout at slave for
// the test's duration, restoring all three in cleanup: promptSelect's own
// real-terminal gate (promptReader == os.Stdin, setup_prompt_select.go) and
// isOutputTerminal/stdoutIsRealTTY (both keyed off the stdout var,
// style.go/banner_player.go) all require this exact set to route into
// enableRawSelectMode/promptSelectInteractive instead of the
// scripted-reader fallback withPromptAnswers uses everywhere else in this
// package -- P1-1: that fallback is exactly why the raw-mode path was
// structurally unreachable from the suite before this file.
func withSetupPTYStdio(t *testing.T, slave *os.File) {
	t.Helper()
	previousStdin := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() { os.Stdin = previousStdin })

	previousReader := promptReader
	promptReader = slave
	t.Cleanup(func() { promptReader = previousReader })

	previousStdout := stdout
	stdout = slave
	t.Cleanup(func() { stdout = previousStdout })
}

// TestSetupPTYRawModeEngagedAndRestoredAcrossWizard is the P1-1 fix's
// headline case: it drives the real `moltnet setup` question tree (runCLI,
// exactly like a human at a terminal) over a genuine pseudo-terminal end to
// end, and proves against the kernel's own termios state -- not just this
// package's bookkeeping -- that:
//
//   - enableRawSelectMode actually clears ECHO|ICANON for every select
//     question (Q1, Q4, Q5, Q6, Q7), and promptSelect's deferred restore
//     (setup_prompt_select.go:65) actually puts them back before the very
//     next, non-select question (Q2) ever reads -- the mutation P1-1 calls
//     out: deleting that defer left the suite green because nothing
//     asserted the real termios bits anywhere.
//   - Arrow-key navigation (down, down, enter) drives renderSelectOptions'
//     redraw path against a real terminal (stdoutIsRealTTY), not just the
//     print-fresh-each-time fallback a non-pty test always takes.
//   - Raw mode is off again once the whole wizard has finished.
func TestSetupPTYRawModeEngagedAndRestoredAcrossWizard(t *testing.T) {
	withInProcessRunChild(t)
	withFakeServiceManager(t, "linux")
	withTestDefaultPort(t)
	shrinkSetupHealthProbeTimeouts(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	master, slave := requireTestPTY(t)
	withSetupPTYStdio(t, slave)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runCLI([]string{"setup"}, "test", defaultSignalContext)
	}()

	// Q1 ("Where should this network live?"): raw mode is engaged before
	// promptSelectInteractive prints anything (promptSelect's own program
	// order), so seeing the question text already proves it.
	readMasterUntil(t, master, "Where should this network live?", 5*time.Second)
	if !rawSelectModeActive(t, master) {
		t.Fatal("raw select mode is not active while Q1's prompt is showing")
	}
	if _, err := master.Write([]byte("\r")); err != nil { // Q1: confirm default ("on this machine").
		t.Fatalf("confirm Q1: %v", err)
	}

	// Q2 ("Name it"): a plain typed prompt with no raw-mode gate of its
	// own -- the only thing that could have restored cooked mode by the
	// time this text appears is Q1's own deferred restore(). This is the
	// mutation-proof checkpoint.
	readMasterUntil(t, master, "Name it [local]:", 5*time.Second)
	if rawSelectModeActive(t, master) {
		t.Fatal("raw select mode is still active after Q1 confirmed; Q1's promptSelect restore() did not run before Q2 read")
	}
	if _, err := master.Write([]byte("\n")); err != nil { // Q2: empty answer (default id).
		t.Fatalf("confirm Q2: %v", err)
	}

	// Q4 ("Reachable from?"): drive arrow-key navigation -- down, down
	// (wraps back to index 0, "this machine only") -- exercising
	// renderSelectOptions' redraw path against this real terminal.
	readMasterUntil(t, master, "Reachable from?", 5*time.Second)
	if !rawSelectModeActive(t, master) {
		t.Fatal("raw select mode is not active while Q4's prompt is showing")
	}
	if _, err := master.Write([]byte("\x1b[B\x1b[B\r")); err != nil {
		t.Fatalf("navigate and confirm Q4: %v", err)
	}

	readMasterUntil(t, master, "Rooms", 5*time.Second)
	if _, err := master.Write([]byte("\r")); err != nil { // Q5: keep "general".
		t.Fatalf("confirm Q5: %v", err)
	}

	readMasterUntil(t, master, "Run as a service?", 5*time.Second)
	if _, err := master.Write([]byte("\r")); err != nil { // Q6: yes.
		t.Fatalf("confirm Q6: %v", err)
	}

	readMasterUntil(t, master, "Connect to another network?", 5*time.Second)
	if _, err := master.Write([]byte("\r")); err != nil { // Q7: not now.
		t.Fatalf("confirm Q7: %v", err)
	}

	trailing := drainMasterInBackground(master)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runCLI([]string{\"setup\"}) error = %v (trailing output: %s)", err, trailing.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("setup did not finish within 10s of Q7 being confirmed")
	}

	if rawSelectModeActive(t, master) {
		t.Fatal("raw select mode is still active after the wizard finished")
	}
	if !strings.Contains(trailing.String(), "network initialized") {
		t.Fatalf("expected the init step to run, trailing output = %q", trailing.String())
	}
}

// TestSetupPTYSIGINTRestoresRawModeAtSelectPrompt is P1-1's other half: a
// SIGINT arriving while a select question is genuinely blocked on a
// keypress (runCLI's own signal.NotifyContext, cli.go) must still leave the
// terminal restored, exactly like a normal confirm does -- the same
// guarantee promptSelectInteractive's ctx.Done() case exists for
// (setup_prompt_select.go), proved here against a real terminal rather than
// just the scripted-reader path setup_prompt_select_test.go already covers.
func TestSetupPTYSIGINTRestoresRawModeAtSelectPrompt(t *testing.T) {
	withTestDefaultPort(t)
	t.Setenv("HOME", t.TempDir())

	master, slave := requireTestPTY(t)
	withSetupPTYStdio(t, slave)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runCLI([]string{"setup"}, "test", defaultSignalContext)
	}()

	readMasterUntil(t, master, "Where should this network live?", 5*time.Second)
	if !rawSelectModeActive(t, master) {
		t.Fatal("raw select mode is not active while Q1's prompt is showing")
	}

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
		t.Fatal("raw select mode is still active after SIGINT; promptSelect's deferred restore() did not run")
	}
}

// TestRenderSelectOptionsRedrawUsesPassedPrevRowsNotFreshCount is round 3's
// direct, mutation-proof pin on the P1 fix's actual mechanism: redraw's
// cursor-up count must be exactly the prevRows argument the caller passes
// in, never a value renderSelectOptions recomputes for itself. priorRows
// here is deliberately set to a number that matches neither len(options)
// (the pre-fix formula) nor this block's own freshly-measured row count, so
// any regression back to either of those would fail this test immediately,
// without needing a wrapped option line or a real terminal-width
// coincidence to expose it.
func TestRenderSelectOptionsRedrawUsesPassedPrevRowsNotFreshCount(t *testing.T) {
	master, slave := requireTestPTY(t)
	setPTYWidth(t, master, 80)
	previousStdout := stdout
	stdout = slave
	t.Cleanup(func() { stdout = previousStdout })
	if !stdoutIsRealTTY() {
		t.Fatal("stdoutIsRealTTY() = false against a real pty slave, want true")
	}

	options := []selectOption{{label: "alpha"}, {label: "beta"}}
	buf := drainMasterInBackground(master)

	const priorRows = 5 // neither len(options) (2) nor this block's own fresh row count (also 2 at width 80).
	renderSelectOptions(80, options, 1, priorRows, true)
	time.Sleep(20 * time.Millisecond)

	want := fmt.Sprintf("\x1b[%dA", priorRows)
	if got := buf.String(); !strings.Contains(got, want) {
		t.Fatalf("renderSelectOptions(..., prevRows=%d, redraw=true) output = %q, want it to contain %q (cursor-up by the passed-in prevRows, not a freshly recomputed count)", priorRows, got, want)
	}
}

// TestPromptSelectInteractiveNarrowWidthCursorUpMatchesWrappedRows is
// round 3's headline P1 regression test: it drives the real arrow-key
// widget (promptSelectInteractive, via promptSelect) over a genuine pty at
// 50 columns with the wizard's own actual Q1 option set (reproduced
// literally, like setup_prompt_select_test.go's own copies of it) — the
// exact width/option combination the reviewer's repro used, where option
// 0's line wraps onto a second terminal row. Before the fix, every redraw
// emitted "\x1b[2A" (len(options)) regardless of wrapping, under-shooting
// the true geometry by one row and leaving the question line spliced by the
// next arrow press. After the fix, every redraw must emit
// "\x1b[<selectOptionsRows>A" instead, and the old, wrap-blind
// "\x1b[2A" must never appear at all.
func TestPromptSelectInteractiveNarrowWidthCursorUpMatchesWrappedRows(t *testing.T) {
	master, slave := requireTestPTY(t)
	setPTYWidth(t, master, 50)
	withSetupPTYStdio(t, slave)

	options := []selectOption{
		{label: "on this machine", hint: "always available, any agent that can reach it"},
		{label: "in this folder", hint: "lives with this project"},
	}
	const width = 50
	wantRows := selectOptionsRows(width, options, 0)
	if wantRows <= len(options) {
		t.Fatalf("selectOptionsRows(%d, ...) = %d, want > %d (option 0's line must wrap at this width for this test to be meaningful)", width, wantRows, len(options))
	}
	wantCursorUp := fmt.Sprintf("\x1b[%dA", wantRows)
	badCursorUp := fmt.Sprintf("\x1b[%dA", len(options)) // the pre-fix, wrap-blind formula.

	buf := drainMasterInBackground(master)
	type outcome struct {
		index int
		rows  int
		err   error
	}
	resultCh := make(chan outcome, 1)
	go func() {
		index, rows, err := promptSelect(context.Background(), "Where should this network live?", options, 0)
		resultCh <- outcome{index, rows, err}
	}()

	waitForSubstring(t, buf, "Where should this network live?", 5*time.Second)

	for i := 0; i < 4; i++ { // several arrow presses, the reviewer's own repro shape.
		if _, err := master.Write([]byte("\x1b[B")); err != nil {
			t.Fatalf("write down-arrow: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := master.Write([]byte("\r")); err != nil {
		t.Fatalf("confirm select prompt: %v", err)
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("promptSelect() error = %v", res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("promptSelect did not return within 5s of confirming")
	}

	time.Sleep(50 * time.Millisecond)
	output := buf.String()
	if !strings.Contains(output, wantCursorUp) {
		t.Fatalf("output never emitted the true wrapped cursor-up %q; the redraw is still measuring the wrong geometry: %q", wantCursorUp, output)
	}
	if strings.Contains(output, badCursorUp) {
		t.Fatalf("output emitted the old wrap-blind cursor-up %q (len(options)), want only %q: %q", badCursorUp, wantCursorUp, output)
	}

	full := stripANSI(output)
	if !strings.Contains(full, "Where should this network live?") {
		t.Fatalf("question line missing/corrupted after narrow-width arrow navigation: %q", full)
	}
	if strings.Count(full, "Where should this network live?") != 1 {
		t.Fatalf("question line duplicated (stale, under-erased content from a prior redraw): %q", full)
	}
}

// TestPromptSelectInteractiveHeightChangeBetweenPressesUsesPriorPaintRows
// exercises the redraw at the exact moment the option block's height would
// change between two consecutive redraws: a live resize arriving between
// two arrow presses. This does not corrupt the transcript, because
// promptSelectInteractive freezes width once at the start of the call
// (P2-1) rather than re-measuring it on every redraw -- so a resize mid-call
// changes nothing about this call's own arithmetic, only about how the
// *next* select prompt will measure itself. That is itself the property
// under test: paint and measure must keep agreeing with each other for a
// call's entire lifetime even when the live terminal does not cooperate.
func TestPromptSelectInteractiveHeightChangeBetweenPressesUsesPriorPaintRows(t *testing.T) {
	master, slave := requireTestPTY(t)
	setPTYWidth(t, master, 50)
	withSetupPTYStdio(t, slave)

	options := []selectOption{
		{label: "on this machine", hint: "always available, any agent that can reach it"},
		{label: "in this folder", hint: "lives with this project"},
	}
	const startWidth = 50
	wantRows := selectOptionsRows(startWidth, options, 0)

	buf := drainMasterInBackground(master)
	type outcome struct {
		index int
		rows  int
		err   error
	}
	resultCh := make(chan outcome, 1)
	go func() {
		index, rows, err := promptSelect(context.Background(), "Where should this network live?", options, 0)
		resultCh <- outcome{index, rows, err}
	}()

	waitForSubstring(t, buf, "Where should this network live?", 5*time.Second)

	if _, err := master.Write([]byte("\x1b[B")); err != nil {
		t.Fatalf("write down-arrow: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	setPTYSize(t, master, 100, 24) // widen mid-call: must not change this call's own frozen-width arithmetic.

	if _, err := master.Write([]byte("\x1b[B")); err != nil {
		t.Fatalf("write second down-arrow: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := master.Write([]byte("\r")); err != nil {
		t.Fatalf("confirm select prompt: %v", err)
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("promptSelect() error = %v", res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("promptSelect did not return within 5s of confirming")
	}

	time.Sleep(50 * time.Millisecond)
	output := buf.String()
	wantCursorUp := fmt.Sprintf("\x1b[%dA", wantRows)
	if !strings.Contains(output, wantCursorUp) {
		t.Fatalf("output never used the frozen start-of-call row count %q after a mid-call resize; a resize must not perturb this call's own arithmetic: %q", wantCursorUp, output)
	}

	full := stripANSI(output)
	if strings.Count(full, "Where should this network live?") != 1 {
		t.Fatalf("question line duplicated/corrupted after a mid-call resize: %q", full)
	}
}

// TestPromptSelectInteractiveExactWidthBoundariesNoTranscriptCorruption is
// P2-3's own headline regression test (the deferred-autowrap fix to
// advanceRows/selectOptionsRows): it drives the real arrow-key widget over a
// genuine pty at exactly 44 and 67 columns -- the two widths where option
// 1's and option 0's own rendered lines land on an exact multiple of the
// terminal width -- through three consecutive redraws, and proves every one
// of them erases exactly the true painted row count, never the pre-fix
// immediate-wrap model's off-by-one (want+1 at both widths, per the
// reviewer's own real-pty measurements: "\x1b[3A against a 2-row block" at
// 67, "\x1b[4A against 3 rows" at 44). Round 3 fixed every width in 45-66
// but regressed exactly these two, so this pins both, not just one, and
// checks all three presses rather than only the first -- a fix that only
// gets the first redraw right but drifts afterward would still fail here.
func TestPromptSelectInteractiveExactWidthBoundariesNoTranscriptCorruption(t *testing.T) {
	options := []selectOption{
		{label: "on this machine", hint: "always available, any agent that can reach it"},
		{label: "in this folder", hint: "lives with this project"},
	}

	for _, width := range []int{44, 67} {
		width := width
		t.Run(fmt.Sprintf("width=%d", width), func(t *testing.T) {
			master, slave := requireTestPTY(t)
			setPTYWidth(t, master, width)
			withSetupPTYStdio(t, slave)

			wantRows := selectOptionsRows(width, options, 0)
			wantCursorUp := fmt.Sprintf("\x1b[%dA", wantRows)
			badCursorUp := fmt.Sprintf("\x1b[%dA", wantRows+1) // round 3's regressed immediate-wrap overcount at these two widths.

			buf := drainMasterInBackground(master)
			type outcome struct {
				index int
				rows  int
				err   error
			}
			resultCh := make(chan outcome, 1)
			go func() {
				index, rows, err := promptSelect(context.Background(), "Where should this network live?", options, 0)
				resultCh <- outcome{index, rows, err}
			}()

			waitForSubstring(t, buf, "Where should this network live?", 5*time.Second)

			for i := 0; i < 3; i++ { // three presses (the impasse report's own repro shape).
				if _, err := master.Write([]byte("\x1b[B")); err != nil {
					t.Fatalf("write down-arrow %d: %v", i, err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if _, err := master.Write([]byte("\r")); err != nil {
				t.Fatalf("confirm select prompt: %v", err)
			}

			select {
			case res := <-resultCh:
				if res.err != nil {
					t.Fatalf("promptSelect() error = %v", res.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("promptSelect did not return within 5s of confirming")
			}

			time.Sleep(50 * time.Millisecond)
			output := buf.String()
			if !strings.Contains(output, wantCursorUp) {
				t.Fatalf("width %d: output never emitted the true cursor-up %q across 3 presses: %q", width, wantCursorUp, output)
			}
			if strings.Contains(output, badCursorUp) {
				t.Fatalf("width %d: output emitted the regressed off-by-one cursor-up %q, want only %q: %q", width, badCursorUp, wantCursorUp, output)
			}

			full := stripANSI(output)
			if strings.Count(full, "Where should this network live?") != 1 {
				t.Fatalf("width %d: question line duplicated/corrupted after 3 presses: %q", width, full)
			}
		})
	}
}
