package main

import (
	"os"
	"testing"
)

// requireTestPTY opens a fresh /dev/ptmx-backed pseudo-terminal pair via
// openTestPTY (openpty_linux_test.go / openpty_darwin_test.go) for the
// test's duration, closing both ends in cleanup, and skips the test
// gracefully — rather than failing it — when a pty cannot be allocated at
// all (e.g. no /dev/ptmx, a sandboxed environment without permission to
// open it). Every disableTerminalEcho/promptHidden test that needs a real
// terminal device (never this test binary's own controlling terminal —
// see the HARD RULE governing this change) goes through this one seam.
func requireTestPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()

	master, slave, err := openTestPTY()
	if err != nil {
		t.Skipf("skipping: could not allocate a test pty: %v", err)
	}
	t.Cleanup(func() {
		_ = slave.Close()
		_ = master.Close()
	})
	return master, slave
}
