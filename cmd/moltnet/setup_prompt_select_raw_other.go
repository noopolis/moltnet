//go:build !darwin && !linux

package main

import (
	"io"
	"os"
)

// enableRawSelectMode is setup_prompt_select_raw_unix.go's counterpart for
// every GOOS other than darwin and linux, mirroring
// prompt_hidden_echo_other.go's own split: there is no known termios ioctl
// mapping here, so a real interactive terminal always fails open
// (ok=false), and promptSelect falls back to the plain numbered-list
// prompt. Unlike prompt_hidden_echo_other.go's disableTerminalEcho (which
// must fail *closed* to protect a secret), this failing open is exactly the
// right default: a select prompt degrading to line-based input on an
// unsupported GOOS is a UX downgrade, not a correctness or security one.
func enableRawSelectMode() (restore func(), ok bool) {
	noop := func() {}
	if promptReader != io.Reader(os.Stdin) || !isInteractive() {
		return noop, true
	}
	return noop, false
}
