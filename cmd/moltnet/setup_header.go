package main

import (
	_ "embed"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// setupHeaderFramesText is the embedded idle-pendulum loop set
// (assets/AGENTS.md: "kept in for a later 'waiting' idle animation") this
// unit wires up as the wizard's live header: 7 frames, a 0deg..+18deg
// quarter-swing that setupHeader ping-pongs for the full sway. It goes
// through the exact same parser/validator banner_frames.go's settle
// animation uses (parseBannerFrames) rather than a second implementation —
// same dimension checks, same shared-content crop, same consecutive-
// duplicate drop.
//
//go:embed assets/banner-frames-loop.txt
var setupHeaderFramesText string

// setupHeaderFrames and setupHeaderFramesErr mirror bannerFrames/
// bannerFramesErr's contract exactly: a non-nil error means the embedded
// asset failed its own validation, and startSetupHeader treats that as
// "cannot animate", falling back to the static banner — never panicking.
var setupHeaderFrames, setupHeaderFramesErr = parseBannerFrames(setupHeaderFramesText)

// setupHeaderFrameWidth is the widest row across every parsed loop frame,
// setupHeaderEligible's width gate input — the same role
// bannerFrameContentWidth plays for the settle animation.
var setupHeaderFrameWidth = maxFrameRowWidth(setupHeaderFrames)

// setupHeaderTickInterval is the header's idle-sway redraw cadence — a var,
// not a const, so tests can shrink it rather than waiting out a realistic
// interval (spinner.go's spinnerInterval is the same pattern).
var setupHeaderTickInterval = 220 * time.Millisecond

// activeSetupHeader is the one live header instance for the current `moltnet
// setup` invocation, or nil whenever no header is running (degraded/static
// fallback, --print-commands, or the non-interactive refusal, which never
// reaches this package at all). It exists so setupPromptSelect/
// setupPromptLineCtx — the two wrappers setup_questions.go calls instead of
// promptSelect/promptLineCtx directly — can suspend/resume the ticker
// without every ask* function having to carry a header parameter of its
// own, the same package-var-seam style this file's neighbors already use
// for stdout, promptReader, and runChild.
//
// It is only ever *written* from the single goroutine driving the question
// flow (startSetupHeader, Stop) and never at all from the ticker goroutine.
// It is an atomic.Pointer rather than a bare var purely so a test on a
// different goroutine (e.g. one asserting the header is live while the
// wizard's own goroutine is blocked at a prompt, or gone once it has
// returned) can read it without racing that single writer — a plain
// pointer read/write pair across goroutines with no synchronization is a
// data race regardless of how obviously ordered the events look from a
// pty's output, since file I/O establishes no happens-before edge the race
// detector can see.
var activeSetupHeader atomic.Pointer[setupHeader]

// setupHeader is the wizard's live header region (SETUP.md's "Driving the
// terminal" / "Rendering"): N rows at the top showing the idle-sway
// wordmark, repainted on a ticker while the question flow and child
// execution proceed, with the transcript growing beneath it via completely
// ordinary writes.
//
// It also *is* the package's stdout var for its entire lifetime (Start to
// Stop) — see Write/Fd below — rather than something that hands stdout back
// and forth across Suspend/Resume: unwrapping stdout during a suspended
// prompt would make isOutputTerminal/stdoutIsRealTTY (both keyed off a type
// assertion against whatever stdout currently is) report the real terminal
// as gone, silently downgrading the select widget to its plain fallback.
// Keeping the wrapper in place throughout, and instead just pausing the
// ticker goroutine during Suspend, is what makes fdFile (banner_player.go)
// necessary and sufficient: everything downstream keeps behaving exactly as
// it would against the bare file, including the select widget's own
// in-place redraw.
//
// bodyLines is the number of terminal rows printed *below* the header since
// it was last (re)painted at the current cursor position — every redraw
// moves up by rows+bodyLines, repaints, and moves back down by bodyLines,
// never touching a scroll region (SETUP.md: "do not use DECSTBM").
//
// It is NOT simply a count of '\n' bytes (that was P1-1/P2-1's bug): a
// wrapped line consumes more terminal rows than it has newlines (Write
// derives that from the live width via advanceRows), and the select
// widget's own in-place arrow-key redraw (renderSelectOptions,
// setup_prompt_select.go) repaints its block net-zero rows even though every
// byte of it still flows through Write — counting those bytes' newlines
// would inflate bodyLines by len(options) on every keypress. That region is
// not append-only, so Write cannot be the sole source of truth for it:
// setupPromptSelect instead snapshots bodyLines before the read and
// reconciles it to the select region's true final size once the read
// returns, overriding whatever Write accumulated in between.
//
// The one exception Write also cannot see at all is promptLineCtx's
// cooked-terminal echo of a typed answer, rendered by the kernel without
// this process writing a single byte for it — see noteEchoedLine.
type setupHeader struct {
	file *os.File

	mu        sync.Mutex
	bodyLines int
	// col is advanceRows' running state across separate Write calls
	// (P2-1) -- the visible column reached by the last byte Write
	// processed. advanceRows' own doc comment (below) has the consolidated
	// note on this cell-width model's known, currently-unreachable limits
	// (mid-CSI state across Write calls, '\r'/'\t' handling, wide-rune cell
	// width).
	col int
	// pendingWrap mirrors a real terminal's own deferred (pending) autowrap
	// state (P2-3): true whenever the last visible character Write processed
	// exactly filled column width-1, so the cursor is parked in the last
	// column rather than already having moved to a new row. It survives
	// across Write calls exactly like col does -- the wrap only actually
	// resolves once the next visible character arrives, which may not
	// happen until a later call (or never, if the line simply ends there).
	pendingWrap bool
	lastNL      bool

	animating   bool
	frames      []bannerFrame
	rows        int
	idx         int
	dir         int
	lastDrawn   int
	running     bool
	stopped     bool // set once inside Stop's stopOnce; startTicker refuses to run again after this (P2-2).
	scrolledOff bool // set once redraw finds rows+bodyLines >= the live viewport height; sticky for the rest of the run (P1-2).
	// accountingStale is set by onResize (setup_header_signal_unix.go) once a
	// SIGWINCH lands: the terminal's own native reflow of already-printed
	// wrapped lines means bodyLines -- accumulated under the *old* width --
	// no longer describes how far below the header the cursor actually sits,
	// so redraw refuses to paint at all (not just incorrectly) while this is
	// true, the same "latch checked at the top of redraw" shape scrolledOff
	// already uses. Unlike scrolledOff it is not permanently sticky by
	// definition -- redraw itself clears it once bodyLines genuinely returns
	// to 0 -- but bodyLines never actually does that during a real run past
	// the header's own first paint, so in practice this behaves the same as
	// scrolledOff: once any resize lands, the header stops animating for the
	// rest of the run rather than risk repainting with math it can no longer
	// trust (P2-1).
	accountingStale bool
	stopCh          chan struct{}
	doneCh          chan struct{}
	resizeCh        chan os.Signal // set by watchResize (unix); nil elsewhere
	resizeDone      chan struct{}  // set by watchResize (unix); closed once its goroutine has fully exited onResize, so stopWatchingResize can block on it exactly like stopTicker blocks on doneCh (P2-2). nil elsewhere.

	stopOnce sync.Once
}

// setupHeaderEligible reuses exactly the gates playBanner already applies
// (SETUP.md constraint 2: "reuse those checks; do not invent parallel
// ones") against this header's own loop frame set and width, rather than
// bannerFrames'/bannerFrameContentWidth's settle-animation values.
func setupHeaderEligible() bool {
	width, ok := terminalWidth()
	return stdoutIsRealTTY() && colorEnabled() && setupHeaderFramesErr == nil &&
		ok && width >= setupHeaderFrameWidth+1
}

// startSetupHeader decides, once, how `moltnet setup` opens: nothing at all
// for --print-commands or a genuinely non-terminal stdout (no header, no
// ticker, no goroutine — the exact non-negotiable this unit's brief calls
// out), a static dim(bannerText) frame — byte-identical in spirit to
// printBanner's own — whenever the terminal can't support the animation
// (NO_COLOR, TERM=dumb, too narrow, a malformed frame asset), or the live,
// ticking header otherwise. Only the last case returns a non-nil
// *setupHeader; the first two return nil, and every method on a nil
// *setupHeader is a safe no-op (mirroring spinner.go's nil-receiver
// pattern), so callers never need their own branch at the call site.
func startSetupHeader(printCommandsOnly bool) *setupHeader {
	if printCommandsOnly || !isOutputTerminal() {
		return nil
	}
	if !setupHeaderEligible() {
		fmt.Fprint(stdout, dim(bannerText))
		fmt.Fprintln(stdout)
		return nil
	}
	file, ok := stdout.(*os.File)
	if !ok {
		// Unreachable in practice: setupHeaderEligible already required
		// stdoutIsRealTTY(), whose own assertion (fdFile) only ever
		// succeeds today for a literal *os.File (nothing else is ever
		// assigned to stdout before this runs). Kept as a fail-safe rather
		// than a panic.
		fmt.Fprint(stdout, dim(bannerText))
		fmt.Fprintln(stdout)
		return nil
	}

	h := &setupHeader{
		file:      file,
		frames:    setupHeaderFrames,
		rows:      len(setupHeaderFrames[0].rows),
		dir:       1,
		lastDrawn: -1,
		lastNL:    true,
		animating: true,
	}
	h.paintInitial()
	stdout = h
	fmt.Fprintln(stdout)
	h.watchResize()
	h.startTicker()
	activeSetupHeader.Store(h)
	return h
}

// Write implements io.Writer, making *setupHeader itself a drop-in stdout
// value: every write this process makes while the header is live — every
// question, checkmark line, warning, and the summary screen — passes
// through here, forwarded to the real file unchanged and counted (via
// advanceRows, against the terminal's live width) so the next redraw knows
// exactly how far below the header the cursor now sits. Guarded by mu, the
// same lock redraw holds, so a tick can never interleave its own escape
// sequences with a transcript write mid-flight (SETUP.md constraint 7: "one
// writer owns stdout at a time").
//
// The select widget's own in-place redraw is a deliberate exception to this
// being the sole source of truth for bodyLines — see setupPromptSelect and
// the struct doc comment above.
func (h *setupHeader) Write(p []byte) (int, error) {
	h.mu.Lock()
	n, err := h.file.Write(p)
	if n > 0 {
		width, _ := terminalWidth() // 0 on failure; advanceRows treats that as "wrapping unknown, count newlines only".
		newCol, rows, newPendingWrap := advanceRows(h.col, width, h.pendingWrap, string(p[:n]))
		h.bodyLines += rows
		h.col = newCol
		h.pendingWrap = newPendingWrap
		h.lastNL = p[n-1] == '\n'
	}
	h.mu.Unlock()
	return n, err
}

// advanceRows lives in setup_advance_rows.go -- split out to keep this file
// under the repo's 400-line limit.

// Fd exposes the wrapped file's descriptor so fdFile-based checks
// (terminalWidth, stdoutIsRealTTY, isOutputTerminal) keep working against
// *setupHeader exactly as they would against the bare file.
func (h *setupHeader) Fd() uintptr { return h.file.Fd() }
