package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// childInvocation describes one child `moltnet <args...>` command the setup
// wizard drives through the runChild seam (SETUP.md's "Driving the
// terminal": exec with piped stdio, children fully flag-driven). Args is
// the argv after the program name itself, e.g. []string{"init", "--id",
// "acme"}.
//
// Env carries additional KEY=VALUE entries appended to the child's
// environment — the one and only channel a secret value (a Cloudflare API
// token, a room credential, ...) is ever allowed to reach a child through.
// Nothing in this package may put a secret in Args: argv is visible to
// every other process on the machine via `ps`, and it is also exactly what
// `--print-commands` prints back to the operator, where a literal secret
// would leak into a transcript or a copy-pasted script. See
// setup_plan.go's placeholder rendering for the print-side half of that
// rule.
type childInvocation struct {
	Args []string
	Env  []string
	// CaptureOutput marks the one documented exception to "child stdout is
	// always discarded" (SETUP.md's "Exception: the invite code"): `pair
	// invite show <pairing-id>` prints the invite code and nothing else, and
	// it is persisted nowhere else the wizard can read it back from. Setting
	// this tells the caller (setup_execute.go's runSetupStep) to keep
	// childResult.Stdout instead of discarding it, and to skip printing a
	// generic step checkmark line for it — the retrieved code gets its own,
	// dedicated presentation in the completion screen instead.
	CaptureOutput bool
}

// childResult is what runChild returns for one invocation. Stdout is empty
// unless the invocation set CaptureOutput: SETUP.md is explicit that a
// child's own stdout ("✓ ready", "next: ...", `service install`'s three-way
// next: block) would contradict the wizard's own single-line-per-step
// summary, so every other step's stdout is piped and simply never read.
// Stderr is always kept, on success or failure alike, so a *failing*
// invocation can surface the child's own diagnostic instead of a bare "exit
// status 1".
type childResult struct {
	Stdout string
	Stderr string
}

// runChild is the injectable seam every setup step invokes a child moltnet
// command through. execChild is the production implementation, spawning a
// real subprocess; cmd/moltnet's own tests swap it for an in-process
// adapter that dispatches through this same test binary's run() dispatcher
// instead (see setup_runner_test.go) — a literal exec.Command(os.Executable(),
// "init", ...) would re-invoke the `go test` binary itself under `go test`,
// not a real moltnet CLI (SETUP.md's "Driving the terminal", precondition
// 5), so production code must never call execChild directly; every setup
// step goes through this var.
var runChild = execChild

// execChild spawns currentExecutable() (uninstall.go's own symlink-resolved
// "which moltnet binary is this" seam, reused rather than duplicated) with
// invocation.Args, piped stdin/stdout/stderr — never the parent's inherited
// file descriptors, so the child always sees a non-TTY regardless of
// whether this process's own stdout happens to be a real terminal. That
// matters concretely: every existing interactivity gate in this CLI
// (playBanner's settle animation, promptHidden's TTY check, ...) already
// does the right, non-interactive thing the moment its stdin/stdout are not
// a genuine terminal, so a piped child degrades to flag-driven, silent
// behavior for free — no child-side flag needed to suppress it.
//
// Stdin is deliberately never connected to anything (cmd.Stdin left nil,
// which os/exec wires to an already-closed /dev/null-equivalent read): no
// child this wizard drives ever prompts for input — that is precisely what
// makes "every child input has a non-interactive channel" (SETUP.md
// precondition 1) a precondition of exec-piped working at all, not
// something this seam itself enforces.
//
// Stdout is captured into a throwaway buffer and never inspected: the
// wizard renders its own one-line-per-step transcript instead of relaying a
// child's (init.go:158-300, service.go:83-97). Stderr is captured too, but
// is returned in childResult so a failing step can surface *why* --
// wrapping cmd.Run()'s bare exit-status error with it would just repeat
// what the operator cannot otherwise see.
// TODO(setup-p2): cmd.Environ() below inherits the parent's full ambient
// environment, so a stray MOLTNET_LISTEN_ADDR (or any other env var a
// child's config load consults) already set in the operator's shell
// silently overrides the exact port this wizard just preflighted, with no
// warning that the child ignored it.
func execChild(ctx context.Context, invocation childInvocation) (childResult, error) {
	exePath, err := currentExecutable()
	if err != nil {
		return childResult{}, fmt.Errorf("resolve moltnet executable: %w", err)
	}

	cmd := exec.CommandContext(ctx, exePath, invocation.Args...)
	cmd.Env = append(cmd.Environ(), invocation.Env...)

	// Both buffers are capped at maxCapturedChildOutput regardless of
	// CaptureOutput -- stdoutBuf fills even for the steps whose output is
	// thrown away right below, so a runaway child (a stray infinite retry
	// loop, say) must not be able to grow either buffer without bound. A
	// capped buffer, not a size limit enforced after the fact: os/exec's
	// internal copy goroutines write to cmd.Stdout/cmd.Stderr directly and
	// treat a Write error as fatal to the copy, so the excess must be
	// silently discarded (never an error) rather than refused.
	stdoutBuf := newBoundedBuffer(maxCapturedChildOutput)
	stderrBuf := newBoundedBuffer(maxCapturedChildOutput)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	// stderr is always retained in the returned childResult, on success or
	// failure alike -- "captured and discarded on success" (SETUP.md)
	// describes stdout only. It is the *caller* (setup_execute.go) that
	// chooses never to print/surface a successful step's stderr; nothing
	// here decides that, since a step that succeeded loudly on stderr
	// (a deprecation notice, say) is a policy call for the wizard's own
	// summary rendering to make, not something this seam should already
	// have thrown away by the time that decision is reached. Read only
	// after cmd.Run() returns: stderrBuf fills in during Run, and Run does
	// not return until the copying goroutines it starts internally have
	// finished draining the pipe, so this is never a partial read.
	runErr := cmd.Run()
	result := childResult{Stderr: strings.TrimSpace(stderrBuf.String())}
	if invocation.CaptureOutput {
		result.Stdout = strings.TrimSpace(stdoutBuf.String())
	}
	if runErr != nil {
		return result, fmt.Errorf(
			"%s %s: %w", filepath.Base(exePath), strings.Join(invocation.Args, " "), runErr,
		)
	}
	return result, nil
}

// maxCapturedChildOutput bounds how much of a child's stdout/stderr
// execChild retains in memory, regardless of CaptureOutput -- every real
// invocation this wizard drives emits at most a few KB either way, so this
// is chosen generously, purely as a backstop against a runaway child.
const maxCapturedChildOutput = 1 << 20 // 1 MiB

// boundedBuffer is a bytes.Buffer that silently stops accepting new bytes
// once it holds limit bytes, discarding the remainder rather than growing
// forever. Write always reports success (never an error) for the full
// length of p regardless of how much was actually kept: cmd.Stdout/cmd.Stderr
// contracts require this -- os/exec's internal copy goroutine treats a
// Write error as fatal to the child's own I/O, so refusing bytes here must
// never turn into a failed or truncated child process, only a truncated
// capture.
type boundedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if remaining := b.limit - b.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			b.buf.Write(p[:remaining])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string { return b.buf.String() }
