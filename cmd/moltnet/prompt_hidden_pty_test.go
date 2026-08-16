package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// These tests exercise disableTerminalEcho's real, non-noop termios path
// (P2-6) against a genuine pseudo-terminal (openpty_test.go), never this
// test binary's own controlling terminal — see the HARD RULE governing
// this change. disableTerminalEcho only engages when promptReader is the
// process's own os.Stdin (prompt_hidden.go), so the only way to reach that
// branch at all is a real, separate child process whose actual stdin is
// the pty's slave side; withPromptAnswers' swapped-io.Reader seam used
// everywhere else in this package deliberately never reaches it.
//
// TestPromptHiddenHelperProcess below is that child process's entrypoint:
// this same test binary re-execs itself (the standard os/exec_test.go
// TestHelperProcess pattern) with MOLTNET_PROMPT_HIDDEN_HELPER set to pick
// a scenario. Run normally (the env var unset), it does nothing and passes
// immediately — it never runs stray on its own.

// promptHiddenHelperEnv selects which scenario a re-exec'd copy of this
// test binary runs, in TestPromptHiddenHelperProcess.
const promptHiddenHelperEnv = "MOLTNET_PROMPT_HIDDEN_HELPER"

// TestPromptHiddenHelperProcess is never meaningfully run by `go test`
// itself — the driving tests below re-exec this same test binary with
// -test.run scoped to just this function and promptHiddenHelperEnv set,
// so it runs as its own separate process with its real os.Stdin/os.Stdout
// dup'd to a pty's slave side.
func TestPromptHiddenHelperProcess(t *testing.T) {
	scenario := os.Getenv(promptHiddenHelperEnv)
	if scenario == "" {
		return
	}

	switch scenario {
	case "guidance-then-read":
		// The real maybePromptForCloudflareToken flow (relay_deploy_token.go):
		// disables echo, prints the multi-line guidance block, then prints
		// the question and reads. Exercises the P3-7 fix's actual call
		// order, and promptHidden's nested no-op re-entry into an already-
		// suppressed disableTerminalEcho.
		token, err := maybePromptForCloudflareToken()
		if err != nil {
			fmt.Fprintf(os.Stderr, "HELPER-ERROR:%v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "HELPER-RESULT:%s\n", token)
	case "block-on-read":
		// Never returns on its own: the parent sends a signal instead, and
		// disableTerminalEcho's own handler is what's expected to exit.
		_, _ = promptHidden("paste token (input hidden): ")
		fmt.Fprintln(os.Stderr, "HELPER-ERROR:read returned without a signal")
		os.Exit(1)
	case "idempotent-restore":
		restore, ok := disableTerminalEcho()
		if !ok {
			fmt.Fprintln(os.Stderr, "HELPER-ERROR:disableTerminalEcho() ok = false")
			os.Exit(1)
		}
		restore()
		restore() // must not panic (P2-5) — a real termios path this time.
		fmt.Fprintln(os.Stderr, "HELPER-RESULT:OK")
	case "flush-then-confirm":
		// The P0 mid-deploy-typeahead fix (prompt_hidden_pty_fixes_test.go):
		// sleeps to model a slow Deploy() call, then flushes and prompts —
		// anything the test writes to the pty during the sleep must never
		// answer the prompt below.
		time.Sleep(300 * time.Millisecond)
		flushPendingTerminalInput()
		confirmed, err := promptYesNoDefaultYes("save? [Y/n] ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "HELPER-ERROR:%v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "HELPER-RESULT:%v\n", confirmed)
	case "echo-unavailable":
		// The P0 fail-open-echo fix (prompt_hidden_pty_fixes_test.go):
		// stubs the termios ioctl seam to always fail, then drives the real
		// maybePromptForCloudflareToken flow and reports what it returned.
		ioctlGetTermios = func(fd int, req uint) (*unix.Termios, error) {
			return nil, unix.EINVAL
		}
		token, err := maybePromptForCloudflareToken()
		if err == nil {
			fmt.Fprintln(os.Stderr, "HELPER-ERROR:expected an error, got nil")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "HELPER-RESULT:err=%v token=%q\n", err, token)
	case "claim-flush-then-attempt":
		// The P1 flush-before-claim-prompt fix's own real-pty coverage
		// (relay_deploy_subdomain_claim_pty_test.go): mirrors
		// "flush-then-confirm" above for
		// attemptInteractiveWorkersDevSubdomainClaim's own flush call.
		runClaimFlushThenAttemptHelperScenario()
	default:
		fmt.Fprintf(os.Stderr, "HELPER-ERROR:unknown scenario %q\n", scenario)
		os.Exit(1)
	}
}

// startPromptHiddenHelper re-execs this test binary as a fresh process
// running TestPromptHiddenHelperProcess under scenario, with its stdin and
// stdout dup'd to slave (a real pty's slave side — see requireTestPTY,
// openpty_test.go) and its stderr piped back for the structured
// HELPER-RESULT/HELPER-ERROR lines the scenarios above print. The caller
// still owns starting/signaling/waiting on the returned *exec.Cmd.
func startPromptHiddenHelper(t *testing.T, scenario string, slave *os.File) (cmd *exec.Cmd, stderr *bytes.Buffer) {
	t.Helper()
	cmd = exec.Command(os.Args[0], "-test.run=^TestPromptHiddenHelperProcess$")
	cmd.Env = append(os.Environ(), promptHiddenHelperEnv+"="+scenario)
	cmd.Stdin = slave
	cmd.Stdout = slave
	stderr = &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process (scenario %q): %v", scenario, err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd, stderr
}

// readMasterUntil reads from master (a pty's master side) on a background
// goroutine until its accumulated output contains substr, returning
// everything read so far. It fails the test if that does not happen within
// timeout, rather than hanging a broken test forever.
func readMasterUntil(t *testing.T, master *os.File, substr string, timeout time.Duration) string {
	t.Helper()
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		var buf bytes.Buffer
		chunk := make([]byte, 256)
		for {
			n, err := master.Read(chunk)
			if n > 0 {
				buf.Write(chunk[:n])
				if strings.Contains(buf.String(), substr) {
					done <- result{text: buf.String()}
					return
				}
			}
			if err != nil {
				done <- result{text: buf.String(), err: err}
				return
			}
		}
	}()

	select {
	case r := <-done:
		if r.err != nil && !strings.Contains(r.text, substr) {
			t.Fatalf("reading pty master for %q: %v (got %q so far)", substr, r.err, r.text)
		}
		return r.text
	case <-time.After(timeout):
		t.Fatalf("timed out after %s waiting for %q on the pty", timeout, substr)
		return ""
	}
}

// syncBuffer is a bytes.Buffer safe for one writer goroutine (
// drainMasterInBackground below) and concurrent readers (a test's own
// goroutine calling String() after the writer goroutine may still be
// running).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// drainMasterInBackground continuously reads master into a syncBuffer on a
// background goroutine until master.Read errors (in practice, when this
// test's t.Cleanup closes it) — modeling what a real terminal emulator
// actually does with its pty master: read and render continuously, never
// stopping. It exists because the P0 input-flush fix
// (prompt_hidden_termios_darwin.go / _linux.go) made disableTerminalEcho's
// restore write use the flush ioctl variant (TIOCSETAF / TCSETSF) for both
// echo-off and restore, and that variant blocks until any output the
// helper process still has queued has actually been read from the far end
// — exactly what a real terminal's continuous draining guarantees, but
// what this suite's earlier demand-driven readMasterUntil/drainMaster
// calls do not once they stop reading after finding a substring. Tests
// that reach a graceful (non-signal) restore after their last
// readMasterUntil call — unlike the signal/idempotent-restore scenarios,
// where nothing more is written after the last demand read — need this
// still running so the helper's own shutdown never stalls waiting for a
// reader that stopped showing up.
func drainMasterInBackground(master *os.File) *syncBuffer {
	buf := &syncBuffer{}
	go func() {
		chunk := make([]byte, 256)
		for {
			n, err := master.Read(chunk)
			if n > 0 {
				buf.Write(chunk[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	return buf
}

// masterEcho reports whether the ECHO local-mode bit is currently set on
// the terminal behind master. Querying/setting termios through a pty's
// master fd affects (and reads back) the same termios state as the slave
// side — confirmed empirically against a real openpty pair before writing
// this suite — so the parent process can observe exactly what the child
// process did to its own controlling terminal without touching the
// child's fds directly.
func masterEcho(t *testing.T, master *os.File) bool {
	t.Helper()
	state, err := unix.IoctlGetTermios(int(master.Fd()), ioctlReadTermios)
	if err != nil {
		t.Fatalf("IoctlGetTermios(master): %v", err)
	}
	return state.Lflag&unix.ECHO != 0
}

// waitForMasterEchoOff polls masterEcho until it reports ECHO cleared or
// timeout elapses, failing the test in the latter case. It exists for the
// bare-promptHidden scenarios ("block-on-read"), where — unlike
// maybePromptForCloudflareToken's guidance-first flow — the question text
// is printed *before* disableTerminalEcho runs (promptHidden's own
// question is short and non-secret, so this ordering is fine in
// production; see promptHidden's doc comment), which means "the question
// text is visible on the pty" does not, by itself, prove echo has been
// disabled yet the way it does for the guidance-block tests above. A short
// poll closes that real (if tiny) race deterministically instead of
// asserting on a single racy snapshot.
func waitForMasterEchoOff(t *testing.T, master *os.File, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if !masterEcho(t, master) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ECHO was still on %s after the helper's read prompt appeared", timeout)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestPromptHiddenPTYNormalPasteHiddenAndRestored is the full "normal
// paste" live-pty scenario end to end: it drives a real child process
// through the actual maybePromptForCloudflareToken flow over a real
// pseudo-terminal, and proves, against the kernel's own termios state
// (not just this package's own bookkeeping) —
//
//   - P2-6: disableTerminalEcho's real IoctlGetTermios/IoctlSetTermios path
//     actually clears and restores ECHO on a real terminal device.
//   - P3-7: ECHO is already off by the time the very first byte of the
//     guidance block reaches the terminal — deterministic, not a timing
//     race: printing happens strictly after disableTerminalEcho in
//     maybePromptForCloudflareToken's program order, so the first visible
//     output byte proves the ioctl already completed.
//   - The pasted token is never visible anywhere in the pty's output
//     stream, from the moment the child starts to the moment it exits —
//     true kernel-level suppression, not just an application choosing not
//     to print it.
//   - P3-8: echo is restored (ECHO back on) after the child's normal
//     return, proving promptHidden's deferred restore() ran.
func TestPromptHiddenPTYNormalPasteHiddenAndRestored(t *testing.T) {
	master, slave := requireTestPTY(t)

	cmd, stderr := startPromptHiddenHelper(t, "guidance-then-read", slave)

	// The very first output byte proves disableTerminalEcho already ran
	// (see the ordering argument above) — check it before reading anything
	// further.
	_ = readMasterUntil(t, master, "Create a Cloudflare API token", 5*time.Second)
	if masterEcho(t, master) {
		t.Fatal("ECHO is still on after the first byte of guidance output; disableTerminalEcho did not run before printing")
	}

	readMasterUntil(t, master, "paste token (input hidden): ", 5*time.Second)

	// From here on, keep draining master continuously in the background —
	// exactly what a real terminal emulator does — instead of the discrete
	// demand reads above. The P0 input-flush fix made disableTerminalEcho's
	// restore write use the flush ioctl variant (TIOCSETAF / TCSETSF),
	// which blocks until any output the helper still has queued (just the
	// trailing newline promptHidden prints once its read returns) has
	// actually been read from the far end; without a reader still running,
	// that would stall the helper's own graceful shutdown below. See
	// drainMasterInBackground's doc comment.
	trailing := drainMasterInBackground(master)

	const pasted = "s3cr3t-pasted-token"
	if _, err := master.Write([]byte(pasted + "\n")); err != nil {
		t.Fatalf("write pasted token to pty master: %v", err)
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("helper process exited with error: %v (stderr: %s)", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper process did not exit within 5s after the paste was written")
	}

	if got := stderr.String(); !strings.Contains(got, "HELPER-RESULT:"+pasted) {
		t.Fatalf("helper stderr = %q, want it to report the pasted token was read correctly", got)
	}

	// Whatever the helper wrote after the paste (just the trailing newline
	// promptHidden prints once the read returns) must not contain the
	// pasted token itself — ECHO was already confirmed off before the
	// paste was written and does not come back on until after this point,
	// so the kernel had no opportunity to echo it either.
	if got := trailing.String(); strings.Contains(got, pasted) {
		t.Fatalf("pasted token appeared in the pty's own output stream (echoed): %q", got)
	}

	if !masterEcho(t, master) {
		t.Fatal("ECHO is still off after the helper process returned normally; the deferred restore() did not run")
	}
}

// TestPromptHiddenPTYIdempotentRestore covers the P2-5 fix against a real
// termios path: calling the restore func disableTerminalEcho returns twice
// must not panic. Before termios landed, this was only testable through
// the no-op branch (see TestDisableTerminalEchoNoopWhenPromptReaderNotStdin,
// relay_deploy_prompt_test.go); this is the first test able to exercise
// the real signal.Stop/close(sigCh)/termios-restore path twice.
func TestPromptHiddenPTYIdempotentRestore(t *testing.T) {
	master, slave := requireTestPTY(t)

	cmd, stderr := startPromptHiddenHelper(t, "idempotent-restore", slave)

	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper process (idempotent-restore) exited with error: %v (stderr: %s)", err, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "HELPER-RESULT:OK") {
		t.Fatalf("helper stderr = %q, want HELPER-RESULT:OK (a double restore() call must not panic)", got)
	}
	if !masterEcho(t, master) {
		t.Fatal("ECHO is still off after the idempotent-restore helper exited")
	}
}
