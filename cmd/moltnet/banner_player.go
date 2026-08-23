package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/mattn/go-isatty"
	"golang.org/x/sys/unix"
)

// bannerFrameInterval is playBanner's target per-frame delay; it may be
// shortened (never lengthened) so a larger future frame set can never push
// the total animation past bannerAnimationHardCap.
const bannerFrameInterval = 60 * time.Millisecond

// bannerAnimationHardCap bounds the settle animation's total run time,
// not just an aspiration baked into the per-frame interval: playBanner
// checks elapsed time against it (nowFunc) before every frame transition
// and, once exceeded, jumps straight to the final frame and returns
// instead of stepping through whatever frames remain — this bounds the
// animation to roughly the cap (one frame interval + final write may
// exceed it slightly). bannerFrameStepInterval also shortens (never
// lengthens) the per-frame delay by this same constant so a larger future
// frame set doesn't need the deadline check to fire in the first place;
// the interval covers the common, unblocked case, the deadline check is
// what actually enforces the cap when something stalls the writes.
const bannerAnimationHardCap = 1200 * time.Millisecond

// nowFunc is playBanner's only point of contact with wall-clock time — a
// var, not a direct time.Now() call, so a test can inject a fake clock
// (TestPlayBannerDeadlineJumpsToFinalFrame) to exercise the
// bannerAnimationHardCap deadline deterministically instead of needing an
// actual slow writer to burn 1.2 real seconds.
var nowFunc = time.Now

// fdFile is the minimal capability terminalWidth, stdoutIsRealTTY, and
// isOutputTerminal (style.go) actually need from the stdout var: the raw OS
// file descriptor behind it. It exists so a wrapper that is not literally an
// *os.File — setup_header.go's live header, which forwards stdout's real
// file through its own io.Writer for the whole time it owns the terminal,
// including across a prompt's Suspend/Resume cycle — can still satisfy
// these three checks exactly as if stdout were the bare file, rather than
// having every one of them read "not a terminal" the moment setup wraps it.
// *os.File already implements Fd() uintptr, so this widens what these three
// checks accept without changing their answer for any caller that passes a
// literal *os.File today (a real os.Stdout, or a test's os.Pipe()/pty
// file).
type fdFile interface {
	Fd() uintptr
}

// terminalSize reports stdout's live column and row counts together, backed
// by the one TIOCGWINSZ ioctl terminalWidth itself used to issue alone: a
// winsize struct always carries both, so a caller that needs both values
// (setup_header.go's viewport-height gate, P1-2) pays for a single syscall
// rather than risking two independent ones observing different sizes either
// side of an in-flight resize.
//
// colsOK and rowsOK are reported independently (P3-1): a real terminal can
// legitimately report Row:0 while Col is perfectly good (observed on a real
// pty: IoctlSetWinsize{Row:0,Col:80} round-trips exactly as set, no kernel
// validation rejects it), and folding both into one combined ok — as an
// earlier version of this function did — let a bad Row silently invalidate
// a good Col, taking terminalWidth's own COLUMNS fallback down with it even
// though COLUMNS is never actually exported by shells and so never actually
// rescues anything. ok is false for a given value under exactly the
// conditions terminalWidth's own doc comment already described for the
// ioctl path. There is no environment-variable fallback for rows here — no
// LINES analogue to COLUMNS is read anywhere else in this codebase — so an
// ioctl-less environment (a plain pipe) always reports rowsOK false even
// when cols alone might still be known via terminalWidth's own COLUMNS
// fallback below.
func terminalSize() (cols, rows int, colsOK, rowsOK bool) {
	f, isFdFile := stdout.(fdFile)
	if !isFdFile {
		return 0, 0, false, false
	}
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, false, false
	}
	return int(ws.Col), int(ws.Row), ws.Col > 0, ws.Row > 0
}

// terminalWidth reports stdout's live column count: terminalSize's ioctl
// when it succeeds, falling back to the COLUMNS environment variable when
// the ioctl isn't available, errors, or reports Col as zero. Returns (0,
// false) when neither source yields a usable positive width; playBanner
// treats that as "assume narrow" (P1) rather than risk animating into a
// terminal it never actually measured.
func terminalWidth() (int, bool) {
	if cols, _, colsOK, _ := terminalSize(); colsOK {
		return cols, true
	}
	if raw := os.Getenv("COLUMNS"); raw != "" {
		if cols, err := strconv.Atoi(raw); err == nil && cols > 0 {
			return cols, true
		}
	}
	return 0, false
}

// stdoutIsRealTTY reports whether stdout is a literal terminal-backed file
// descriptor, checked directly rather than through the mockable
// isOutputTerminal var (style.go) that other tests override to simulate a
// TTY without one actually attached (see style_test.go). The settle
// animation writes raw cursor-up escape sequences that only make sense
// against a real terminal's line-based redraw model, so this check is
// deliberately independent of that seam: same stdout var, same isatty
// check isOutputTerminal's own default implementation does, just as a
// plain func no test can override. A pipe posing as a TTY (every non-pty
// test's captureStdout, including ones that force isOutputTerminal true)
// always takes playBanner's static fallback; only a genuine pseudo-
// terminal (requireTestPTY, openpty_test.go) makes it return true.
func stdoutIsRealTTY() bool {
	f, ok := stdout.(fdFile)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// bannerFrameStepInterval returns the per-transition delay playBanner
// should sleep between frames: bannerFrameInterval, unless frameCount
// transitions at that pace would exceed bannerAnimationHardCap, in which
// case it is shortened so the whole animation still fits the cap.
func bannerFrameStepInterval(frameCount int) time.Duration {
	transitions := frameCount - 1
	if transitions <= 0 {
		return 0
	}
	interval := bannerFrameInterval
	if total := interval * time.Duration(transitions); total > bannerAnimationHardCap {
		interval = bannerAnimationHardCap / time.Duration(transitions)
	}
	return interval
}

// playBanner writes the banner's content: the settle animation when
// stdout is a genuine terminal (stdoutIsRealTTY) capable of ANSI styling
// (colorEnabled — NO_COLOR/TERM=dumb excluded), the embedded frame set
// parsed cleanly (bannerFramesErr == nil), and the terminal is wide enough
// to show a frame without wrapping (terminalWidth() reports at least
// bannerFrameContentWidth+1 columns — the "+1" is headroom for deferred
// autowrap, where a terminal only wraps a full-width line once the *next*
// character arrives rather than at the last column itself, so writing
// exactly bannerFrameContentWidth columns on an exactly-that-wide terminal
// can still smear). Below that width, or whenever the width can't be
// determined at all, playBanner takes the static path rather than risk it.
//
// When it does animate: frame 0 is written immediately, then each
// subsequent frame is drawn by moving the cursor back up to the top of the
// block ("\x1b[<rows>A") and reprinting, dimmed exactly like the static
// banner, ~60ms apart (bannerFrameStepInterval) — unless
// bannerAnimationHardCap has already elapsed by the time a given frame is
// due, in which case playBanner jumps straight to the final frame and
// returns instead of stepping through whatever frames remain. It never
// saves the cursor, switches to an alternate screen, or hides the cursor —
// only cursor-up plus ordinary line writes — so a Ctrl-C mid-animation
// needs no special cleanup.
//
// Whenever any of the above don't hold, it falls back to printing
// dim(bannerText) — byte-identical to printBanner's own static output, so
// every existing non-animated caller and test is unaffected.
//
// ctx is consulted between frames (via a select against time.After(interval)
// rather than an uninterruptible time.Sleep, so a cancellation lands within
// one frame interval instead of waiting out whatever sleep was already in
// progress): when ctx is done, playBanner returns immediately, leaving
// whatever partial frame is already on screen — unlike the hard-cap branch
// above, it does NOT jump to the final settled frame, since a cancelled ctx
// means the caller (runInit) is about to exit on ctx.Err() anyway, not
// continue on to whatever the completed animation would normally lead into.
// The static fallback path above never sleeps, so it has no cancellation
// point of its own — ctx is not consulted there.
func playBanner(ctx context.Context) {
	width, widthOK := terminalWidth()
	if !stdoutIsRealTTY() || !colorEnabled() || bannerFramesErr != nil || !widthOK || width < bannerFrameContentWidth+1 {
		fmt.Fprint(stdout, dim(bannerText))
		return
	}

	rows := len(bannerFrames[0].rows)
	cursorUp := fmt.Sprintf("\x1b[%dA", rows)
	interval := bannerFrameStepInterval(len(bannerFrames))
	last := len(bannerFrames) - 1

	fmt.Fprint(stdout, dim(bannerFrames[0].block()))

	start := nowFunc()
	for i := 1; i <= last; i++ {
		if nowFunc().Sub(start) >= bannerAnimationHardCap {
			fmt.Fprint(stdout, cursorUp)
			fmt.Fprint(stdout, dim(bannerFrames[last].block()))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
		fmt.Fprint(stdout, cursorUp)
		fmt.Fprint(stdout, dim(bannerFrames[i].block()))
	}
}
