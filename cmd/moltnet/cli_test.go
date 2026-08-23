package main

import (
	"context"
	"strings"
	"testing"
)

// TestBareCommandPrintsHelpInsteadOfStartingServer is the PLAN 7A.5
// regression test: bare `moltnet` used to start a foreground daemon
// (runServer) with no explanation, which blocked a terminal for anyone who
// typed the bare command just to discover the tool — the help block never
// even advertised this as a way to start it (it lists `start`/`server`
// instead). Bare `moltnet` must now behave like `moltnet help`: print usage
// and return immediately, never blocking on runServer. This is asserted
// indirectly (there is no server to observe not-starting) by requiring
// run() to return promptly with a nil error and the same usage text `help`
// prints, which only happens if the "" case short-circuits before ever
// reaching runServer's blocking accept loop.
func TestBareCommandPrintsHelpInsteadOfStartingServer(t *testing.T) {
	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{}, "test")
	})

	if err != nil {
		t.Fatalf("run([]) error = %v, want nil", err)
	}
	if !strings.Contains(output, "Usage:") {
		t.Fatalf("run([]) stdout = %q, want it to contain the usage block", output)
	}
	if !strings.Contains(output, "moltnet start") {
		t.Fatalf("run([]) stdout = %q, want a next-step hint toward \"moltnet start\"", output)
	}
}

// TestBareCommandAndHelpProduceTheSameUsage pins bare `moltnet` and `moltnet
// help` to the same buildUsage() text (bare command additionally appends a
// "next:" hint start), guarding against the two drifting apart again now
// that they share the same short-circuit shape.
func TestBareCommandAndHelpProduceTheSameUsage(t *testing.T) {
	bareOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{}, "test"); err != nil {
			t.Fatalf("run([]) error = %v", err)
		}
	})
	helpOutput := captureStdout(t, func() {
		if err := run(context.Background(), []string{"help"}, "test"); err != nil {
			t.Fatalf("run([\"help\"]) error = %v", err)
		}
	})

	if !strings.Contains(bareOutput, buildUsage()) {
		t.Fatalf("bare command output = %q, want it to contain buildUsage()", bareOutput)
	}
	if helpOutput != buildUsage() {
		t.Fatalf("help output = %q, want exactly buildUsage()", helpOutput)
	}
}
