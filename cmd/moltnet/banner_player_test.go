package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestParseBannerFramesUniformDimensions pins the embedded
// assets/banner-frames.txt (16 raw frames, 8 rows of 40 columns) and the
// final bannerFrames var playBanner uses: cropped/deduped down to 14
// frames of 6 rows (parseBannerFrames).
func TestParseBannerFramesUniformDimensions(t *testing.T) {
	raw, err := splitAndValidateBannerFrames(bannerFramesText)
	if err != nil {
		t.Fatalf("splitAndValidateBannerFrames error = %v, want nil", err)
	}
	if len(raw) != 16 {
		t.Fatalf("len(raw frames) = %d, want 16", len(raw))
	}
	for i, f := range raw {
		if len(f.rows) != 8 {
			t.Fatalf("raw frame %d has %d rows, want 8", i, len(f.rows))
		}
		for j, row := range f.rows {
			if got := len([]rune(row)); got != 40 {
				t.Fatalf("raw frame %d row %d has %d columns, want 40", i, j, got)
			}
		}
	}

	if bannerFramesErr != nil {
		t.Fatalf("bannerFramesErr = %v, want nil", bannerFramesErr)
	}
	if len(bannerFrames) != 14 {
		t.Fatalf("len(bannerFrames) = %d, want 14 (16 raw minus 2 deduped duplicate pairs)", len(bannerFrames))
	}
	for i, f := range bannerFrames {
		if len(f.rows) != 6 {
			t.Fatalf("bannerFrames[%d] has %d rows, want 6 (cropped to shared content)", i, len(f.rows))
		}
	}
	if bannerFrameContentWidth != 36 {
		t.Fatalf("bannerFrameContentWidth = %d, want 36", bannerFrameContentWidth)
	}
}

// TestParseBannerFramesRejectsMalformedInput proves parseBannerFrames
// errors rather than panicking or silently accepting ragged frames that
// would get playBanner's cursor-up arithmetic wrong.
func TestParseBannerFramesRejectsMalformedInput(t *testing.T) {
	for name, raw := range map[string]string{
		"mismatched row count":    "aa\nbb\n%%\naa\n",
		"mismatched column count": "aa\nbb\n%%\naaa\nbb\n",
		"single frame":            "aa\nbb\n",
	} {
		if _, err := parseBannerFrames(raw); err == nil {
			t.Fatalf("%s: parseBannerFrames() error = nil, want an error", name)
		}
	}
}

// TestCropBannerFramesToSharedContentUsesUnionWindow pins the P2 fix
// directly: the crop window is the UNION of every frame's content box,
// applied identically to every frame, not each frame's own tightest box.
func TestCropBannerFramesToSharedContentUsesUnionWindow(t *testing.T) {
	frames := []bannerFrame{
		{rows: []string{"      ", " AA   ", "      "}}, // content cols 1-2
		{rows: []string{"      ", "  BBB ", "      "}}, // content cols 2-4
	}
	got := cropBannerFramesToSharedContent(frames)
	// Union column window [1,4]; rows 0/2 are blank in both frames and drop
	// out entirely, leaving only row 1.
	want := []bannerFrame{{rows: []string{"AA"}}, {rows: []string{" BBB"}}}
	assertFramesEqual(t, got, want)
}

// TestDedupeConsecutiveBannerFrames: only *consecutive* duplicates collapse
// (P3); a later, non-adjacent repeat is a real transition and stays.
func TestDedupeConsecutiveBannerFrames(t *testing.T) {
	a := bannerFrame{rows: []string{"x"}}
	b := bannerFrame{rows: []string{"y"}}
	got := dedupeConsecutiveBannerFrames([]bannerFrame{a, a, b, b, a})
	assertFramesEqual(t, got, []bannerFrame{a, b, a})
}

// assertFramesEqual is TestCropBannerFramesToSharedContentUsesUnionWindow
// and TestDedupeConsecutiveBannerFrames' shared row-by-row comparison.
func assertFramesEqual(t *testing.T, got, want []bannerFrame) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !slices.Equal(got[i].rows, want[i].rows) {
			t.Fatalf("frame[%d].rows = %#v, want %#v", i, got[i].rows, want[i].rows)
		}
	}
}

// TestBannerFrameBlockAlwaysOneTrailingNewline pins block()'s contract:
// rows joined by "\n" plus exactly one trailing "\n", which playBanner's
// "\x1b[<rows>A" cursor-up math depends on for every frame.
func TestBannerFrameBlockAlwaysOneTrailingNewline(t *testing.T) {
	f := bannerFrame{rows: []string{"ab", "cd", "ef"}}
	if got, want := f.block(), "ab\ncd\nef\n"; got != want {
		t.Fatalf("block() = %q, want %q", got, want)
	}
}

// TestFinalBannerFrameMatchesStaticBanner is the drift guard between the
// parsed settled last frame and assets/banner.txt: strict equality, no
// normalization, since cropBannerFramesToSharedContent already right-trims
// each row. TestPlayBannerAnimatesOverRealPTY checks the same guarantee
// against what's actually written to a real terminal.
func TestFinalBannerFrameMatchesStaticBanner(t *testing.T) {
	if bannerFramesErr != nil {
		t.Fatalf("bannerFramesErr = %v, want nil", bannerFramesErr)
	}
	last := bannerFrames[len(bannerFrames)-1]
	got := strings.Join(last.rows, "\n")
	want := strings.TrimRight(bannerText, "\n")
	if got != want {
		t.Fatalf("final banner frame =\n%q\nwant (banner.txt):\n%q", got, want)
	}
}

// TestBannerFrameStepInterval pins both branches: no shortening well under
// the cap, and shortened enough that a larger frame set still fits it.
func TestBannerFrameStepInterval(t *testing.T) {
	if got := bannerFrameStepInterval(0); got != 0 {
		t.Fatalf("bannerFrameStepInterval(0) = %v, want 0", got)
	}
	if got := bannerFrameStepInterval(1); got != 0 {
		t.Fatalf("bannerFrameStepInterval(1) = %v, want 0", got)
	}
	if got := bannerFrameStepInterval(5); got != bannerFrameInterval {
		t.Fatalf("bannerFrameStepInterval(5) = %v, want %v (well under the cap)", got, bannerFrameInterval)
	}

	const manyFrames = 100 // 99 transitions * bannerFrameInterval blows past the cap
	got := bannerFrameStepInterval(manyFrames)
	want := bannerAnimationHardCap / time.Duration(manyFrames-1)
	if got != want {
		t.Fatalf("bannerFrameStepInterval(%d) = %v, want %v (shortened to fit the cap)", manyFrames, got, want)
	}
	if total := got * time.Duration(manyFrames-1); total > bannerAnimationHardCap {
		t.Fatalf("total = %v, exceeds hard cap %v", total, bannerAnimationHardCap)
	}
}

// TestTerminalWidthColumnsFallback exercises the COLUMNS fallback (P1):
// stdout is a plain pipe here, so TIOCGWINSZ always fails (ENOTTY) and
// COLUMNS is the only source left. Unparseable/unset must report ok=false.
func TestTerminalWidthColumnsFallback(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	previousStdout := stdout
	stdout = w
	t.Cleanup(func() { stdout = previousStdout })

	t.Setenv("COLUMNS", "42")
	if got, ok := terminalWidth(); !ok || got != 42 {
		t.Fatalf("terminalWidth() = (%d, %v), want (42, true)", got, ok)
	}
	for _, bad := range []string{"not-a-number", ""} {
		t.Setenv("COLUMNS", bad)
		if _, ok := terminalWidth(); ok {
			t.Fatalf("terminalWidth() ok = true with COLUMNS=%q, want false", bad)
		}
	}
}

// TestPrintBannerAnimatedHiddenWhenNotTerminal mirrors
// TestStyleBannerHiddenWhenNotTerminal (style_test.go): with
// isOutputTerminal false, printBannerAnimated is a no-op.
func TestPrintBannerAnimatedHiddenWhenNotTerminal(t *testing.T) {
	if output := captureStdout(t, func() { printBannerAnimated(context.Background()) }); output != "" {
		t.Fatalf("output = %q, want no banner output when stdout is not a terminal", output)
	}
}

// TestPrintBannerAnimatedMatchesStaticOutsideRealTTY forces isOutputTerminal
// true but leaves stdout as an ordinary pipe (not a real pty):
// stdoutIsRealTTY() stays false, so playBanner takes its static fallback,
// byte-identical to printBanner()'s own output. This is what guarantees the
// animation can never fire against anything but a genuine terminal.
func TestPrintBannerAnimatedMatchesStaticOutsideRealTTY(t *testing.T) {
	previousTerminal := isOutputTerminal
	isOutputTerminal = func() bool { return true }
	t.Cleanup(func() { isOutputTerminal = previousTerminal })

	withNoColorUnset(t)
	t.Setenv("TERM", "xterm-256color")

	animated := captureStdout(t, func() { printBannerAnimated(context.Background()) })
	static := captureStdout(t, func() { printBanner() })
	if animated != static {
		t.Fatalf("printBannerAnimated() = %q, want byte-identical to printBanner() = %q", animated, static)
	}
	if !strings.Contains(animated, "\x1b[") {
		t.Fatalf("output = %q, want at least one ANSI escape from dim styling", animated)
	}
	if bannerFramesErr == nil && strings.Contains(animated, cursorUpEscape()) {
		t.Fatalf("output = %q, want no cursor-up sequence outside a real TTY", animated)
	}
}

// stripANSI removes SGR escapes (style_test.go's ansiEscapePattern) from s.
// Callers below always slice off any cursor-up escape first (LastIndex),
// so the 'm'-suffixed SGR pattern is all that's left to strip.
func stripANSI(s string) string { return ansiEscapePattern.ReplaceAllString(s, "") }

// withNoColorUnset unsets NO_COLOR for the test's duration (restored after)
// so it never masks the TTY/width gates a test actually means to exercise.
func withNoColorUnset(t *testing.T) {
	t.Helper()
	previous, had := os.LookupEnv("NO_COLOR")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatalf("unset NO_COLOR: %v", err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv("NO_COLOR", previous)
		} else {
			os.Unsetenv("NO_COLOR")
		}
	})
}

// setPTYWidth sets the pty pair's reported width (TIOCSWINSZ) via the
// master side; a later TIOCGWINSZ read on the slave side sees it too.
func setPTYWidth(t *testing.T, master *os.File, cols int) {
	t.Helper()
	ws := &unix.Winsize{Row: 24, Col: uint16(cols)}
	if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		t.Fatalf("set pty width to %d cols: %v", cols, err)
	}
}

// preparePTYBannerTest allocates a fresh pty (requireTestPTY,
// openpty_test.go — never this test binary's own controlling terminal, per
// the HARD RULE), sets its width, swaps stdout to the slave side, forces
// the styling gates playBanner needs, and drains the master in background.
func preparePTYBannerTest(t *testing.T, cols int) *syncBuffer {
	t.Helper()

	master, slave := requireTestPTY(t)
	setPTYWidth(t, master, cols)

	previousStdout := stdout
	stdout = slave
	t.Cleanup(func() { stdout = previousStdout })

	withNoColorUnset(t)
	t.Setenv("TERM", "xterm-256color")

	if !stdoutIsRealTTY() {
		t.Fatal("stdoutIsRealTTY() = false against a real pty slave, want true")
	}
	return drainMasterInBackground(master)
}

// drainedOutput waits for the background drain goroutine to catch up, then
// normalizes the pty's "\n" -> "\r\n" (ONLCR) expansion back to a bare "\n".
func drainedOutput(buf *syncBuffer) string {
	time.Sleep(200 * time.Millisecond)
	return strings.ReplaceAll(buf.String(), "\r\n", "\n")
}

// cursorUpEscape is the "\x1b[<rows>A" sequence playBanner emits per frame.
func cursorUpEscape() string { return fmt.Sprintf("\x1b[%dA", len(bannerFrames[0].rows)) }

// staticBannerOutput captures printBanner's own output (ANSI stripped),
// forcing isOutputTerminal true (captureStdout's pipe is never a real
// terminal) so it actually writes something to compare against.
func staticBannerOutput(t *testing.T) string {
	t.Helper()
	previous := isOutputTerminal
	isOutputTerminal = func() bool { return true }
	t.Cleanup(func() { isOutputTerminal = previous })
	return stripANSI(captureStdout(t, func() { printBanner() }))
}

// assertSettledOutputMatchesStaticBanner extracts the tail of a pty output
// stream after its LAST cursor-up escape — what a viewer sees once the
// writes stop moving — and checks it is byte-identical, modulo ANSI, to
// printBanner's own static output (the P2 position-offset regression guard).
func assertSettledOutputMatchesStaticBanner(t *testing.T, output, cursorUp string) {
	t.Helper()
	idx := strings.LastIndex(output, cursorUp)
	if idx == -1 {
		t.Fatalf("output has no cursor-up sequence at all: %q", output)
	}
	settled := stripANSI(output[idx+len(cursorUp):])
	static := staticBannerOutput(t)
	if settled != static {
		t.Fatalf("settled animation output (ANSI stripped) = %q, want %q (static printBanner(), ANSI stripped)", settled, static)
	}
}

// TestPlayBannerAnimatesOverRealPTY: 80 columns, comfortably above
// bannerFrameContentWidth+1, so playBanner animates for real. Asserts one
// cursor-up per transition, no cursor-save/alternate-screen escapes, and
// that the settled screen matches printBanner's static output (P2 guard).
func TestPlayBannerAnimatesOverRealPTY(t *testing.T) {
	if bannerFramesErr != nil {
		t.Fatalf("bannerFramesErr = %v, want nil", bannerFramesErr)
	}

	buf := preparePTYBannerTest(t, 80)
	printBannerAnimated(context.Background())
	output := drainedOutput(buf)

	cursorUp := cursorUpEscape()
	wantTransitions := len(bannerFrames) - 1
	if got := strings.Count(output, cursorUp); got != wantTransitions {
		t.Fatalf("cursor-up count = %d, want %d; output = %q", got, wantTransitions, output)
	}
	for _, forbidden := range []string{"\x1b[s", "\x1b[u", "\x1b[?1049h", "\x1b[?25l"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output contains forbidden escape %q", forbidden)
		}
	}
	assertSettledOutputMatchesStaticBanner(t, output, cursorUp)
}

// TestPlayBannerStaticFallbackWhenTerminalNarrow is the P1 fix's own pty
// test: 30 columns, below bannerFrameContentWidth+1 (37), so playBanner
// must take the static fallback with no cursor-up redraw at all.
func TestPlayBannerStaticFallbackWhenTerminalNarrow(t *testing.T) {
	if bannerFramesErr != nil {
		t.Fatalf("bannerFramesErr = %v, want nil", bannerFramesErr)
	}
	if bannerFrameContentWidth+1 <= 30 {
		t.Fatalf("test assumes 30 cols is narrower than bannerFrameContentWidth+1 = %d", bannerFrameContentWidth+1)
	}

	buf := preparePTYBannerTest(t, 30)
	printBannerAnimated(context.Background())
	output := drainedOutput(buf)
	cursorUp := cursorUpEscape()
	if strings.Contains(output, cursorUp) {
		t.Fatalf("output contains a cursor-up sequence below the content width; want the static fallback: %q", output)
	}
	if !strings.Contains(output, "\x1b[2m") {
		t.Fatalf("output = %q, want the dim styling ANSI code", output)
	}
	if got, static := stripANSI(output), staticBannerOutput(t); got != static {
		t.Fatalf("narrow-terminal output (ANSI stripped) = %q, want %q", got, static)
	}
}

// TestPlayBannerDeadlineJumpsToFinalFrame is the P2 fake-cap fix's own
// test: nowFunc is faked so the first elapsed-time check already reports
// the hard cap exceeded, so playBanner must jump straight from frame 0 to
// the final frame — one cursor-up, not one per remaining transition —
// settling on the same content the full animation does. No real sleep is
// ever reached, so this stays fast and deterministic.
func TestPlayBannerDeadlineJumpsToFinalFrame(t *testing.T) {
	if bannerFramesErr == nil && len(bannerFrames) < 3 {
		t.Fatalf("test wants at least 3 frames; got %d", len(bannerFrames))
	}

	previousNow := nowFunc
	base := time.Unix(0, 0)
	calls := 0
	nowFunc = func() time.Time {
		calls++
		if calls == 1 {
			return base // establishes playBanner's start time
		}
		return base.Add(bannerAnimationHardCap) // every check after: cap exceeded
	}
	t.Cleanup(func() { nowFunc = previousNow })

	buf := preparePTYBannerTest(t, 80)

	testStart := time.Now()
	printBannerAnimated(context.Background())
	if elapsed := time.Since(testStart); elapsed > 500*time.Millisecond {
		t.Fatalf("printBannerAnimated() took %v; want a prompt jump to the final frame, not sleeping through every transition", elapsed)
	}
	output := drainedOutput(buf)
	cursorUp := cursorUpEscape()
	if got := strings.Count(output, cursorUp); got != 1 {
		t.Fatalf("cursor-up count = %d, want exactly 1 (frame 0, then one jump to the final frame); output = %q", got, output)
	}
	assertSettledOutputMatchesStaticBanner(t, output, cursorUp)
}
