//go:build darwin || linux

package main

import (
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// ioctlGetTermios/ioctlSetTermios are disableTerminalEcho's and
// flushPendingTerminalInput's only points of contact with the kernel — vars,
// not direct unix.IoctlGetTermios/IoctlSetTermios calls, so a test can
// inject a get/set failure (the P0 fail-closed-echo coverage in
// prompt_hidden_pty_test.go) without needing a real terminal device that
// can be made to fail an ioctl on demand, which a pty pair cannot do.
var (
	ioctlGetTermios = unix.IoctlGetTermios
	ioctlSetTermios = unix.IoctlSetTermios
)

// disableTerminalEcho turns off local echo on the controlling terminal
// before promptHidden reads a pasted token, so the token's characters
// never appear on screen or in a captured terminal transcript, and returns
// a restore func that puts the terminal's exact prior state back.
//
// It talks directly to the kernel via golang.org/x/sys/unix termios
// ioctls (through the ioctlGetTermios/ioctlSetTermios vars above) on
// os.Stdin's file descriptor — no `stty` subprocess, no PATH lookup, no
// exec window during which a pasted token could theoretically be observed
// by another process inspecting argv. Only the ECHO local-mode bit is
// cleared; ICANON (and everything else) is left exactly as the terminal
// had it, so line editing (backspace, etc.) during the paste still works
// normally — it just isn't drawn. Both the echo-off write here and the
// restore write below go through ioctlWriteTermios, which is the
// *flush* ioctl variant (TIOCSETAF / TCSETSF —
// prompt_hidden_termios_darwin.go / _linux.go): any input already queued
// in the kernel's input buffer at either transition is discarded, closing
// the P0 tail-of-a-multi-line-paste leak (queued lines getting
// echoed/executed by the shell after this prompt exits, or silently
// answering a later prompt).
//
// It is a no-op — returning (a no-op restore func, ok=true) — whenever
// promptReader is not the real os.Stdin (the test seam, or any future
// non-interactive caller), stdin is not a real terminal, or echo is
// already suppressed by an outer, still-active call (echoSuppressed
// below): there is nothing to do, and nothing needed doing, in any of
// those cases. It fails closed — (a no-op restore func, ok=false) — if the
// termios ioctls themselves error: isInteractive already confirmed stdin
// is a TTY, so this should not happen in practice, but when it does the
// caller (promptHidden) must refuse to read rather than silently letting a
// secret echo to the screen (P0 fail-open-echo fix; see
// errTerminalEchoUnavailable, prompt_hidden.go).
//
// The nesting note: an outer, still-active disableTerminalEcho call (e.g.
// maybePromptForCloudflareToken disabling echo before printing its
// guidance block, then promptHidden's own call right before its read)
// already owns both the termios restoration and the signal handling for
// the whole window; this no-ops rather than re-toggling an already-off
// ECHO bit and registering a second signal handler for the same window.
//
// The returned restore func is idempotent (sync.Once-guarded) and safe to
// call more than once: once via the deferred call in promptHidden after
// the read returns, and once from the signal handler registered here, so
// a SIGINT/SIGTERM/SIGHUP during the paste still restores echo — and
// exits the process — before it would otherwise leave the terminal
// silently un-echoing keystrokes.
//
// SIGTSTP (Ctrl-Z) gets its own handling instead of being folded into the
// SIGINT/SIGTERM/SIGHUP exit path: on SIGTSTP the terminal is restored to
// its normal (echoing) state, SIGTSTP's disposition is reset to default
// and re-raised against this process's own group in an attempt to let the
// kernel do the actual stopping, and — once that unblocks — echo is
// re-suppressed (a fresh ioctl, independent of the once-guarded final
// restore, which must still be free to run later) and SIGTSTP is re-armed
// for the next attempt.
//
// In practice, this only achieves the echo part. Go's signal.Reset cannot
// actually restore the OS default stop semantics for a running Go
// process, so the re-raised SIGTSTP does not suspend it: Ctrl-Z during
// this prompt is effectively inert. What the user sees is echo blinking
// off, briefly on, then off again while the process keeps running — and
// the terminal is still left in the correct (restored) state when the
// prompt exits normally or via SIGINT/SIGTERM/SIGHUP. Do not "fix" the
// apparent no-op by looping or retrying the kill: there is no way to make
// signal.Reset hand back genuine default stop behavior here; a real
// suspend would need a mechanism outside signal.Reset entirely.
//
// This covers SIGINT/SIGTERM/SIGHUP/SIGTSTP specifically; it is not a
// guarantee against every termination path (SIGKILL, a crash, or a killed
// parent process can still leave echo off — recoverable with `stty sane`
// or `reset`).
func disableTerminalEcho() (restore func(), ok bool) {
	noop := func() {}
	if promptReader != io.Reader(os.Stdin) || !isInteractive() {
		return noop, true
	}
	if echoSuppressed {
		return noop, true
	}

	fd := int(os.Stdin.Fd())
	original, err := ioctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return noop, false
	}

	raw := *original
	raw.Lflag &^= unix.ECHO
	if err := ioctlSetTermios(fd, ioctlWriteTermios, &raw); err != nil {
		return noop, false
	}
	echoSuppressed = true

	var restoreOnce sync.Once
	restoreTermios := func() {
		restoreOnce.Do(func() {
			_ = ioctlSetTermios(fd, ioctlWriteTermios, original)
		})
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGTSTP)
	go func() {
		for sig := range sigCh {
			if sig == syscall.SIGTSTP {
				_ = ioctlSetTermios(fd, ioctlWriteTermios, original)
				signal.Reset(syscall.SIGTSTP)
				_ = syscall.Kill(0, syscall.SIGTSTP) // blocks here until SIGCONT
				if current, err := ioctlGetTermios(fd, ioctlReadTermios); err == nil {
					r := *current
					r.Lflag &^= unix.ECHO
					_ = ioctlSetTermios(fd, ioctlWriteTermios, &r)
				}
				signal.Notify(sigCh, syscall.SIGTSTP)
				continue
			}
			restoreTermios()
			os.Exit(exitCodeForSignal(sig))
		}
	}()

	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			signal.Stop(sigCh)
			close(sigCh)
			restoreTermios()
			echoSuppressed = false
		})
	}, true
}

// echoSuppressed tracks whether an active, not-yet-restored
// disableTerminalEcho call already has the controlling terminal's ECHO bit
// cleared. The whole interactive-prompt flow runs synchronously on a
// single goroutine (no concurrent prompts), so a plain bool is enough; see
// disableTerminalEcho's nesting note above for why this exists.
var echoSuppressed bool

// exitCodeForSignal maps a signal disableTerminalEcho watches for to the
// conventional 128+signal-number shell exit code (bash and most other
// shells report a signal-terminated process this way), so a SIGINT/SIGTERM
// /SIGHUP during the token paste exits with the same code a caller would
// see if the default disposition had killed the process directly.
func exitCodeForSignal(sig os.Signal) int {
	switch sig {
	case syscall.SIGHUP:
		return 128 + 1
	case syscall.SIGINT:
		return 128 + 2
	case syscall.SIGTERM:
		return 128 + 15
	default:
		return 1
	}
}

// flushPendingTerminalInput discards any input already queued in the
// kernel's terminal input buffer for the controlling terminal, without
// changing any termios flag. relay_deploy_token.go calls it immediately
// before the post-deploy save prompts (maybeSaveCloudflareToken,
// maybeSavePastedCloudflareToken): a Cloudflare deploy can take several
// seconds, and any key(s) the operator presses during that window — most
// commonly a stray Enter — would otherwise still be sitting in the input
// buffer by the time the save prompt's read starts, silently answering a
// question the operator never actually saw (the P0 mid-deploy-typeahead
// fix). It works by reapplying the terminal's current termios state to
// itself through ioctlWriteTermios, the same flush-variant ioctl
// disableTerminalEcho's own writes use (TIOCSETAF / TCSETSF —
// prompt_hidden_termios_darwin.go / _linux.go): no flag changes, just the
// kernel-level input-queue discard that write implies.
//
// Unlike disableTerminalEcho, this is unconditionally best-effort: it only
// ever runs on the real terminal path (promptReader is the real os.Stdin
// and isInteractive() is true — the same gate disableTerminalEcho uses),
// and a failed ioctl here — get or set — is silently ignored rather than
// treated as a hard failure. There is no secret at risk the way there is
// in disableTerminalEcho's fail-closed echo requirement: the worst case of
// a no-op here is just the pre-existing typeahead race this exists to
// close, not a new leak, so it never blocks or fails the prompt it guards.
func flushPendingTerminalInput() {
	if promptReader != io.Reader(os.Stdin) || !isInteractive() {
		return
	}
	fd := int(os.Stdin.Fd())
	current, err := ioctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return
	}
	_ = ioctlSetTermios(fd, ioctlWriteTermios, current)
}
