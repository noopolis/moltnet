package main

import (
	"fmt"
	"strings"
	"time"
)

// setup_header_render.go continues setup_header.go (type, gating, Start,
// Write/Fd all live there): everything below is the render/ticker/lifecycle
// half — painting, the ping-pong redraw, the ticker goroutine, Suspend/
// Resume/Stop, and the two header-aware prompt wrappers — split out purely
// to keep each file under this repo's 400-line limit.

// paintInitial writes the header's first frame with no cursor movement at
// all — there is nothing above it yet to move up over, unlike every
// subsequent redraw. Called before stdout is repointed at h, so this writes
// straight to the real file directly (never through Write: this is the
// header's own content, not transcript body).
func (h *setupHeader) paintInitial() {
	var b strings.Builder
	for _, row := range h.frames[0].rows {
		b.WriteString(dim(row))
		b.WriteByte('\n')
	}
	h.file.Write([]byte(b.String())) //nolint:errcheck // best-effort cosmetic write; a failure here surfaces on the next real transcript Write instead.
	h.lastDrawn = 0
}

// redraw repaints the header in place with frame idx: cursor up over the
// header's own rows plus everything printed below it since, clear and
// rewrite each header row (never a per-frame-shape assumption — \x1b[2K
// erases whatever a longer previous frame left behind), then cursor back
// down by the same body distance so the very next transcript write lands
// exactly where it would have without any of this happening. Never a scroll
// region (SETUP.md constraint 5) and never more than a handful of relative
// cursor moves — no absolute positioning.
//
// P1-2: a CUU/CUD pair that overshoots the viewport does NOT "just get
// clamped harmlessly" the way an earlier version of this comment claimed —
// CUU clamps at the top margin, so once rows+bodyLines reaches the live
// viewport height, the \x1b[2K + header rows land on top of the live
// transcript instead of the header, and the trailing CUD clamps at the
// bottom, misplacing the next write too. So this checks the live viewport
// height (terminalSize) before ever emitting a byte, and once it finds
// rows+bodyLines has reached it, sets scrolledOff and every subsequent call
// returns immediately: SETUP.md's "it scrolls away" outcome for real, not
// "it repaints at the clamp". scrolledOff is sticky for the header's whole
// remaining lifetime — bodyLines only ever grows from here (the transcript
// never shrinks), so there is no path back under the threshold once it is
// hit. Caller must hold mu; writes straight to h.file, never through Write,
// so the header's own rows are never counted as body.
//
// P2-1: a second, independently-checked latch guards the same entry point —
// accountingStale, set by onResize (setup_header_signal_unix.go) whenever a
// SIGWINCH lands. A resize makes a real terminal reflow whatever wrapped
// lines are already on screen, so bodyLines (accumulated under the *old*
// width) no longer describes how far below the header the cursor actually
// sits; painting anything using it — even just re-drawing the current frame
// in place — could land on top of live transcript instead of the header.
// Rather than try to recompute bodyLines after the fact (nothing records
// enough history to redo that math once the reflow has already happened),
// this simply refuses to paint at all while accountingStale is true, and
// clears it once bodyLines has genuinely gone back to 0 — in practice, past
// the header's own first paint, that never happens again for the rest of a
// real run, so this behaves the same as scrolledOff: once any resize lands,
// the header stops animating rather than risk repainting with math it can
// no longer trust.
//
// Correction to the "bodyLines only ever grows from here" claim above:
// bodyLines is not actually strictly monotonic, because
// reconcileSelectBodyLines (setupPromptSelect's P1-1 fix) *sets* rather
// than adds, and so can drop from the mid-select peak the arrow-key
// redraws inflated it to. scrolledOff's stickiness is what makes that
// safe regardless: once set, this function never re-evaluates the
// viewport check again for the rest of the run, so a bodyLines dip can
// never un-stick it and risk a repaint at now-stale, possibly-still-too-large
// geometry. (Separately, the ticker is also suspended for a select's whole
// duration, so redraw never actually observes the dip live either way --
// belt and braces, not the only thing keeping this safe.) See
// TestRedrawStaysScrolledOffOnceSet (setup_header_render_test.go) for the
// stickiness guarantee pinned directly.
func (h *setupHeader) redraw(idx int) {
	if h.scrolledOff {
		return
	}
	if h.accountingStale {
		if h.bodyLines != 0 {
			return
		}
		h.accountingStale = false
	}
	if _, viewportRows, _, rowsOK := terminalSize(); rowsOK && h.rows+h.bodyLines >= viewportRows {
		h.scrolledOff = true
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[%dA", h.rows+h.bodyLines)
	for _, row := range h.frames[idx].rows {
		b.WriteString("\x1b[2K")
		b.WriteString(dim(row))
		b.WriteByte('\n')
	}
	if h.bodyLines > 0 {
		fmt.Fprintf(&b, "\x1b[%dB", h.bodyLines)
	}
	h.file.Write([]byte(b.String())) //nolint:errcheck // best-effort cosmetic write; see paintInitial.
	h.lastDrawn = idx
}

// nextPingPongIndex advances idx one step along dir within [0, n-1],
// bouncing direction at either end — the "ping-pongs it for the full swing"
// idle motion assets/AGENTS.md describes for this frame set. A pure
// function, independent of any terminal state, so it is unit-tested
// directly (setup_header_test.go) without a pty.
func nextPingPongIndex(idx, dir, n int) (int, int) {
	if n <= 1 {
		return 0, dir
	}
	idx += dir
	switch {
	case idx >= n-1:
		idx, dir = n-1, -1
	case idx <= 0:
		idx, dir = 0, 1
	}
	return idx, dir
}

// tick is the ticker goroutine's one unit of work: advance the ping-pong
// index and redraw, unless running is false (a Suspend/Stop raced the timer
// firing — running is only ever flipped under mu, so this check is
// authoritative), the frame hasn't actually changed (constraint 6: bounded
// repaint cost — no redraw when nothing would change on screen), or lastNL
// is false, meaning some write is mid-line right now. That last case should
// never actually happen while running is true (every prompt suspends the
// ticker first), but costs nothing to check defensively: skipping one frame
// is invisible, corrupting a partial line is not.
func (h *setupHeader) tick() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.running || !h.animating || !h.lastNL {
		return
	}
	idx, dir := nextPingPongIndex(h.idx, h.dir, len(h.frames))
	h.idx, h.dir = idx, dir
	if idx == h.lastDrawn {
		return
	}
	h.redraw(idx)
}

// startTicker starts the redraw goroutine if it is not already running, and
// refuses outright once stopped is set (P2-2): Stop() hands stdout back to
// the bare file and clears activeSetupHeader, so a ticker started after that
// point would be writing header escapes into a terminal (or a stdout var)
// this header no longer owns. Not reachable through today's call ordering —
// nothing calls Resume() after Stop() — but nothing enforced that either,
// so this guard exists rather than relying on callers to get the ordering
// right forever. Otherwise idempotent: Resume() calls it unconditionally,
// and a paired Suspend() may not have run first (defensive, though
// setupPromptSelect/setupPromptLineCtx always pair them).
func (h *setupHeader) startTicker() {
	h.mu.Lock()
	if h.running || h.stopped {
		h.mu.Unlock()
		return
	}
	h.running = true
	stop := make(chan struct{})
	done := make(chan struct{})
	h.stopCh, h.doneCh = stop, done
	h.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(setupHeaderTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				h.tick()
			}
		}
	}()
}

// stopTicker stops the redraw goroutine, if running, and blocks until it has
// actually exited before returning — so the caller (Suspend, or Stop via
// stopOnce) can rely on there being zero chance of a tick landing after this
// call returns, which is exactly the guarantee a prompt about to read stdin
// (or read raw keys) needs (SETUP.md constraint 1).
func (h *setupHeader) stopTicker() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	h.running = false
	stop, done := h.stopCh, h.doneCh
	h.mu.Unlock()

	close(stop)
	<-done // TestStopTickerBlocksUntilGoroutineExits (setup_header_render_test.go) pins this wait directly.
}

// Suspend stops the ticker for the duration of one prompt read. Safe to call
// on a nil *setupHeader (the degraded/static/--print-commands cases, where
// there is no ticker to begin with).
func (h *setupHeader) Suspend() {
	if h == nil {
		return
	}
	h.stopTicker()
}

// Resume restarts the ticker after a prompt read has returned. A no-op once
// a resize has permanently degraded this header to static (onResize,
// setup_header_signal_unix.go) — there is nothing left to resume ticking.
func (h *setupHeader) Resume() {
	if h == nil {
		return
	}
	h.mu.Lock()
	animating := h.animating
	h.mu.Unlock()
	if !animating {
		return
	}
	h.startTicker()
}

// noteEchoedLine accounts for the one terminal row a promptLineCtx answer
// occupies that this process never itself writes: unlike the select widget
// (enableRawSelectMode disables ECHO, so every visible byte is this
// process's own Write call, already counted), promptLineCtx never touches
// termios, so the typed answer and its terminating Enter are rendered by
// the terminal's own cooked-mode echo — bytes that land on the real screen
// but never pass through Write. Left uncorrected, bodyLines would
// permanently undercount by one after every typed answer, and the very
// next redraw would move up one row short, painting header content over
// the first line of transcript instead of the header. Only ever called
// after a promptLineCtx call actually returned a completed line (err ==
// nil) — see setupPromptLineCtx — so a cancelled read (ctx done before
// Enter) never claims a row that was never actually completed on screen.
//
// TODO(setup-p2): this assumes ECHO stays enabled through every
// promptLineCtx call in this wizard, which is true today (nothing before
// askSetupNetworkID's or askSetupRooms' typed sub-prompts disables it) but
// is not something setupPromptLineCtx itself verifies — a future typed
// prompt added under a raw/no-echo mode would silently double-count here.
//
// answer is the completed line promptLineCtx returned (trimmed, per its own
// contract): the row count is measured the same way selectBlockRows
// measures the select widget's own block, via advanceRows, rather than
// blindly charging exactly one row regardless of whether the echoed
// "question + typed answer" line actually wraps at the live terminal width
// (P1-1's own wrap-blindness fix for the select widget, applied here too).
// h.col already holds the column the question's own Write left the cursor
// at, so advanceRows is seeded with it instead of column 0 — the terminal's
// cooked-mode echo continues from exactly that column, not a fresh line.
// This is an approximation in one respect: the terminal echoes the
// operator's raw keystrokes, including any leading/trailing whitespace
// promptLineCtx's own strings.TrimSpace already discarded by the time this
// runs — trimmed-away whitespace is not reflected in the row count, a minor
// gap compared to the flat "+1" this replaces.
func (h *setupHeader) noteEchoedLine(answer string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	width, _ := terminalWidth() // 0 on failure; advanceRows treats that as "wrapping unknown, count newlines only".
	_, rows, _ := advanceRows(h.col, width, h.pendingWrap, answer+"\n")
	h.bodyLines += rows
	h.col = 0
	h.pendingWrap = false
	h.lastNL = true
	h.mu.Unlock()
}

// Stop settles the header (redraws once more at the resting frame, idx 0,
// so the completion screen never freezes mid-swing) and releases everything
// it owns: the ticker goroutine, the SIGWINCH watch, and stdout itself,
// handed back to the bare file. Idempotent (stopOnce) and safe on a nil
// *setupHeader, so runSetupInteractive can unconditionally `defer
// header.Stop()` right after startSetupHeader regardless of which branch
// that returned. Never touches termios or the alternate screen, so there is
// nothing here a Ctrl-C needs this to run for (SETUP.md constraint 3) — it
// exists for a clean visual settle and to stop the goroutine, not for
// correctness of the terminal's own state.
//
// P2-3: the settle redraw additionally requires h.lastNL, matching every
// other call site that invokes redraw (tick, onResize) — Stop was the one
// place that didn't. A Ctrl-C landing while a typed question is mid-line
// (cursor sitting after a partial prompt, no trailing newline) leaves
// lastNL false; CUU preserves the column it moves along, so redrawing
// there would land the header's first row at whatever column the cursor
// already happened to be at instead of column 0.
func (h *setupHeader) Stop() {
	if h == nil {
		return
	}
	h.stopOnce.Do(func() {
		h.stopTicker()
		h.stopWatchingResize()
		h.mu.Lock()
		if h.animating && h.lastNL {
			h.redraw(0)
		}
		h.stopped = true
		h.mu.Unlock()
		stdout = h.file
		activeSetupHeader.Store(nil)
	})
}

// setupPromptSelect, snapshotBodyLines, reconcileSelectBodyLines, and
// setupPromptLineCtx live in setup_header_prompts.go -- split out to keep
// this file under the repo's 400-line limit.
