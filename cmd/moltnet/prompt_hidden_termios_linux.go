//go:build linux

package main

import "golang.org/x/sys/unix"

// ioctlReadTermios/ioctlWriteTermios are the ioctl request numbers
// prompt_hidden.go's disableTerminalEcho uses to read and write the
// controlling terminal's termios state via golang.org/x/sys/unix. Linux
// names them TCGETS/TCSETSF; the BSD family (see
// prompt_hidden_termios_darwin.go) uses different names for the same
// concept, hence the per-OS file.
//
// ioctlWriteTermios is deliberately the *flush* variant (TCSETSF, not
// plain TCSETS): TCSETSF applies the new termios state after first
// discarding any input already queued in the kernel's terminal input
// buffer (equivalent to TCSAFLUSH in POSIX tcsetattr terms). Both of
// disableTerminalEcho's two writes — clearing ECHO before the read, and
// restoring the original state after — go through this same constant, so
// both transitions flush: a multi-line paste whose trailing lines would
// otherwise sit in the input buffer and get echoed/executed by the shell
// after the prompt exits, or answer a later prompt unasked, is discarded
// at each boundary instead.
const (
	ioctlReadTermios  = unix.TCGETS
	ioctlWriteTermios = unix.TCSETSF
)
