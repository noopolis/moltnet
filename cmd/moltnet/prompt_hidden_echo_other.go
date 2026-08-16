//go:build !darwin && !linux

package main

import (
	"io"
	"os"
)

// disableTerminalEcho is prompt_hidden_echo_unix.go's counterpart for every
// GOOS other than darwin and linux — the only two with a termios ioctl
// mapping wired up (prompt_hidden_termios_darwin.go / _linux.go). There is
// no known way to suppress terminal echo here at all, so a real
// interactive terminal always fails closed (ok=false): promptHidden
// refuses to read rather than risk echoing a secret to the screen (see
// errTerminalEchoUnavailable, prompt_hidden.go), and its caller falls back
// to the non-interactive token-guidance path. The test seam (a swapped
// promptReader) and a non-interactive stdin still no-op successfully —
// nothing needs suppressing in either case — matching
// prompt_hidden_echo_unix.go's own no-op branches, so this package's
// existing tests keep passing (and `GOOS=freebsd go build ./cmd/moltnet`,
// for example, keeps compiling) on every GOOS.
func disableTerminalEcho() (restore func(), ok bool) {
	noop := func() {}
	if promptReader != io.Reader(os.Stdin) || !isInteractive() {
		return noop, true
	}
	return noop, false
}

// flushPendingTerminalInput is flushPendingTerminalInput's
// (prompt_hidden_echo_unix.go) counterpart here: there is no termios ioctl
// mapping on this GOOS either, so there is nothing to flush. Unlike
// disableTerminalEcho, a missing flush is never a security concern (see
// prompt_hidden_echo_unix.go's doc comment on why a flush failure is
// always safe to ignore), so this is simply an unconditional no-op rather
// than a fail-closed gate.
func flushPendingTerminalInput() {}
