package main

import (
	"fmt"
	"sync"
	"time"
)

// spinnerFrames is the braille-dot animation `moltnet relay deploy` (and any
// future multi-second network wait) cycles through while a spinner is
// running. A var, not a const slice, so a test can swap in a smaller/larger
// set if it ever needs to; spinnerInterval is a var for the same reason —
// tests shorten it so a handful of frames render inside a short sleep
// instead of needing to wait out a realistic interval.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerInterval is the redraw cadence between frames.
var spinnerInterval = 90 * time.Millisecond

// spinnerEnabled reports whether the CLI should show the animated progress
// spinner for a network wait: only over a genuine terminal (stdoutIsRealTTY,
// banner_player.go's own unmockable file-descriptor check — never the
// mockable isOutputTerminal seam other tests override to simulate a TTY
// without one actually attached) with color actually enabled (colorEnabled
// — NO_COLOR/TERM=dumb excluded). This mirrors playBanner's own gating
// exactly, for the same reason: every existing captureStdout-based test in
// this package writes to an os.Pipe, never a real pty, so spinner frames can
// never leak into a piped/golden capture regardless of what any test forces
// isOutputTerminal to — piped output must stay clean.
func spinnerEnabled() bool {
	return stdoutIsRealTTY() && colorEnabled()
}

// spinner is a small progress indicator for a network wait — the sequence
// of Cloudflare API calls `moltnet relay deploy` makes inside a single
// relaydeploy.Deploy call (token verify -> account -> upload -> secret ->
// subdomain) is the first use, via startSpinner/Stop wrapping that call in
// relay_deploy.go. It redraws "<frame> <label>" in place on one line every
// spinnerInterval and clears that line completely on Stop, so whatever the
// caller prints next starts on a clean line — it never fights
// sectionPrinter's blank-line bookkeeping (style.go), since by the time
// Stop returns nothing of the spinner remains on screen.
type spinner struct {
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// startSpinner starts and returns a running spinner showing label, or nil
// when spinnerEnabled() is false. Every method on a nil *spinner is a safe
// no-op (see Stop), so callers never need their own enabled check at the
// call site — `sp := startSpinner("..."); ...; sp.Stop()` is always safe to
// write unconditionally.
func startSpinner(label string) *spinner {
	if !spinnerEnabled() {
		return nil
	}
	s := &spinner{stop: make(chan struct{}), done: make(chan struct{})}
	go s.run(label)
	return s
}

// run is the spinner's redraw loop: it renders frame 0 immediately (no
// initial delay before anything appears), then redraws in place every
// spinnerInterval until told to stop, at which point it clears its line and
// returns. Runs on its own goroutine, started by startSpinner.
func (s *spinner) run(label string) {
	defer close(s.done)

	frame := 0
	render := func() {
		fmt.Fprintf(stdout, "\r\x1b[2K  %s %s", spinnerFrames[frame%len(spinnerFrames)], label)
		frame++
	}
	render()

	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			fmt.Fprint(stdout, "\r\x1b[2K")
			return
		case <-ticker.C:
			render()
		}
	}
}

// Stop halts the spinner and blocks until its goroutine has actually
// cleared its line, so the caller's very next write is guaranteed to land
// on a clean line rather than racing the final clear. Safe to call on a nil
// *spinner (startSpinner's disabled-path return value) and safe to call more
// than once, including concurrently: sync.Once guards the channel close
// itself, since the previous select-on-s.stop/default check was not safe
// against two goroutines calling Stop() at the same instant (both could
// observe the channel as still open and both attempt to close it).
func (s *spinner) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
}
