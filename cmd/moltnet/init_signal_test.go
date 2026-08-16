package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestRunInitAbortsOnSIGINTDuringAnimation is item 3's own regression guard:
// runCLI installs signal.NotifyContext(SIGINT, SIGTERM) (cli.go), but before
// this change runInit never consulted it, so a Ctrl-C while the settle
// animation (or the --id prompt) was in progress was inert — init ran to
// completion regardless. This drives runInit with the real production
// signal factory (defaultSignalContext, signals.go) against a real pty (a
// plain captureStdout pipe never lets the animation actually run, so it
// could never be interrupted mid-flight either), sends the process itself a
// real SIGINT partway through the ~0.9s animation, and checks that runInit
// returns promptly on ctx.Err() and creates nothing on disk.
func TestRunInitAbortsOnSIGINTDuringAnimation(t *testing.T) {
	if bannerFramesErr != nil {
		t.Fatalf("bannerFramesErr = %v, want nil", bannerFramesErr)
	}

	buf := preparePTYBannerTest(t, 80)
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := filepath.Join(home, "sigint-target")

	ctx, stop := defaultSignalContext()
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runInit(ctx, []string{"--dir", target})
	}()

	// bannerAnimationHardCap is 1200ms; wait long enough for the animation
	// to have genuinely started (frame 0 already drawn, more still queued)
	// but nowhere near done, so a false pass ("it just finished on its own
	// right as we signaled") is not plausible.
	time.Sleep(150 * time.Millisecond)

	sigintAt := time.Now()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("signal self with SIGINT: %v", err)
	}

	select {
	case err := <-errCh:
		elapsed := time.Since(sigintAt)
		if err == nil {
			t.Fatal("runInit() error = nil, want an error after SIGINT")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runInit() error = %v, want context.Canceled", err)
		}
		// bannerFrameInterval (banner_player.go) is 60ms: playBanner's
		// between-frames select on ctx.Done() must observe SIGINT within
		// roughly one frame interval, not run out whatever sleep or
		// remaining animation was already in flight. 300ms is generous
		// headroom over that while still catching a regression where the
		// select isn't actually wired to ctx — such a version still
		// eventually returns (via the hard cap or the animation simply
		// finishing), just far slower than a genuinely pinned abort.
		if elapsed >= 300*time.Millisecond {
			t.Fatalf("runInit() took %v to return after SIGINT, want < 300ms (the between-frames ctx select must observe cancellation promptly)", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runInit() did not return within 3s of SIGINT")
	}

	if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target directory stat error = %v, want os.ErrNotExist (nothing should be created on a cancelled init)", statErr)
	}

	// Drain what did make it to the pty so the background reader
	// (drainMasterInBackground) doesn't outlive the test.
	_ = drainedOutput(buf)
}

// TestResolveInitNetworkIDAbortsOnSIGINTDuringPrompt covers item 3's other
// abort point: the interactive --id prompt (resolveInitNetworkID) blocks on
// a synchronous bufio.Reader.ReadString(os.Stdin) with no ctx-aware variant,
// so it must race that read against ctx.Done() in a separate goroutine
// (see resolveInitNetworkID's doc comment) rather than check ctx up front
// and then block anyway. os.Stdin is swapped for the read end of a pipe
// that never receives input, so the read goroutine is genuinely blocked —
// exactly the state a real interactive prompt is in while waiting on a
// human — when SIGINT arrives.
func TestResolveInitNetworkIDAbortsOnSIGINTDuringPrompt(t *testing.T) {
	previousInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = previousInteractive })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	previousStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = previousStdin })

	_ = captureStdout(t, func() {
		ctx, stop := defaultSignalContext()
		defer stop()

		resultCh := make(chan error, 1)
		go func() {
			_, idErr := resolveInitNetworkID(ctx, "")
			resultCh <- idErr
		}()

		// No input is ever written to w, so the read goroutine is reliably
		// still blocked on ReadString by the time SIGINT lands.
		time.Sleep(100 * time.Millisecond)

		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			t.Fatalf("signal self with SIGINT: %v", err)
		}

		select {
		case idErr := <-resultCh:
			if !errors.Is(idErr, context.Canceled) {
				t.Fatalf("resolveInitNetworkID() error = %v, want context.Canceled", idErr)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("resolveInitNetworkID() did not return within 3s of SIGINT")
		}
	})
}
