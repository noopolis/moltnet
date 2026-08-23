//go:build darwin || linux

package main

import (
	"syscall"
	"testing"
)

// TestExitCodeForSignalGivesSIGTSTPItsOwnDistinguishableCode pins the fix
// for a real gap: exitCodeForSignal had no syscall.SIGTSTP arm, so it fell
// through to the generic default case (exit 1) -- indistinguishable from a
// genuine setup failure to a wrapper script inspecting the exit code, and
// unlike every other signal this function maps, which each get their own
// conventional 128+signal code.
func TestExitCodeForSignalGivesSIGTSTPItsOwnDistinguishableCode(t *testing.T) {
	want := 128 + int(syscall.SIGTSTP)
	if got := exitCodeForSignal(syscall.SIGTSTP); got != want {
		t.Fatalf("exitCodeForSignal(SIGTSTP) = %d, want %d", got, want)
	}
	// Every other mapped signal must be unaffected by this addition.
	cases := map[syscall.Signal]int{
		syscall.SIGHUP:  128 + 1,
		syscall.SIGINT:  128 + 2,
		syscall.SIGTERM: 128 + 15,
	}
	for sig, want := range cases {
		if got := exitCodeForSignal(sig); got != want {
			t.Fatalf("exitCodeForSignal(%v) = %d, want %d", sig, got, want)
		}
	}
	if got := exitCodeForSignal(syscall.SIGUSR1); got != 1 {
		t.Fatalf("exitCodeForSignal(SIGUSR1) = %d, want the generic default 1 for an unmapped signal", got)
	}
}
