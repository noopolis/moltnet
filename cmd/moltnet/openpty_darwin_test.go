//go:build darwin

package main

import (
	"bytes"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openTestPTY allocates a fresh Unix98 pseudo-terminal pair — a master
// (control) side and its paired slave (child) side — via /dev/ptmx, the
// same device a real terminal emulator uses. Tests use it to exercise
// disableTerminalEcho's real termios ioctls against an actual terminal
// device, never the test's own controlling terminal (see HARD RULE in the
// task this backs). The caller owns closing both returned files.
//
// Darwin's pty-allocation sequence differs from Linux's (openpty_linux_test.go):
// TIOCPTYGRANT then TIOCPTYUNLK prepare the slave, and TIOCPTYGNAME reads
// its device path back as a NUL-terminated C string into a fixed buffer —
// there is no golang.org/x/sys/unix helper for that last one (it is not an
// int- or Termios-shaped ioctl), so it goes through unix.Syscall directly.
func openTestPTY() (master, slave *os.File, err error) {
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	master = os.NewFile(uintptr(masterFD), "ptmx")

	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(masterFD), uintptr(unix.TIOCPTYGRANT), 0); errno != 0 {
		_ = master.Close()
		return nil, nil, fmt.Errorf("grant pty: %w", errno)
	}
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(masterFD), uintptr(unix.TIOCPTYUNLK), 0); errno != 0 {
		_ = master.Close()
		return nil, nil, fmt.Errorf("unlock pty: %w", errno)
	}

	var nameBuf [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(masterFD), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&nameBuf[0]))); errno != 0 {
		_ = master.Close()
		return nil, nil, fmt.Errorf("get pty slave name: %w", errno)
	}
	end := bytes.IndexByte(nameBuf[:], 0)
	if end < 0 {
		end = len(nameBuf)
	}
	slavePath := string(nameBuf[:end])

	slave, err = os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("open %s: %w", slavePath, err)
	}
	return master, slave, nil
}
