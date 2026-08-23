package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// setupSelectSignalHelperEnv selects TestSetupSelectSignalHelperProcess'
// scenario in a re-exec'd copy of this test binary, following the same
// re-exec pattern prompt_hidden_pty_test.go's own TestPromptHiddenHelperProcess
// uses (its own env var, own scenario switch): P2-3's fix installs an
// os.Exit-driven signal handler for SIGHUP/SIGTSTP
// (setup_prompt_select_raw_unix.go), and exercising an os.Exit path safely
// needs a genuinely separate process — running it in this test binary's own
// process would kill the whole `go test` run, not just the scenario under
// test.
const setupSelectSignalHelperEnv = "MOLTNET_SETUP_SELECT_SIGNAL_HELPER"

// TestSetupSelectSignalHelperProcess is never meaningfully run by `go test`
// itself — the driving tests below re-exec this same test binary with
// -test.run scoped to just this function and setupSelectSignalHelperEnv
// set, so it runs as its own separate process with its real
// os.Stdin/os.Stdout dup'd to a pty's slave side (promptReader defaults to
// os.Stdin at package init, so no extra seam-swapping is needed inside this
// fresh process — it already is the real terminal path).
func TestSetupSelectSignalHelperProcess(t *testing.T) {
	if os.Getenv(setupSelectSignalHelperEnv) == "" {
		return
	}
	options := []selectOption{{label: "alpha"}, {label: "beta"}}
	_, _, err := promptSelect(context.Background(), "Pick one", options, 0)
	// Only reached if the signal handler's os.Exit did not fire (a
	// regression): the parent asserts on the process exit code, not this
	// line, but reporting it aids debugging a failed run.
	fmt.Fprintf(os.Stderr, "HELPER-RESULT:err=%v\n", err)
}

// startSetupSelectSignalHelper re-execs this test binary as a fresh process
// running TestSetupSelectSignalHelperProcess, with its stdin and stdout
// dup'd to slave (a real pty's slave side — requireTestPTY, openpty_test.go)
// and its stderr piped back. The caller owns starting/signaling/waiting on
// the returned *exec.Cmd, mirroring startPromptHiddenHelper's own contract
// (prompt_hidden_pty_test.go) without sharing its helper process or env var.
func startSetupSelectSignalHelper(t *testing.T, slave *os.File) (cmd *exec.Cmd, stderr *bytes.Buffer) {
	t.Helper()
	cmd = exec.Command(os.Args[0], "-test.run=^TestSetupSelectSignalHelperProcess$")
	cmd.Env = append(os.Environ(), setupSelectSignalHelperEnv+"=1")
	cmd.Stdin = slave
	cmd.Stdout = slave
	stderr = &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd, stderr
}

// TestSetupSelectPTYSignalsRestoreRawModeAndExit is the P2-3 fix's own
// real-pty coverage: SIGHUP and SIGTSTP arriving while a select question is
// genuinely blocked reading a keypress must each restore the terminal (not
// leave ECHO|ICANON cleared indefinitely, worse than promptHidden's own
// ECHO-only exposure) and exit with the conventional 128+signal code —
// exactly the guarantee setup_prompt_select_raw_unix.go's doc comment now
// makes, and the gap the finding's own doc-comment quote ("ISIG is
// deliberately left untouched... that reasoning holds for SIGINT only")
// called out as previously unhandled.
func TestSetupSelectPTYSignalsRestoreRawModeAndExit(t *testing.T) {
	cases := []struct {
		name     string
		signal   syscall.Signal
		wantCode int
	}{
		{"SIGHUP", syscall.SIGHUP, 128 + 1},
		{"SIGTSTP", syscall.SIGTSTP, 128 + int(syscall.SIGTSTP)}, // exitCodeForSignal's own SIGTSTP arm (prompt_hidden_echo_unix.go): no re-suspend attempt here, but a distinguishable code, not the generic default-case 1.
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			master, slave := requireTestPTY(t)
			cmd, stderr := startSetupSelectSignalHelper(t, slave)

			readMasterUntil(t, master, "Pick one", 5*time.Second)
			if !rawSelectModeActive(t, master) {
				t.Fatalf("raw select mode is not active while the helper's prompt is showing; stderr: %s", stderr.String())
			}

			if err := cmd.Process.Signal(tc.signal); err != nil {
				t.Fatalf("send %s to helper process: %v", tc.name, err)
			}

			waitErr := make(chan error, 1)
			go func() { waitErr <- cmd.Wait() }()

			select {
			case err := <-waitErr:
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("helper process wait error = %v (%T), want *exec.ExitError; stderr: %s", err, err, stderr.String())
				}
				if got := exitErr.ExitCode(); got != tc.wantCode {
					t.Fatalf("helper process exit code = %d, want %d; stderr: %s", got, tc.wantCode, stderr.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("helper process did not exit within 5s of %s; stderr: %s", tc.name, stderr.String())
			}

			if rawSelectModeActive(t, master) {
				t.Fatalf("raw select mode is still active after %s killed the helper process", tc.name)
			}
		})
	}
}
