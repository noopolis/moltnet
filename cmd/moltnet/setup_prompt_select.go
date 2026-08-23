package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// selectOption is one arrow-key-selectable choice promptSelect offers:
// label is the value itself (also what the plain-fallback path matches a
// typed answer against, case-insensitively), hint is optional dimmed
// explanatory text printed alongside it (SETUP.md's onboarding mockup:
// "on this machine   always available, any agent that can reach it").
type selectOption struct {
	label string
	hint  string
}

// errSetupCancelled is promptSelect's Ctrl-C sentinel for the raw-key
// interactive path. In practice a real terminal's ISIG bit (left untouched
// by enableRawSelectMode — only ECHO and ICANON are cleared) means Ctrl-C
// raises SIGINT at the OS level and never reaches this program as the byte
// 0x03 at all; runCLI's signal.NotifyContext (cli.go) turns that into a
// cancelled ctx instead, which promptSelectInteractive's ctx.Done() case
// already handles the same way every other interactive prompt in this
// package does. This sentinel exists for the two cases where the byte
// really is seen directly: a terminal with ISIG disabled, and — the one
// this package's own tests exercise — a scripted promptReader that sends
// 0x03 deliberately.
var errSetupCancelled = errors.New("setup: cancelled")

// promptSelect asks question and returns the index of the chosen option in
// options, plus paintedRows — the number of terminal rows the question line
// and the option block actually occupy in their final, settled state (P1-1),
// or selectRowsUnmeasured (-1) when promptSelectPlain ran instead: that
// path's output is pure top-to-bottom prints with no in-place redraw, so
// Write's own running bodyLines count (setup_header.go) is already exactly
// correct and setupPromptSelect must not overwrite it with a foreign value.
// defaultIndex is returned unchanged when the answer is empty (an
// out-of-range defaultIndex is clamped to 0) — the "Enter-through-everything
// must be a valid complete path" rule every setup question has to satisfy.
//
// Two implementations back this, chosen by the same real-terminal gate
// disableTerminalEcho already uses (prompt_hidden_echo_unix.go):
// promptReader must still be the process's own os.Stdin, isInteractive()
// (stdin) and isOutputTerminal() (stdout) must both hold. When they do, and
// the controlling terminal's raw mode can actually be engaged
// (enableRawSelectMode), the real arrow-key/Enter UI runs
// (promptSelectInteractive). Everything else — a script, a test's scripted
// promptReader, a piped session, or a terminal enableRawSelectMode could not
// touch — falls back to promptSelectPlain: the numbered list is printed
// once, and one line is read and parsed as an empty answer (default), a
// 1-based number, or a case-insensitive label match. Both paths accept
// exactly the same set of valid answers in spirit; only the input mechanism
// differs.
func promptSelect(ctx context.Context, question string, options []selectOption, defaultIndex int) (int, int, error) {
	if len(options) == 0 {
		return 0, selectRowsUnmeasured, fmt.Errorf("promptSelect %q: no options given", question)
	}
	if defaultIndex < 0 || defaultIndex >= len(options) {
		defaultIndex = 0
	}

	if promptReader == io.Reader(os.Stdin) && isInteractive() && isOutputTerminal() {
		if restore, ok := enableRawSelectMode(); ok {
			defer restore()
			return promptSelectInteractive(ctx, question, options, defaultIndex)
		}
	}
	index, err := promptSelectPlain(question, options, defaultIndex)
	return index, selectRowsUnmeasured, err
}

// selectRowsUnmeasured is promptSelect's sentinel paintedRows value for the
// promptSelectPlain path (P1-1): "not measured, and not needed — trust
// whatever Write already counted."
const selectRowsUnmeasured = -1

// promptSelectPlain is promptSelect's non-interactive/fallback path: print
// the question and the numbered option list once, then read one line via
// promptLine and parse it.
func promptSelectPlain(question string, options []selectOption, defaultIndex int) (int, error) {
	fmt.Fprintf(stdout, "  %s\n", question)
	for i, opt := range options {
		marker := " "
		if i == defaultIndex {
			marker = "*"
		}
		line := fmt.Sprintf("%d) %s", i+1, opt.label)
		if opt.hint != "" {
			line += "  " + dim(opt.hint)
		}
		fmt.Fprintf(stdout, "  %s %s\n", marker, line)
	}

	answer, err := promptLine(fmt.Sprintf("  choice [%d]: ", defaultIndex+1))
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	index, parseErr := parseSelectAnswer(answer, options, defaultIndex)
	if parseErr != nil {
		return 0, parseErr
	}
	printInitConfigCheckLine(options[index].label, "")
	return index, nil
}

// parseSelectAnswer interprets one line of typed input against options:
// empty (including an io.EOF-terminated empty read, e.g. non-interactive
// stdin closed outright) selects defaultIndex, a 1-based number selects
// that option, and a case-insensitive prefix match against a label selects
// the first option it matches. Anything else is a clear, actionable error
// naming the valid choices, rather than silently falling back to the
// default — a typo should never be mistaken for "Enter pressed".
func parseSelectAnswer(answer string, options []selectOption, defaultIndex int) (int, error) {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return defaultIndex, nil
	}
	if n, convErr := strconv.Atoi(trimmed); convErr == nil {
		if n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		return 0, fmt.Errorf("choice %q is out of range (1-%d)", trimmed, len(options))
	}
	lower := strings.ToLower(trimmed)
	for i, opt := range options {
		if strings.HasPrefix(strings.ToLower(opt.label), lower) {
			return i, nil
		}
	}
	labels := make([]string, len(options))
	for i, opt := range options {
		labels[i] = opt.label
	}
	return 0, fmt.Errorf("choice %q not recognized; expected one of: %s", trimmed, strings.Join(labels, ", "))
}

// selectKey, its constants, and readSelectKey live in setup_select_key.go --
// split out to keep this file under the repo's 400-line limit.

// promptSelectInteractive is promptSelect's real-terminal path: it assumes
// the caller has already engaged raw mode (enableRawSelectMode) and only
// restores it on return via the caller's own deferred restore — this
// function itself never toggles terminal state. It prints the question and
// option list once, then loops reading one decoded key at a time
// (readSelectKey), redrawing the option block on every Up/Down and
// returning the current selection on Enter.
//
// Each read runs in its own goroutine, raced against ctx.Done() exactly
// like resolveInitNetworkID's interactive --id prompt (init.go): bufio's
// ReadByte has no ctx-aware variant, so a cancelled ctx (SIGINT/SIGTERM,
// caught by runCLI's signal.NotifyContext) returns immediately rather than
// waiting on a keypress that may never come, leaving that one goroutine to
// exit on its own once the process does — bounded the same way, for the
// same reason.
//
// P1-1: every return path reports paintedRows — the question line's row(s)
// plus the option block's rows in their final, currently-visible state,
// measured against the terminal's live width via selectBlockRows/
// advanceRows rather than assumed as one row per option. This widget is the
// one place that actually knows what it painted (renderSelectOptions'
// redraw reprints the whole block in place, so naive forward Write
// accounting inflates by len(options) per keypress — see
// setup_header_render.go's setupPromptSelect); reporting the true figure
// here, once, at the point of return, is simpler and more robust than
// teaching Write to undo that inflation after the fact.
func promptSelectInteractive(ctx context.Context, question string, options []selectOption, defaultIndex int) (int, int, error) {
	fmt.Fprintf(stdout, "  %s\n", question)

	// width is captured exactly once, before the initial paint, and reused
	// for every measurement and redraw this call makes (P2-1/round 3): the
	// initial block is painted at whatever width is live right now, so
	// re-querying terminalWidth() later -- as this used to do inside rows(),
	// and as a naive per-redraw fix might do inside the arrow-key loop below
	// -- would measure a resized-since-paint width against rows that were
	// actually drawn at the old one, the same paint/measure mismatch this
	// round exists to close, just moved from "wrong formula" to "wrong
	// width". 0 on failure; selectOptionsRows/selectBlockRows/advanceRows
	// degrade to newline-only counting, same as Write's own fallback.
	width, _ := terminalWidth()
	current := defaultIndex
	// optionRows is the option block's row count as it was last actually
	// painted -- renderSelectOptions' own cursor-up count on the *next*
	// redraw, so a redraw always erases exactly what is really on screen
	// (round 3's P1 fix) rather than either len(options) (the bug) or this
	// call's freshly recomputed row count (which, for a fixed option list
	// and this now-frozen width, is provably identical to optionRows today,
	// since selectOptionText's marker is the same visible width whether or
	// not it is current -- but the two numbers are only guaranteed equal by
	// that coincidence, not by construction, so this is threaded through
	// explicitly rather than assumed).
	optionRows := selectOptionsRows(width, options, current)
	renderSelectOptions(width, options, current, optionRows, false)

	rows := func() int {
		return selectBlockRows(width, question, options, current)
	}

	reader := bufio.NewReader(promptReader)
	type keyResult struct {
		key selectKey
		err error
	}

	for {
		resultCh := make(chan keyResult, 1)
		go func() {
			key, err := readSelectKey(reader)
			resultCh <- keyResult{key: key, err: err}
		}()

		select {
		case <-ctx.Done():
			return 0, rows(), ctx.Err()
		case res := <-resultCh:
			if res.err != nil {
				if errors.Is(res.err, io.EOF) {
					// Input closed before Enter: treat exactly like a
					// non-interactive empty answer rather than an error --
					// the same "Enter-through" default applies.
					fmt.Fprintln(stdout)
					return current, rows() + 1, nil
				}
				return 0, rows(), fmt.Errorf("read selection: %w", res.err)
			}
			switch res.key {
			case selectKeyUp:
				current = (current - 1 + len(options)) % len(options)
				renderSelectOptions(width, options, current, optionRows, true)
				optionRows = selectOptionsRows(width, options, current)
			case selectKeyDown:
				current = (current + 1) % len(options)
				renderSelectOptions(width, options, current, optionRows, true)
				optionRows = selectOptionsRows(width, options, current)
			case selectKeyConfirm:
				fmt.Fprintln(stdout)
				return current, rows() + 1, nil
			case selectKeyCancel:
				return 0, rows(), errSetupCancelled
			case selectKeyNone:
				// Unrecognized byte/sequence: ignored, loop for the next key.
			}
		}
	}
}

// selectOptionText returns the marker+label(+hint) text renderSelectOptions
// writes for one option (sans the leading "  " indent and trailing
// newline), given whether it is the currently selected one. It is the
// single formatting authority both the paint path (renderSelectOptions) and
// the row-measurement path (selectBlockRows, P1-1) share, so the two can
// never drift out of sync with each other the way a second, hand-copied
// format string could.
func selectOptionText(opt selectOption, isCurrent bool) string {
	marker := "  "
	if isCurrent {
		marker = green("> ")
	}
	line := opt.label
	if opt.hint != "" {
		line += "   " + dim(opt.hint)
	}
	return marker + line
}

// renderSelectOptions prints options with current marked "> ", the rest
// indented to match. redraw moves the cursor back up over the previously
// printed block first — but only when stdoutIsRealTTY() (the concrete,
// non-mockable terminal check banner_player.go's playBanner also insists
// on, deliberately independent of the swappable isOutputTerminal var):
// promptSelect's own gate already requires isOutputTerminal() to reach this
// function at all, but a redraw additionally emitting raw cursor-motion
// escapes into a stream that turns out not to be a genuine ANSI terminal
// (a test that forced isOutputTerminal() true with no real tty behind it)
// would corrupt captured output instead of just under-drawing it. Without a
// genuine terminal, this simply reprints the block fresh every time --
// functionally complete (the caller still sees each transition), just not
// an in-place redraw.
//
// Round 3 / P1: the redraw used to always emit "\x1b[<n>A" for exactly
// len(options) rows, with no terminal-width check at all -- an option line
// that wraps onto a second terminal row under a narrow width under-shot the
// cursor-up count by one row per wrapped line, and under-shot it again on
// every subsequent redraw (the stale rows never got erased, so they pushed
// everything printed after them one row further down each time -- "drift is
// exactly 1 row per arrow press"). prevRows fixes this by making the caller
// (promptSelectInteractive) respect the same rule setup_header.go's own
// redraw already follows: undo exactly what was actually painted, not what
// the thing about to be painted will occupy. It must be the row count
// selectOptionsRows measured for the *previous* call's current index (or,
// on the very first paint, this same call's own index, since nothing has
// been painted yet to disagree with) -- see promptSelectInteractive's own
// optionRows variable, which is what actually thread this through.
func renderSelectOptions(width int, options []selectOption, current, prevRows int, redraw bool) {
	if redraw && stdoutIsRealTTY() {
		fmt.Fprintf(stdout, "\x1b[%dA", prevRows)
	}
	for i, opt := range options {
		fmt.Fprintf(stdout, "  %s\n", selectOptionText(opt, i == current))
	}
}

// selectOptionsRows measures how many terminal rows the option block alone
// (no question line) occupies at width columns for options with current
// selected, wrap included. Split out from selectBlockRows (round 3 / P1) so
// renderSelectOptions' own cursor-up count can share exactly this number --
// the option block is the only region that redraw ever erases and reprints;
// the question line above it is never touched again once printed. width <=
// 0 degrades to advanceRows' own newline-only fallback rather than guessing
// a wrap point that may not exist.
//
// Both this function and its sibling selectBlockRows (below) always measure
// starting from column 0, rather than whatever column a preceding write
// actually left the cursor at -- selectOptionsRows because every option
// line renderSelectOptions writes ends in '\n' (so the cursor is always
// back at column 0 before the next one starts), and selectBlockRows'
// question-line half for the same reason plus one more: it ignores h.col
// (the header's own running Write state, setup_header.go) entirely rather
// than seeding advanceRows with it. Both are unreachable today for the same
// upstream invariant: promptSelectInteractive always writes the question
// with a trailing '\n' before either function ever runs, and h.lastNL is
// always true when a select prompt starts, so col is always genuinely 0 by
// the time either measurement matters. Consolidated here as one note rather
// than two, since a fix to one (seed from the real starting column) would
// naturally fix the other the same way.
func selectOptionsRows(width int, options []selectOption, current int) int {
	var b strings.Builder
	for i, opt := range options {
		fmt.Fprintf(&b, "  %s\n", selectOptionText(opt, i == current))
	}
	_, rows, _ := advanceRows(0, width, false, b.String())
	return rows
}

// selectBlockRows measures how many terminal rows promptSelectInteractive's
// question line plus its option block actually occupy at width columns in
// their current (current-index) settled state — the real geometry, wrap
// included, rather than the "1 + len(options)" formula P1-1 replaces. It is
// now the question line's own row count (measured the same way, via
// advanceRows) plus selectOptionsRows' option-block figure — split apart in
// round 3 so renderSelectOptions' redraw can share the option-only half of
// this same measurement instead of assuming one terminal row per option.
// Equivalent to the old single-builder version bit-for-bit: the question
// line always ends in '\n' (resetting col to 0), so measuring it separately
// from the option block that follows changes nothing about the total.
//
// See selectOptionsRows' doc comment above for the column-0 assumption both
// functions share, and why it is unreachable today.
func selectBlockRows(width int, question string, options []selectOption, current int) int {
	_, questionRows, _ := advanceRows(0, width, false, fmt.Sprintf("  %s\n", question))
	return questionRows + selectOptionsRows(width, options, current)
}
