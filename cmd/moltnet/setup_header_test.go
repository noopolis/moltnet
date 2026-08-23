package main

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestNextPingPongIndexBouncesAtBothEnds pins the idle-sway motion
// nextPingPongIndex drives: it advances by dir until it would leave [0,
// n-1], then reverses at that boundary, exactly the "ping-pongs it for the
// full swing" motion assets/AGENTS.md describes for banner-frames-loop.txt.
func TestNextPingPongIndexBouncesAtBothEnds(t *testing.T) {
	const n = 4 // indices 0..3
	idx, dir := 0, 1
	var seen []int
	for i := 0; i < 10; i++ {
		idx, dir = nextPingPongIndex(idx, dir, n)
		seen = append(seen, idx)
	}
	want := []int{1, 2, 3, 2, 1, 0, 1, 2, 3, 2}
	for i, w := range want {
		if seen[i] != w {
			t.Fatalf("sequence = %v, want %v (diverged at step %d)", seen, want, i)
		}
	}
}

// TestNextPingPongIndexDegenerateSingleFrame covers n<=1: there is nothing
// to bounce between, so the index must always stay 0 rather than panic on
// an empty frame set.
func TestNextPingPongIndexDegenerateSingleFrame(t *testing.T) {
	for _, n := range []int{0, 1} {
		idx, dir := nextPingPongIndex(0, 1, n)
		if idx != 0 {
			t.Fatalf("nextPingPongIndex(0, 1, %d) idx = %d, want 0", n, idx)
		}
		idx, _ = nextPingPongIndex(idx, dir, n)
		if idx != 0 {
			t.Fatalf("second call with n=%d: idx = %d, want 0", n, idx)
		}
	}
}

// TestSetupHeaderFramesParseCleanly pins the embedded
// assets/banner-frames-loop.txt against the same parser/validator
// banner_frames_test.go pins banner-frames.txt against — a regression guard
// on the asset itself, independent of any terminal.
func TestSetupHeaderFramesParseCleanly(t *testing.T) {
	if setupHeaderFramesErr != nil {
		t.Fatalf("setupHeaderFramesErr = %v, want nil", setupHeaderFramesErr)
	}
	if len(setupHeaderFrames) < 2 {
		t.Fatalf("len(setupHeaderFrames) = %d, want at least 2 for an animation", len(setupHeaderFrames))
	}
	wantRows := len(setupHeaderFrames[0].rows)
	for i, f := range setupHeaderFrames {
		if len(f.rows) != wantRows {
			t.Fatalf("setupHeaderFrames[%d] has %d rows, want %d (frame 0's row count)", i, len(f.rows), wantRows)
		}
	}
	if setupHeaderFrameWidth <= 0 {
		t.Fatalf("setupHeaderFrameWidth = %d, want > 0", setupHeaderFrameWidth)
	}
}

// TestStartSetupHeaderPrintCommandsStartsNothing is the print-commands half
// of "no header, no ticker, no goroutine": even with printCommandsOnly
// forced over a stdout that would otherwise be fully eligible to animate
// (isOutputTerminal forced true), startSetupHeader must return nil and
// write nothing at all before ever inspecting the terminal further.
func TestStartSetupHeaderPrintCommandsStartsNothing(t *testing.T) {
	previous := isOutputTerminal
	isOutputTerminal = func() bool { return true }
	t.Cleanup(func() { isOutputTerminal = previous })

	output := captureStdout(t, func() {
		h := startSetupHeader(true)
		if h != nil {
			t.Fatal("startSetupHeader(true) != nil, want nil for --print-commands")
		}
	})
	if output != "" {
		t.Fatalf("output = %q, want no output at all for --print-commands", output)
	}
	if activeSetupHeader.Load() != nil {
		t.Fatal("activeSetupHeader was set for --print-commands, want it to stay nil")
	}
}

// TestStartSetupHeaderNonTerminalStartsNothing covers the plain, by-far-most-
// common case every other setup test already runs under: a piped stdout
// (captureStdout's os.Pipe, never a real terminal) with isOutputTerminal
// left at its real, unmocked implementation. startSetupHeader must produce
// no output and no header at all — the exact behavior every pre-existing
// setup test already implicitly depends on continuing to hold.
func TestStartSetupHeaderNonTerminalStartsNothing(t *testing.T) {
	output := captureStdout(t, func() {
		h := startSetupHeader(false)
		if h != nil {
			t.Fatal("startSetupHeader(false) != nil over a non-terminal stdout, want nil")
		}
	})
	if output != "" {
		t.Fatalf("output = %q, want no output over a non-terminal stdout", output)
	}
}

// TestStartSetupHeaderDegradesToStaticOutsideRealTTY forces isOutputTerminal
// true but leaves stdout an ordinary pipe: stdoutIsRealTTY() stays false
// (fdFile is satisfied, but isatty itself correctly says no), so
// setupHeaderEligible() is false and startSetupHeader must fall back to the
// same static dim(bannerText) output printBanner's own gate produces —
// never a live header, never a ticker.
func TestStartSetupHeaderDegradesToStaticOutsideRealTTY(t *testing.T) {
	previous := isOutputTerminal
	isOutputTerminal = func() bool { return true }
	t.Cleanup(func() { isOutputTerminal = previous })
	withNoColorUnset(t)
	t.Setenv("TERM", "xterm-256color")

	output := captureStdout(t, func() {
		h := startSetupHeader(false)
		if h != nil {
			t.Fatal("startSetupHeader(false) != nil outside a real TTY, want nil (static fallback only)")
		}
	})
	want := staticBannerOutput(t)
	if got := stripANSI(output); got != want {
		t.Fatalf("output (ANSI stripped) = %q, want the static banner output %q", got, want)
	}
	if !strings.Contains(output, "\x1b[2m") {
		t.Fatalf("output = %q, want the dim styling ANSI code", output)
	}
	if activeSetupHeader.Load() != nil {
		t.Fatal("activeSetupHeader was set outside a real TTY, want it to stay nil")
	}
}

// TestSetupPromptSelectAndLineCtxAreNoOpWrappersWithoutAHeader proves
// setupPromptSelect/setupPromptLineCtx behave identically to calling
// promptSelect/promptLineCtx directly whenever activeSetupHeader is nil —
// the state every non-PTY test in this package (and --print-commands, and
// the non-interactive refusal) runs under.
func TestSetupPromptSelectAndLineCtxAreNoOpWrappersWithoutAHeader(t *testing.T) {
	activeSetupHeader.Store(nil) // the default; explicit for clarity/isolation.

	withPromptAnswers(t, "2", "typed-answer")
	options := []selectOption{{label: "alpha"}, {label: "beta"}}

	output := captureStdout(t, func() {
		index, err := setupPromptSelect(context.Background(), "Pick one", options, 0)
		if err != nil {
			t.Fatalf("setupPromptSelect() error = %v", err)
		}
		if index != 1 {
			t.Fatalf("setupPromptSelect() index = %d, want 1 (\"beta\")", index)
		}
		answer, err := setupPromptLineCtx(context.Background(), "  typed: ")
		if err != nil {
			t.Fatalf("setupPromptLineCtx() error = %v", err)
		}
		if answer != "typed-answer" {
			t.Fatalf("setupPromptLineCtx() answer = %q, want %q", answer, "typed-answer")
		}
	})
	if !strings.Contains(output, "Pick one") || !strings.Contains(output, "beta") {
		t.Fatalf("output = %q, want the plain-fallback prompt transcript", output)
	}
}

// TestSetupHeaderMethodsNilSafe pins every exported *setupHeader method as a
// safe no-op on a nil receiver — the degraded/print-commands/non-terminal
// contract every call site in setup.go/setup_questions.go relies on without
// its own nil check.
func TestSetupHeaderMethodsNilSafe(t *testing.T) {
	var h *setupHeader
	h.Suspend()
	h.Resume()
	h.noteEchoedLine("answer")
	h.Stop()
	h.Stop() // idempotent even when nil.
}

// TestFdFileAcceptsBareOSFile is a narrow regression pin on the fdFile
// generalization itself (banner_player.go): every existing caller passing a
// literal *os.File must still satisfy it, independent of setupHeader.
func TestFdFileAcceptsBareOSFile(t *testing.T) {
	var f *os.File = os.Stdout
	var _ fdFile = f
}

// TestAdvanceRowsWrappedLineAdvancesByTrueRowCount is P2-1's own test: a
// single 100-character line (no newline at all) at column 0 on a 45-column
// terminal must consume 2 additional rows before wrapping a third time
// would be needed (row 1: cols 1-45, row 2: cols 46-90, row 3: cols 91-100 —
// two row *transitions*, ending mid-row-3 at column 10), not the 0 the old
// newline-only model (bytes.Count of '\n') would have reported for a line
// with no '\n' in it at all. Mirrors the reviewer's own real-pty
// measurement (100 chars / 45 cols) exactly.
func TestAdvanceRowsWrappedLineAdvancesByTrueRowCount(t *testing.T) {
	const width = 45
	line := strings.Repeat("x", 100)

	newCol, rows, pending := advanceRows(0, width, false, line)
	if rows != 2 {
		t.Fatalf("advanceRows(0, %d, 100 chars) rows = %d, want 2", width, rows)
	}
	if newCol != 10 {
		t.Fatalf("advanceRows(0, %d, 100 chars) newCol = %d, want 10", width, newCol)
	}
	if pending {
		t.Fatalf("advanceRows(0, %d, 100 chars) pendingWrap = true, want false (col 10 has not reached width %d)", width, width)
	}
}

// TestAdvanceRowsPlainNewlineStillCountsOneRow pins the common, unwrapped
// case (a short line well under the width) still behaves exactly like the
// old bytes.Count(p, "\n") model: one '\n' is one row, full stop.
func TestAdvanceRowsPlainNewlineStillCountsOneRow(t *testing.T) {
	newCol, rows, _ := advanceRows(0, 80, false, "short line\n")
	if rows != 1 {
		t.Fatalf("advanceRows short line rows = %d, want 1", rows)
	}
	if newCol != 0 {
		t.Fatalf("advanceRows short line newCol = %d, want 0", newCol)
	}
}

// TestAdvanceRowsSkipsANSISGREscapes proves a dim()/green()/yellow()-styled
// span (style.go's only escape shape, "\x1b[<code>m...\x1b[0m") is not
// counted as columns at all: a styled word that easily fits the width must
// not wrap just because its escape bytes were counted as if they were
// visible characters.
func TestAdvanceRowsSkipsANSISGREscapes(t *testing.T) {
	styled := "\x1b[33m" + "warning" + "\x1b[0m" + ": " + strings.Repeat("y", 30)
	plain := "warning: " + strings.Repeat("y", 30)

	_, styledRows, _ := advanceRows(0, 40, false, styled)
	_, plainRows, _ := advanceRows(0, 40, false, plain)
	if styledRows != plainRows {
		t.Fatalf("styled rows = %d, plain rows = %d, want equal (escapes must be zero-width)", styledRows, plainRows)
	}
	if styledRows != 0 {
		t.Fatalf("rows = %d, want 0 (the visible text alone fits in 40 columns)", styledRows)
	}
}

// TestAdvanceRowsUnknownWidthDisablesWrapping covers width<=0 (terminalWidth
// could not determine a live width): wrapping must be disabled entirely,
// degrading to the old newline-only model rather than fabricating a wrap
// point at some arbitrary width.
func TestAdvanceRowsUnknownWidthDisablesWrapping(t *testing.T) {
	newCol, rows, pending := advanceRows(0, 0, false, strings.Repeat("z", 500))
	if rows != 0 {
		t.Fatalf("rows = %d, want 0 with width unknown (no '\\n' in the input)", rows)
	}
	if newCol != 500 {
		t.Fatalf("newCol = %d, want 500 (column still tracked even with wrapping disabled)", newCol)
	}
	if pending {
		t.Fatalf("pendingWrap = true, want false (wrapping disabled entirely when width is unknown)")
	}
}

// TestAdvanceRowsSkipsNonSGREscapesInsteadOfDiscardingTheTail is P2-1's own
// test, reproducing the reviewer's exact real-pty measurements: the old CSI
// scan only recognized 'm' (SGR) as a terminator, so a cursor-motion escape
// like "\x1b[3A" or "\x1b[2K" (final byte 'A'/'K', never 'm') was
// misdiagnosed as unterminated and caused the function to return early,
// discarding every byte written after it in the same call -- including its
// newlines.
func TestAdvanceRowsSkipsNonSGREscapesInsteadOfDiscardingTheTail(t *testing.T) {
	if _, rows, _ := advanceRows(0, 80, false, "\x1b[3A"+"line one\nline two\n"); rows != 2 {
		t.Fatalf(`advanceRows(0, 80, "\x1b[3A"+"line one\nline two\n") rows = %d, want 2`, rows)
	}
	if _, rows, _ := advanceRows(0, 80, false, "\r\x1b[2K  installing...\n"); rows != 1 {
		t.Fatalf(`advanceRows(0, 80, "\r\x1b[2K  installing...\n") rows = %d, want 1`, rows)
	}
}

// TestAdvanceRowsGenuinelyUnterminatedCSIKeepsPriorCount pins the tail case
// P2-1's fix still has to handle: a CSI introducer with no final byte
// anywhere in the rest of the string at all (never actually produced by
// this codebase's own writers, but not something to guess at either).
// Whatever was correctly counted before it must survive, even though
// nothing after the escape can be.
func TestAdvanceRowsGenuinelyUnterminatedCSIKeepsPriorCount(t *testing.T) {
	newCol, rows, _ := advanceRows(0, 80, false, "line one\n"+"\x1b[123")
	if rows != 1 {
		t.Fatalf("rows = %d, want 1 (the complete line before the unterminated escape)", rows)
	}
	if newCol != 0 {
		t.Fatalf("newCol = %d, want 0 (reset by the '\\n' before the unterminated escape)", newCol)
	}
}

// TestAdvanceRowsExactWidthLineThenNewlineConsumesOneRowNotTwo is P2-3's
// direct pin on the deferred (pending) autowrap fix itself: a line landing
// exactly on the last column, immediately followed by '\n', must consume
// only the one row the '\n' itself contributes -- not a second row for
// "reaching the width", which is what the pre-fix immediate-wrap model
// charged (and exactly the bug that broke widths 44 and 67 for the wizard's
// own Q1 option lines).
func TestAdvanceRowsExactWidthLineThenNewlineConsumesOneRowNotTwo(t *testing.T) {
	const width = 50
	newCol, rows, pending := advanceRows(0, width, false, strings.Repeat("x", width)+"\n")
	if rows != 1 {
		t.Fatalf("rows = %d, want 1 (an exact-width line plus '\\n' is one row, not two)", rows)
	}
	if newCol != 0 {
		t.Fatalf("newCol = %d, want 0", newCol)
	}
	if pending {
		t.Fatalf("pendingWrap = true, want false ('\\n' must resolve/clear any pending wrap)")
	}
}

// TestAdvanceRowsExactWidthLineThenMoreTextWrapsOnceOnNextChar is the other
// half of the same fix: when a visible character (not '\n') arrives right
// after a line has exactly filled the width, deferred autowrap says *that*
// character is what actually resolves the wrap, landing on the new row --
// so the total is still just one additional row, charged once, not at the
// moment the width was reached.
func TestAdvanceRowsExactWidthLineThenMoreTextWrapsOnceOnNextChar(t *testing.T) {
	const width = 50
	newCol, rows, pending := advanceRows(0, width, false, strings.Repeat("x", width)+"y")
	if rows != 1 {
		t.Fatalf("rows = %d, want 1 (the resolving character wraps exactly once)", rows)
	}
	if newCol != 1 {
		t.Fatalf("newCol = %d, want 1 (the resolving character itself is the first column of the new row)", newCol)
	}
	if pending {
		t.Fatalf("pendingWrap = true, want false (already resolved by the 'y')")
	}
}

// TestAdvanceRowsPendingWrapPersistsAcrossCalls is the cross-call half of
// the same fix (mirroring col's own existing cross-call contract, P2-1):
// filling exactly to width in one call, with nothing after it, must leave
// pendingWrap true for the *next* call to resolve -- not lose the state at
// the call boundary the way a version that only tracked col (and reset it
// to 0 immediately on reaching width) would.
func TestAdvanceRowsPendingWrapPersistsAcrossCalls(t *testing.T) {
	const width = 50
	col1, rows1, pending1 := advanceRows(0, width, false, strings.Repeat("x", width))
	if rows1 != 0 {
		t.Fatalf("first call rows = %d, want 0 (nothing follows the exact-width fill yet)", rows1)
	}
	if !pending1 {
		t.Fatalf("first call pendingWrap = false, want true (must carry the pending wrap into the next Write call)")
	}

	col2, rows2, pending2 := advanceRows(col1, width, pending1, "\n")
	if rows2 != 1 {
		t.Fatalf("second call rows = %d, want 1 (the '\\n' consumes exactly one row, resolving the pending wrap for free)", rows2)
	}
	if col2 != 0 || pending2 {
		t.Fatalf("second call (col, pendingWrap) = (%d, %v), want (0, false)", col2, pending2)
	}
}

// TestAdvanceRowsCSIEscapeNeverResolvesAPendingWrap is the regression guard
// on the invariant the whole P2-3 fix rests on: an ANSI escape is
// zero-width, so it must never be treated as the "next visible character"
// that resolves a pending wrap, even though the default (visible-character)
// branch right below the CSI branch does exactly that. Mutation-proven
// unguarded: making the CSI branch also resolve pendingWrap (rows++; col =
// 0; pendingWrap = false, mirroring the default branch) leaves the entire
// shipped cmd/moltnet suite green, while a real-pty sweep fails at exactly
// widths 44 and 67 -- the wizard's Q1 option lines end with a dim(hint)
// reset ("\x1b[0m") that lands exactly on the pending-wrap boundary at
// those two widths on every single redraw. Both an SGR escape (style.go's
// own "\x1b[0m") and a non-SGR one (a cursor-motion escape like "\x1b[3A")
// must leave the wrap pending, since neither is a visible character.
func TestAdvanceRowsCSIEscapeNeverResolvesAPendingWrap(t *testing.T) {
	const width = 50

	check := func(t *testing.T, s string) {
		t.Helper()
		newCol, rows, pending := advanceRows(0, width, false, s)
		if newCol != width {
			t.Fatalf("newCol = %d, want %d (an escape must not advance col)", newCol, width)
		}
		if rows != 0 {
			t.Fatalf("rows = %d, want 0 (an escape must not resolve/charge a pending wrap)", rows)
		}
		if !pending {
			t.Fatal("pendingWrap = false, want true (an escape is zero-width and must not resolve a pending wrap)")
		}
	}

	t.Run("SGR reset", func(t *testing.T) {
		check(t, strings.Repeat("x", width)+"\x1b[0m")
	})
	t.Run("non-SGR cursor motion", func(t *testing.T) {
		check(t, strings.Repeat("x", width)+"\x1b[3A")
	})
}

// TestSetupHeaderWriteExactWidthLineAcrossTwoCallsCountsOneRow is P2-3's
// fix verified at setupHeader.Write's own level (item 5 of the impasse
// report's verification list): bodyLines -- the count redraw's own
// cursor-up math depends on -- must not overcount an exactly-width
// transcript line any more than advanceRows itself does, and the pending
// wrap must survive across two separate Write calls exactly like h.col
// already does (the struct doc comment's own contract).
func TestSetupHeaderWriteExactWidthLineAcrossTwoCallsCountsOneRow(t *testing.T) {
	const width = 50
	t.Setenv("COLUMNS", strconv.Itoa(width))

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})
	go io.Copy(io.Discard, r) // drain so Write never blocks on a full pipe buffer.

	h := &setupHeader{file: w}

	// First call fills exactly `width` columns with no trailing newline: the
	// wrap must be deferred, not charged yet.
	if _, err := h.Write([]byte(strings.Repeat("x", width))); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if !h.pendingWrap {
		t.Fatal("pendingWrap = false after filling exactly one line, want true (deferred wrap must persist across Write calls)")
	}
	if h.bodyLines != 0 {
		t.Fatalf("bodyLines = %d after the exact-width fill alone, want 0 (nothing has actually wrapped yet)", h.bodyLines)
	}

	// Second call is just the line's own trailing '\n', arriving in a
	// separate Write the way a real caller's Fprintln would after this
	// package's own dim()/green() helpers already flushed the styled span.
	if _, err := h.Write([]byte("\n")); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if h.bodyLines != 1 {
		t.Fatalf("bodyLines = %d after the trailing '\\n', want 1 (an exactly-width line is one terminal row, not two)", h.bodyLines)
	}
	if h.pendingWrap {
		t.Fatal("pendingWrap = true after '\\n', want false")
	}
	if h.col != 0 {
		t.Fatalf("col = %d after '\\n', want 0", h.col)
	}
}
