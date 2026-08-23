package main

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestNoteEchoedLineAccountsForWrap is P1-1-style wrap-blindness fix,
// applied to the typed-answer path: noteEchoedLine used to always add
// exactly one row regardless of whether the echoed "question + typed
// answer" line actually wraps at the live terminal width. It must now
// measure the real row count via advanceRows, seeded from the column the
// question's own Write left the cursor at (h.col), not column 0.
func TestNoteEchoedLineAccountsForWrap(t *testing.T) {
	const width = 20
	t.Setenv("COLUMNS", strconv.Itoa(width))

	// A short answer starting at column 0 must still add exactly one row --
	// the common case, and what the old flat "+1" already got right.
	short := &setupHeader{}
	short.noteEchoedLine("short")
	if short.bodyLines != 1 {
		t.Fatalf("bodyLines = %d after a short echoed answer, want 1", short.bodyLines)
	}

	// An answer that overflows the terminal width from column 0 must wrap
	// onto a second row -- exactly what the flat "+1" model could never
	// report, regardless of answer length.
	long := &setupHeader{}
	long.noteEchoedLine(strings.Repeat("x", width+5))
	if long.bodyLines != 2 {
		t.Fatalf("bodyLines = %d after a %d-column echoed answer on a %d-column terminal, want 2 (one wrap)", long.bodyLines, width+5, width)
	}

	// Seeded from a non-zero starting column (the question's own Write
	// already advanced h.col there): an answer that only overflows *because*
	// of that starting column, not on its own, must still wrap -- proving
	// the seed is actually used, not just defaulted to 0.
	seeded := &setupHeader{col: width - 3}
	seeded.noteEchoedLine("more-than-3-chars")
	if seeded.bodyLines != 2 {
		t.Fatalf("bodyLines = %d starting from column %d, want 2 (the seeded column pushes this answer past the wrap point)", seeded.bodyLines, width-3)
	}
}

// withActiveSetupHeaderForTest wires up a *setupHeader as both stdout and
// activeSetupHeader for the duration of fn, guarded by processStateMu (the
// same lock captureStdout uses) since both are shared package vars. Returns
// the header so the caller can inspect its state afterward.
func withActiveSetupHeaderForTest(t *testing.T, fn func(h *setupHeader)) *setupHeader {
	t.Helper()
	processStateMu.Lock()
	defer processStateMu.Unlock()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})
	go io.Copy(io.Discard, r) // drain so Write never blocks.

	h := &setupHeader{file: w, animating: false}
	previousStdout := stdout
	stdout = h
	activeSetupHeader.Store(h)
	t.Cleanup(func() {
		stdout = previousStdout
		activeSetupHeader.Store(nil)
	})

	fn(h)
	return h
}

// TestSetupPromptLineCtxAccountsForEchoedLineOnlyOnSuccess pins two
// mutation-proven gaps at once: setupPromptLineCtx must call
// h.noteEchoedLine on a successful read (deleting the call left the suite
// green before this test existed), and must NOT call it on a cancelled one
// (calling it unconditionally also left the suite green).
func TestSetupPromptLineCtxAccountsForEchoedLineOnlyOnSuccess(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		h := withActiveSetupHeaderForTest(t, func(h *setupHeader) {
			previousReader := promptReader
			promptReader = strings.NewReader("typed-answer\n")
			t.Cleanup(func() { promptReader = previousReader })

			before := h.snapshotBodyLines()
			answer, err := setupPromptLineCtx(context.Background(), "  question: ")
			if err != nil {
				t.Fatalf("setupPromptLineCtx() error = %v", err)
			}
			if answer != "typed-answer" {
				t.Fatalf("setupPromptLineCtx() answer = %q, want %q", answer, "typed-answer")
			}
			if got := h.snapshotBodyLines(); got <= before {
				t.Fatalf("bodyLines = %d after a successful read, want more than the pre-read %d (the echoed line must be accounted for)", got, before)
			}
		})
		_ = h
	})

	t.Run("cancelled", func(t *testing.T) {
		withActiveSetupHeaderForTest(t, func(h *setupHeader) {
			// A reader that never produces a line: ReadString blocks
			// forever on it, so only ctx cancellation can end this read
			// (promptLineCtx's own select on ctx.Done()).
			previousReader := promptReader
			blocked, _ := io.Pipe()
			promptReader = blocked
			t.Cleanup(func() { promptReader = previousReader })

			before := h.snapshotBodyLines()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := setupPromptLineCtx(ctx, "  question: "); err == nil {
				t.Fatal("setupPromptLineCtx() error = nil, want ctx.Err() for an already-cancelled context")
			}
			if got := h.snapshotBodyLines(); got != before {
				t.Fatalf("bodyLines changed on a cancelled read: got %d, want unchanged %d", got, before)
			}
		})
	})
}

// TestRedrawStaysScrolledOffOnceSet pins scrolledOff's own stickiness
// contract directly: once redraw has set it, every later call must keep
// returning immediately without painting -- never re-evaluating the
// viewport check again, regardless of what bodyLines does afterward
// (redraw's own doc comment: bodyLines is not strictly monotonic, since
// reconcileSelectBodyLines can drop it from a mid-select peak).
func TestRedrawStaysScrolledOffOnceSet(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})
	go io.Copy(io.Discard, r)

	h := &setupHeader{
		file:        w,
		frames:      setupHeaderFrames,
		rows:        len(setupHeaderFrames[0].rows),
		lastDrawn:   -1,
		scrolledOff: true, // already tripped by an earlier redraw call.
		bodyLines:   0,    // a value that, taken alone, would not itself look "scrolled off".
	}

	h.redraw(1)

	if h.lastDrawn != -1 {
		t.Fatalf("redraw() painted (lastDrawn = %d) after scrolledOff was already set, want it to stay stuck and paint nothing", h.lastDrawn)
	}
	if !h.scrolledOff {
		t.Fatal("scrolledOff flipped back to false after a redraw call, want it to remain sticky for the header's whole remaining lifetime")
	}
}

// TestStopTickerBlocksUntilGoroutineExits pins stopTicker's own <-done wait:
// mutation-proven that removing it leaves the rest of the suite green (the
// running flag, checked under mu inside tick(), already prevents any
// *visible* redraw after Suspend/Stop, so nothing else notices). This test
// asserts the exact contract that wait provides directly: by the instant
// stopTicker() returns, doneCh must already be closed -- not "will close
// soon" -- so a caller relying on "zero chance of a tick landing after this
// call returns" (the doc comment's own promise, e.g. a raw-mode prompt about
// to take over the terminal) is not racing the ticker goroutine's own exit.
func TestStopTickerBlocksUntilGoroutineExits(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})
	go io.Copy(io.Discard, r)

	h := &setupHeader{file: w, frames: setupHeaderFrames, animating: true, lastNL: true}
	h.startTicker()
	h.stopTicker()

	select {
	case <-h.doneCh:
		// Closed already, as stopTicker's own contract requires.
	default:
		t.Fatal("stopTicker() returned before its ticker goroutine's doneCh was closed")
	}
}
