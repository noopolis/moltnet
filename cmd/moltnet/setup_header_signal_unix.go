//go:build darwin || linux

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// watchResize installs a SIGWINCH handler for h's lifetime (SETUP.md
// constraint 4: "SIGWINCH invalidates row accounting. Install a handler and
// do a full-region repaint."). Unlike enableRawSelectMode's SIGHUP/SIGTSTP
// handling, this never calls os.Exit — a resize is not a reason to
// terminate, just to repaint — so it is a plain background goroutine reading
// the notify channel until stopWatchingResize closes it.
//
// P2-2: resizeDone is closed (via the deferred close, mirroring
// startTicker's doneCh) only once this goroutine has actually left its
// range loop — i.e. any onResize call already in flight has returned —
// giving stopWatchingResize something to block on before Stop() hands
// stdout back to the bare file. Without it, onResize (which reads the
// package stdout var via terminalWidth/terminalSize) could still be running
// concurrently with Stop()'s own write to that same var: closing ch alone
// only stops *future* signals, it does not wait out one already being
// handled.
func (h *setupHeader) watchResize() {
	ch := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(ch, syscall.SIGWINCH)
	h.mu.Lock()
	h.resizeCh = ch
	h.resizeDone = done
	h.mu.Unlock()

	go func() {
		defer close(done)
		for range ch {
			h.onResize()
		}
	}()
}

// stopWatchingResize undoes watchResize: signal.Stop first (so no further
// SIGWINCH is ever delivered to ch), then closes ch to let its goroutine
// exit — mirroring "restore any handler you install" (SETUP.md constraint
// 4) — and finally blocks on resizeDone (P2-2) so the caller (Stop, via
// stopOnce) can rely on there being zero chance of onResize still running
// once this returns, the same guarantee stopTicker's own <-done gives for
// the ticker goroutine. Safe to call even if watchResize's goroutine already
// exited for some other reason; closing an already-drained, still-open
// channel just ends its range loop, and done is already closed by then.
func (h *setupHeader) stopWatchingResize() {
	h.mu.Lock()
	ch := h.resizeCh
	done := h.resizeDone
	h.resizeCh = nil
	h.resizeDone = nil
	h.mu.Unlock()
	if ch == nil {
		return
	}
	// signal.Stop before close, in that order, matters: closing ch while it
	// is still registered with signal.Notify would let a later SIGWINCH
	// attempt to send to a closed channel -- Go's signal delivery has no
	// "already closed" protection, so that send panics the whole process.
	// TestStopWatchingResizeUnregistersBeforeClosingChannel pins this
	// ordering directly by raising a real SIGWINCH right after this
	// function returns.
	signal.Stop(ch)
	close(ch)
	<-done
}

// onResize is watchResize's per-signal handler, unless the resize has
// narrowed the terminal below what the loop frame set needs
// (setupHeaderEligible, re-checked fresh here — a live width change is
// exactly the case that gate exists for), in which case this header
// permanently stops animating for the rest of the run rather than risk
// redrawing into a terminal too narrow to hold a frame without wrapping
// (SETUP.md constraint 2's degrade-to-static behavior, applied
// retroactively to an already-running header instead of only at
// startSetupHeader's own one-time check).
//
// P2-1 / round 3: when the header stays eligible, this does NOT attempt the
// "full-region repaint" it used to — a real terminal reflows whatever
// wrapped lines are already on screen the instant it resizes, so bodyLines
// (redraw's own cursor-up input, accumulated under the *old* width) no
// longer describes how far below the header the cursor now actually sits.
// Repainting with that stale number could land the header's own rows on top
// of live transcript instead of over itself — worse than not repainting at
// all. So this only sets accountingStale; redraw (setup_header_render.go)
// is the single place that decides whether painting is safe, and it now
// refuses until bodyLines genuinely returns to 0 (which, past the header's
// own first paint, does not happen again during a real run — so in practice
// this permanently freezes the header's last-drawn frame in place once any
// resize lands, the same conservative outcome the ineligible branch above
// reaches by a different path).
//
// TODO(setup-p2): once a resize narrows the terminal below
// setupHeaderFrameWidth and this header degrades to static, a later resize
// back to a wide-enough terminal does not re-enable animation — animating
// is a one-way decision for the life of one `moltnet setup` run, same as
// startSetupHeader's own initial gate.
func (h *setupHeader) onResize() {
	h.mu.Lock()
	animating := h.animating
	h.mu.Unlock()
	if !animating {
		return
	}

	if !setupHeaderEligible() {
		h.stopTicker()
		h.mu.Lock()
		h.animating = false
		h.mu.Unlock()
		return
	}

	h.mu.Lock()
	h.accountingStale = true
	if h.running && h.lastNL {
		h.redraw(h.idx) // no-op in practice: redraw's own accountingStale guard refuses to paint using now-stale accounting.
	}
	h.mu.Unlock()
}
