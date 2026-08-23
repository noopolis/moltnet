//go:build darwin || linux

package main

import (
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// enableRawSelectMode clears the controlling terminal's ECHO and ICANON
// local-mode bits so promptSelectInteractive can read one key at a time
// instead of waiting for a line terminated by Enter, and returns a restore
// func that puts the terminal's exact prior state back.
//
// It reuses ioctlGetTermios/ioctlSetTermios and the ioctlReadTermios/
// ioctlWriteTermios request constants disableTerminalEcho already defines
// (prompt_hidden_echo_unix.go, prompt_hidden_termios_darwin.go/_linux.go)
// rather than duplicating the platform-specific ioctl numbers a third time.
// Unlike disableTerminalEcho, this installs no SIGINT/SIGTERM handling of
// its own: there is no secret at risk here that demands the fail-closed,
// signal-safe restoration promptHidden's contract requires
// (errTerminalEchoUnavailable's doc comment) — arrow-key selection is not
// reading a credential, and ISIG is deliberately left untouched below, so a
// real Ctrl-C during selection still raises SIGINT the normal way and is
// handled by runCLI's signal.NotifyContext cancelling ctx (see
// promptSelectInteractive's ctx.Done() case), at which point this
// function's returned restore, deferred by promptSelect, still runs on the
// way out since the process does not os.Exit on that path (only main.go
// does, once run() has already returned). That reasoning holds only for
// SIGINT/SIGTERM, which runCLI's signal.NotifyContext already traps
// (signals.Default(), signals.go): SIGHUP and SIGTSTP are not in that set
// (P2-3), so without a handler of its own here a terminal-closing SIGHUP or
// a Ctrl-Z SIGTSTP mid-select would leave the real terminal in raw mode
// indefinitely — worse than promptHidden's own ECHO-only exposure, since
// this also clears ICANON. This function does install a handler for those
// two, below, reusing disableTerminalEcho's restore-then-exit shape but
// without its SIGTSTP re-raise dance (prompt_hidden_echo_unix.go): there is
// no attempt to actually re-suspend the process on SIGTSTP here, just a
// clean restore-and-exit like every other signal this handles, since a
// select prompt has no in-flight secret whose paste needs to survive a
// stop/resume cycle the way promptHidden's does.
//
// It is a no-op success (a no-op restore func, ok=true) whenever
// promptReader is not the real os.Stdin or stdin is not interactive — the
// same test-seam gate disableTerminalEcho uses — and fails open in the
// caller's favor (ok=false, so promptSelect falls back to
// promptSelectPlain) rather than fail-closed like disableTerminalEcho: a
// select prompt that cannot get raw mode is not a security problem the way
// an un-suppressed secret echo would be, it is just degraded UX, and the
// plain numbered-list fallback is a completely valid path in its own right
// (Enter-through-everything already goes through it in every
// non-interactive test in this package).
func enableRawSelectMode() (restore func(), ok bool) {
	noop := func() {}
	if promptReader != io.Reader(os.Stdin) || !isInteractive() {
		return noop, true
	}

	fd := int(os.Stdin.Fd())
	original, err := ioctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return noop, false
	}

	raw := *original
	raw.Lflag &^= unix.ECHO | unix.ICANON
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := ioctlSetTermios(fd, ioctlWriteTermios, &raw); err != nil {
		return noop, false
	}

	var once sync.Once
	restoreTermios := func() {
		once.Do(func() {
			// The immediate variant, not ioctlWriteTermios (the flush
			// variant) — see ioctlWriteTermiosImmediate's own doc comment
			// (setup_prompt_select_termios_darwin.go/_linux.go) for the
			// full measurement. In short: the flush variant's hazard here
			// is not a pending read on the fd (that returns immediately)
			// but that it also flushes queued output, blocking until a far
			// end that stopped reading — a demand-driven test pty reader,
			// or real XOFF/Ctrl-S flow control — actually reads it. The
			// immediate variant is safe to use instead because the read
			// loop above already drains the input queue before this
			// restore ever runs, so there is nothing queued left to leak
			// by skipping the flush variant's input-discard semantics.
			_ = ioctlSetTermios(fd, ioctlWriteTermiosImmediate, original)
		})
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTSTP)
	go func() {
		sig, ok := <-sigCh
		if !ok {
			return
		}
		restoreTermios()
		// Unlike disableTerminalEcho's own signal loop (prompt_hidden_echo_unix.go),
		// which special-cases SIGTSTP to actually suspend and resume, this
		// path always exits -- Ctrl-Z at a select prompt terminates the
		// wizard outright rather than suspending it. Nothing has mutated at
		// this point, so there is no partial state; exitCodeForSignal still
		// gives SIGTSTP its own distinguishable 128+signal code rather than
		// the generic default-case 1 an ordinary failure would also produce.
		os.Exit(exitCodeForSignal(sig))
	}()

	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			signal.Stop(sigCh)
			close(sigCh)
			restoreTermios()
		})
	}, true
}
