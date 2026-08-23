package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

// withInProcessRunChild swaps runChild for the wizard's own tests with an
// adapter that dispatches straight into this test binary's run() dispatcher
// (cli.go) instead of spawning a real subprocess — the same reason
// cmd/moltnet's existing tests call run()/runCLI directly rather than
// exec'ing a built binary: os.Executable() under `go test` is the test
// binary itself, so a literal execChild call would try to run "moltnet.test
// init" and fail by construction (see runChild's own doc comment, runner.go).
//
// This is also, deliberately, the fix for the exact hazard SETUP.md calls
// out by name: under a PTY-backed setup test, the package-global `stdout`
// var can be a genuine terminal file for the *outer* wizard process, and a
// naive in-process adapter sharing that same var with the "child" call
// would let init play its settle animation and treat its own prompts as
// interactive (banner.go:51-53, isOutputTerminal's *os.File type check,
// style.go). runChildInProcess closes that gap by swapping stdout/stderr to
// plain *bytes.Buffer values for the inner call's duration: isOutputTerminal's
// default implementation type-asserts stdout to *os.File, so once it is a
// buffer, the child-side call sees a non-TTY regardless of what the outer
// test's own stdout was.
//
// P2-6: the original fix only closed half of that same hazard. promptReader
// and isInteractive (the stdin-side halves of the exact gate promptSelect
// itself uses, setup_prompt_select.go) were left untouched, so a nested
// "child" dispatched under a PTY-backed outer setup test would still see
// the outer wizard's own real os.Stdin/isInteractive()=true — a child that
// ever prompted would read the wizard's own terminal, the mirror of the
// stdout hazard this helper already exists to close. Nothing this unit
// ships actually prompts today, but forcing both false/empty here removes
// the hazard outright rather than leaving it latent for the next child that
// does (see TestRunChildInProcessForcesNonInteractiveForNestedPrompts,
// below, which proves it: a nested `setup` invocation must hit its own
// non-interactive refusal, never the outer test's scripted answers).
func withInProcessRunChild(t *testing.T) {
	t.Helper()
	previous := runChild
	runChild = runChildInProcess
	t.Cleanup(func() { runChild = previous })
}

// runChildInProcess is withInProcessRunChild's implementation: it holds
// processStateMu (main_test.go) for the same reason every other stdout-
// swapping test helper in this package does (captureStdoutLocked,
// captureMainOutput, captureStdoutAndStderr) — stdout/stderr are package
// globals, so two tests mutating them concurrently would race — applies
// invocation.Env as real process environment variables for the call's
// duration (production execChild passes these as real child env vars;
// run()'s own commands read them via os.Getenv, so this is the in-process
// equivalent, not a simulation of it), and dispatches through run() with a
// background context — never invocation's own ctx — matching execChild's
// contract that a child is a fully separate, non-interactive process the
// wizard does not get to cancel by proxy once started.
func runChildInProcess(_ context.Context, invocation childInvocation) (childResult, error) {
	processStateMu.Lock()
	defer processStateMu.Unlock()

	restoreEnv := setTestEnv(invocation.Env)
	defer restoreEnv()

	previousStdout, previousStderr := stdout, stderr
	var stdoutBuf, stderrBuf bytes.Buffer
	stdout, stderr = &stdoutBuf, &stderrBuf
	defer func() { stdout, stderr = previousStdout, previousStderr }()

	previousReader := promptReader
	promptReader = strings.NewReader("")
	defer func() { promptReader = previousReader }()

	previousInteractive := isInteractive
	isInteractive = func() bool { return false }
	defer func() { isInteractive = previousInteractive }()

	// stderr is retained regardless of outcome, matching execChild's own
	// contract (runner.go): only stdout is ever discarded, and only when the
	// invocation did not request CaptureOutput (the `pair invite show`
	// exception — SETUP.md's "Exception: the invite code").
	err := run(context.Background(), invocation.Args, "test")
	result := childResult{Stderr: strings.TrimSpace(stderrBuf.String())}
	if invocation.CaptureOutput {
		result.Stdout = strings.TrimSpace(stdoutBuf.String())
	}
	return result, err
}

// TestRunChildInProcessForcesNonInteractiveForNestedPrompts is P2-6's
// mutation-proof regression test: it simulates the exact hazard the fix
// above closes by forcing the *outer* isInteractive/promptReader into the
// state a PTY-backed setup test leaves them in, then dispatching a nested
// "setup" invocation through runChildInProcess directly. Before the fix,
// the nested call would see the outer isInteractive()=true and the outer
// scripted promptReader untouched, and would attempt to prompt through
// them — consuming the outer test's own scripted answer text as a select
// choice and failing with a "not recognized" parse error, or worse,
// silently answering from input meant for the outer wizard. With the fix,
// the nested call sees isInteractive()=false regardless of the outer
// state, and hits its own non-interactive refusal (runSetup, setup.go)
// instead.
func TestRunChildInProcessForcesNonInteractiveForNestedPrompts(t *testing.T) {
	withTestDefaultPort(t)
	t.Setenv("HOME", t.TempDir())

	previousInteractive := isInteractive
	isInteractive = func() bool { return true } // simulate the outer PTY-backed wizard's own terminal state.
	t.Cleanup(func() { isInteractive = previousInteractive })

	outerReader := strings.NewReader("should never be read by the nested child\n")
	previousReader := promptReader
	promptReader = outerReader
	t.Cleanup(func() { promptReader = previousReader })

	_, err := runChildInProcess(context.Background(), childInvocation{Args: []string{"setup"}})
	if err == nil {
		t.Fatal("expected the nested setup child to refuse non-interactively, got nil error")
	}
	if !strings.Contains(err.Error(), "requires a terminal") {
		t.Fatalf("nested child error = %v, want the non-interactive refusal (runChildInProcess must force isInteractive false for the call's duration)", err)
	}

	if !isInteractive() {
		t.Fatal("isInteractive was not restored to its previous (outer) value after runChildInProcess returned")
	}
	if promptReader != io.Reader(outerReader) {
		t.Fatal("promptReader was not restored to its previous (outer) value after runChildInProcess returned")
	}
}

// setTestEnv applies kv ("KEY=VALUE") pairs as real process environment
// variables and returns a func restoring whatever was there before —
// os/exec's real Env slice is per-process, but the in-process adapter above
// has no such isolation available, so this is the closest equivalent for a
// dispatch that runs inside this same test process.
func setTestEnv(kv []string) func() {
	type saved struct {
		key      string
		value    string
		hadValue bool
	}
	var restores []saved
	for _, entry := range kv {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		previous, had := os.LookupEnv(key)
		restores = append(restores, saved{key: key, value: previous, hadValue: had})
		os.Setenv(key, value)
	}
	return func() {
		for _, r := range restores {
			if r.hadValue {
				os.Setenv(r.key, r.value)
			} else {
				os.Unsetenv(r.key)
			}
		}
	}
}
