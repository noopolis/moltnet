//go:build darwin

package main

import "golang.org/x/sys/unix"

// ioctlWriteTermiosImmediate is enableRawSelectMode's restore-path ioctl
// request number: TIOCSETA, the immediate (POSIX TCSANOW-equivalent)
// variant, deliberately not TIOCSETAF (the flush variant ioctlWriteTermios,
// prompt_hidden_termios_darwin.go, already uses for disableTerminalEcho).
//
// This is not a workaround for a pending-read deadlock: an isolated probe
// (a genuinely pending non-canonical VMIN=1/VTIME=0 blocking read() on the
// fd, no queued output) shows TIOCSETAF returns immediately — a pending
// read in flight on the same fd is not what makes the flush variant hang.
// The real hazard TIOCSETAF has here is that it also flushes *output*: it
// blocks until output already queued for the fd has actually been read
// from the far end (POSIX TCSAFLUSH), and in the exact ctx-cancellation
// scenario this call site restores from (SIGINT mid keypress, with
// promptSelectInteractive's own read goroutine deliberately left to exit on
// its own), a test harness driving a pty master that has stopped reading
// (readMasterUntil stops once it finds its substring — see
// prompt_hidden_pty_test.go:201-217 and drainBackgroundMaster, which exists
// for exactly this reason) will make that flush block indefinitely. A real
// terminal emulator keeps reading its pty master continuously and would not
// hang here, but any far end that stops reading — an undrained emulator, or
// XOFF/Ctrl-S flow control — would still hang on TIOCSETAF regardless of
// platform, which is reason enough to prefer the immediate variant here
// even where it happens not to bite today.
//
// The immediate variant is also not losing safety it needs: it does not
// discard the input queue on restore the way TIOCSETAF would, and on this
// platform enableRawSelectMode's own read loop (a bufio reader) drains that
// queue before restore ever runs — measured queuedBeforeRestore=0 for both
// a small keypress burst and a 6 KB paste — so nothing survives in the
// queue to leak into whatever reads the terminal next.
const ioctlWriteTermiosImmediate = unix.TIOCSETA
