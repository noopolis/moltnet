package main

import (
	"bytes"
	"os"
	"testing"
)

// captureMainOutput, captureStderr, and captureStdoutAndStderr live in their
// own file, split out of main_test.go (which was approaching the repo's
// 400-line limit) once the P2 fix moved reportConsoleServerDown's failure
// prose from stdout to stderr: captureStderr/captureStdoutAndStderr mirror
// captureStdout/captureStdoutLocked's shape (still in main_test.go) but read
// from the stderr package var (main.go) instead of, or in addition to,
// stdout.

// captureMainOutput sets os.Args to args for the duration of fn (restoring
// it after) and returns everything fn wrote to stdout — for the handful of
// tests that exercise main()/runMain() directly rather than calling run()
// with an explicit argument slice.
func captureMainOutput(t *testing.T, args []string, fn func()) string {
	t.Helper()

	processStateMu.Lock()
	defer processStateMu.Unlock()

	previousArgs := os.Args
	defer func() { os.Args = previousArgs }()
	os.Args = append([]string(nil), args...)

	return captureStdoutLocked(t, fn)
}

// captureStderr swaps the stderr package var for the duration of fn and
// returns everything written to it, mirroring captureStdout's shape for the
// handful of callers (reportConsoleServerDown, console.go) that deliberately
// write to standard error instead of standard output.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	processStateMu.Lock()
	defer processStateMu.Unlock()

	current := stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer reader.Close()

	stderr = writer
	defer func() {
		stderr = current
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(reader); err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	return buffer.String()
}

// captureStdoutAndStderr swaps both the stdout and stderr package vars for
// the duration of fn and returns everything written to each, for the tests
// that need to assert on both at once -- e.g. that `moltnet console --print`
// against a down server leaves stdout empty (safe for
// `URL=$(moltnet console --print)`) while the failure prose lands on stderr
// instead.
func captureStdoutAndStderr(t *testing.T, fn func()) (stdoutOutput, stderrOutput string) {
	t.Helper()

	processStateMu.Lock()
	defer processStateMu.Unlock()

	previousStdout, previousStderr := stdout, stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer stdoutReader.Close()
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer stderrReader.Close()

	stdout, stderr = stdoutWriter, stderrWriter
	defer func() {
		stdout, stderr = previousStdout, previousStderr
	}()

	fn()

	if err := stdoutWriter.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}

	var stdoutBuffer, stderrBuffer bytes.Buffer
	if _, err := stdoutBuffer.ReadFrom(stdoutReader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if _, err := stderrBuffer.ReadFrom(stderrReader); err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	return stdoutBuffer.String(), stderrBuffer.String()
}
