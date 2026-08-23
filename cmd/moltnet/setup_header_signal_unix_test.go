//go:build darwin || linux

package main

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// TestStopWatchingResizeUnregistersBeforeClosingChannel pins the fix for a
// real gap: gutting stopWatchingResize (leaking the signal.Notify
// registration below instead of undoing it with signal.Stop) used to leave
// the rest of the suite green. That ordering is not cosmetic -- if ch were
// closed while still registered, a SIGWINCH arriving afterward would still
// try to reach it: Go's signal delivery has no "channel already closed"
// protection, so that send panics the whole process, crashing every
// `moltnet setup` run a resize happened to land in right after the header
// stopped watching for one. This test raises a real SIGWINCH after
// stopWatchingResize returns and simply requires the process to survive it.
func TestStopWatchingResizeUnregistersBeforeClosingChannel(t *testing.T) {
	h := &setupHeader{}
	h.watchResize()
	h.stopWatchingResize()

	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("raise SIGWINCH: %v", err)
	}
	// If signal.Stop were skipped, the runtime's signal-delivery goroutine
	// would attempt (and panic on) a send to h's now-closed channel
	// somewhere in this window -- surviving it without a crash is the
	// entire assertion.
	time.Sleep(50 * time.Millisecond)
}
