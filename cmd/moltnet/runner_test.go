package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withScratchScriptExecutable writes a throwaway shell script as
// currentExecutable's target for the test's duration — the same seam
// uninstall_test.go's withScratchExecutable uses, but with a real script
// body instead of a no-op stub, since these tests need to observe what
// execChild actually connects the child's stdio to.
func withScratchScriptExecutable(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-moltnet")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write scratch script: %v", err)
	}

	previous := currentExecutable
	currentExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { currentExecutable = previous })
	return path
}

// TestExecChildPipesNonTTYStdio is the runner seam's core contract test
// (SETUP.md's "Driving the terminal": exec with piped stdio, never
// inherited): a child spawned through execChild must see a closed stdin and
// a piped (non-TTY) stdout/stderr regardless of whether this test process's
// own file descriptors are a real terminal — go test's are not, which is
// exactly the point: execChild must not simply inherit whatever this
// process has, it must always pipe.
//
// The script reports all three streams' TTY-ness on stderr, including its
// own -- otherwise execChild's stderr piping is unverified by this test:
// stderr is the one stream this test can observe at all (stdout is
// discarded unless CaptureOutput is set), so a script that never checked
// isatty(2) on its own fd 2 could pass even if execChild silently connected
// the child's stderr to something TTY-backed instead of the pipe runner.go
// promises.
func TestExecChildPipesNonTTYStdio(t *testing.T) {
	script := "#!/bin/sh\n" +
		"if [ -t 0 ]; then echo stdin-is-tty >&2; else echo stdin-not-tty >&2; fi\n" +
		"if [ -t 1 ]; then echo stdout-is-tty >&2; else echo stdout-not-tty >&2; fi\n" +
		"if [ -t 2 ]; then echo stderr-is-tty >&2; else echo stderr-not-tty >&2; fi\n" +
		"exit 0\n"
	withScratchScriptExecutable(t, script)

	result, err := execChild(context.Background(), childInvocation{Args: []string{"probe"}})
	if err != nil {
		t.Fatalf("execChild() error = %v", err)
	}
	if !strings.Contains(result.Stderr, "stdin-not-tty") {
		t.Fatalf("expected child to see a closed/non-TTY stdin, stderr = %q", result.Stderr)
	}
	if !strings.Contains(result.Stderr, "stdout-not-tty") {
		t.Fatalf("expected child to see a piped/non-TTY stdout, stderr = %q", result.Stderr)
	}
	if !strings.Contains(result.Stderr, "stderr-not-tty") {
		t.Fatalf("expected child to see a piped/non-TTY stderr, stderr = %q", result.Stderr)
	}
}

// TestExecChildDiscardsStdoutOnSuccess proves "child stdout is suppressed,
// not rendered": a successful child's stdout is captured by execChild and
// never surfaced anywhere a caller can observe — childResult carries no
// stdout field at all, so there is nothing for a caller to accidentally
// relay into the wizard's transcript. Its stderr, by contrast, is retained
// even on success (runner.go's own doc comment: only stdout is discarded on
// success; stderr is always retained, and it is the caller's choice not to
// print/surface it when a step succeeds) — asserted here too, so a future
// change collapsing that distinction is caught immediately.
func TestExecChildDiscardsStdoutOnSuccess(t *testing.T) {
	script := "#!/bin/sh\necho 'noisy child banner'\necho 'retained stderr line' >&2\nexit 0\n"
	withScratchScriptExecutable(t, script)

	result, err := execChild(context.Background(), childInvocation{Args: []string{"noisy"}})
	if err != nil {
		t.Fatalf("execChild() error = %v", err)
	}
	if result.Stderr != "retained stderr line" {
		t.Fatalf("result.Stderr = %q, want the child's stderr retained even on success", result.Stderr)
	}
}

// TestExecChildSurfacesStderrOnFailure: a failing child's stderr must come
// back in childResult (runner.go's own doc comment) so the wizard can name
// what actually went wrong, rather than a bare "exit status 1".
func TestExecChildSurfacesStderrOnFailure(t *testing.T) {
	script := "#!/bin/sh\necho 'boom: could not bind port' >&2\nexit 1\n"
	withScratchScriptExecutable(t, script)

	result, err := execChild(context.Background(), childInvocation{Args: []string{"fail"}})
	if err == nil {
		t.Fatal("expected execChild to return an error for a nonzero exit")
	}
	if result.Stderr != "boom: could not bind port" {
		t.Fatalf("result.Stderr = %q, want the child's stderr", result.Stderr)
	}
	if !strings.Contains(err.Error(), "fake-moltnet fail") {
		t.Fatalf("error %q should name the binary and argv that failed", err.Error())
	}
}

// TestExecChildPassesEnvNeverArgv proves the "never pass secrets on argv"
// rule's mechanics: a value threaded through childInvocation.Env reaches the
// child (via its real process environment), so callers never need to put it
// in Args.
func TestExecChildPassesEnvNeverArgv(t *testing.T) {
	script := "#!/bin/sh\necho \"secret=$MOLTNET_TEST_SECRET\" >&2\nexit 0\n"
	withScratchScriptExecutable(t, script)

	result, err := execChild(context.Background(), childInvocation{
		Args: []string{"probe"},
		Env:  []string{"MOLTNET_TEST_SECRET=shh-do-not-argv-me"},
	})
	if err != nil {
		t.Fatalf("execChild() error = %v", err)
	}
	if !strings.Contains(result.Stderr, "secret=shh-do-not-argv-me") {
		t.Fatalf("expected env var to reach the child, stderr = %q", result.Stderr)
	}
}

// TestBoundedBufferDiscardsExcessWithoutErroring pins the fix for a real
// resource gap: execChild's stdout/stderr capture used to be a plain
// bytes.Buffer with no cap, so a runaway child (a stray infinite retry
// loop, say) could grow either buffer without bound. boundedBuffer must
// keep exactly the first limit bytes, discard everything after, and never
// report a Write error for the discarded remainder -- os/exec's internal
// copy goroutine treats a Write error as fatal to the child's own I/O, so a
// full buffer must never turn into a failed or truncated child process.
func TestBoundedBufferDiscardsExcessWithoutErroring(t *testing.T) {
	const limit = 8
	b := newBoundedBuffer(limit)

	n, err := b.Write([]byte("0123456789")) // 10 bytes, 2 over the limit.
	if err != nil {
		t.Fatalf("Write() error = %v, want nil even when the buffer is over its limit", err)
	}
	if n != 10 {
		t.Fatalf("Write() n = %d, want the full 10 reported regardless of how much was kept", n)
	}
	if got := b.String(); got != "01234567" {
		t.Fatalf("String() = %q, want the first %d bytes only", got, limit)
	}

	// A second Write once already full must also report success and add
	// nothing further.
	n, err = b.Write([]byte("more"))
	if err != nil {
		t.Fatalf("second Write() error = %v, want nil", err)
	}
	if n != 4 {
		t.Fatalf("second Write() n = %d, want 4 (the full length, even though nothing more was kept)", n)
	}
	if got := b.String(); got != "01234567" {
		t.Fatalf("String() after a second Write past the limit = %q, want unchanged %q", got, "01234567")
	}
}

// TestExecChildMissingExecutable exercises currentExecutable's own error
// path (an unresolvable moltnet binary) rather than a script failure.
func TestExecChildMissingExecutable(t *testing.T) {
	previous := currentExecutable
	currentExecutable = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { currentExecutable = previous })

	if _, err := execChild(context.Background(), childInvocation{Args: []string{"init"}}); err == nil {
		t.Fatal("expected an error when currentExecutable fails")
	}
}
