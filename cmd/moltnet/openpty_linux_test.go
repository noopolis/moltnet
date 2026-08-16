//go:build linux

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openTestPTY allocates a fresh Unix98 pseudo-terminal pair — a master
// (control) side and its paired slave (child) side — via /dev/ptmx, the
// same device a real terminal emulator uses. Tests use it to exercise
// disableTerminalEcho's real termios ioctls against an actual terminal
// device, never the test's own controlling terminal (see HARD RULE in the
// task this backs). The caller owns closing both returned files.
func openTestPTY() (master, slave *os.File, err error) {
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	master = os.NewFile(uintptr(masterFD), "ptmx")

	// TIOCSPTLCK: unlock the slave side (Linux ptys start locked so nothing
	// can open the slave until the master explicitly unlocks it).
	if err := unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("unlock pty: %w", err)
	}

	n, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("get pty number: %w", err)
	}

	slavePath := fmt.Sprintf("/dev/pts/%d", n)
	slave, err = os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("open %s: %w", slavePath, err)
	}
	return master, slave, nil
}
