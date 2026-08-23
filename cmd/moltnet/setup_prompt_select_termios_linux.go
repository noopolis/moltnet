//go:build linux

package main

import "golang.org/x/sys/unix"

// ioctlWriteTermiosImmediate is enableRawSelectMode's restore-path ioctl
// request number: TCSETS, the immediate (POSIX TCSANOW-equivalent) variant,
// deliberately not TCSETSF (the flush variant ioctlWriteTermios,
// prompt_hidden_termios_linux.go, already uses for disableTerminalEcho).
// See setup_prompt_select_termios_darwin.go's doc comment for the full
// reasoning: TCSETSF's hazard here is not a pending read on the fd (an
// isolated probe shows the flush ioctl returns immediately against a
// pending non-canonical read with no queued output) but that it also
// flushes queued *output*, blocking until a far end that has stopped
// reading actually reads it — exactly what a test harness's demand-driven
// pty reader does once it stops looking for more input (readMasterUntil,
// see prompt_hidden_pty_test.go:201-217 and drainBackgroundMaster), and
// what a real terminal under XOFF/Ctrl-S flow control or a stalled emulator
// would also do.
//
// Unlike on darwin, this is a real behavioral change on Linux, not just a
// safer restore path: measured, TCSETS preserves whatever is left in the
// input queue at restore time while TCSETSF discards it. That distinction
// does not matter for this call site because enableRawSelectMode's own
// read loop (a bufio reader) already drains the input queue before restore
// ever runs — measured queuedBeforeRestore=0 for both a small keypress
// burst and a 6 KB paste — so there is nothing left in the queue for
// TCSETS's non-discarding restore to leak.
const ioctlWriteTermiosImmediate = unix.TCSETS
