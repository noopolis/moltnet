package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReadSelectKeyDecodesArrowsAndEnter(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\x1b[B\x1b[A\r\x03\x1b[Zx"))

	want := []selectKey{selectKeyDown, selectKeyUp, selectKeyConfirm, selectKeyCancel, selectKeyNone, selectKeyNone}
	for i, expected := range want {
		key, err := readSelectKey(reader)
		if err != nil {
			t.Fatalf("readSelectKey()[%d] error = %v", i, err)
		}
		if key != expected {
			t.Fatalf("readSelectKey()[%d] = %v, want %v", i, key, expected)
		}
	}
}

func TestReadSelectKeyEOF(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(""))
	if _, err := readSelectKey(reader); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestParseSelectAnswerEmptyUsesDefault(t *testing.T) {
	options := []selectOption{{label: "alpha"}, {label: "beta"}}
	index, err := parseSelectAnswer("", options, 1)
	if err != nil || index != 1 {
		t.Fatalf("parseSelectAnswer(\"\") = (%d, %v), want (1, nil)", index, err)
	}
}

func TestParseSelectAnswerByNumber(t *testing.T) {
	options := []selectOption{{label: "alpha"}, {label: "beta"}}
	index, err := parseSelectAnswer("2", options, 0)
	if err != nil || index != 1 {
		t.Fatalf("parseSelectAnswer(\"2\") = (%d, %v), want (1, nil)", index, err)
	}
}

func TestParseSelectAnswerByLabel(t *testing.T) {
	options := []selectOption{{label: "on this machine"}, {label: "in this folder"}}
	index, err := parseSelectAnswer("in this folder", options, 0)
	if err != nil || index != 1 {
		t.Fatalf("parseSelectAnswer(label) = (%d, %v), want (1, nil)", index, err)
	}
	index, err = parseSelectAnswer("IN THIS", options, 0)
	if err != nil || index != 1 {
		t.Fatalf("parseSelectAnswer(case-insensitive prefix) = (%d, %v), want (1, nil)", index, err)
	}
}

func TestParseSelectAnswerOutOfRangeNumber(t *testing.T) {
	options := []selectOption{{label: "alpha"}}
	if _, err := parseSelectAnswer("5", options, 0); err == nil {
		t.Fatal("expected an out-of-range error")
	}
}

func TestParseSelectAnswerUnrecognized(t *testing.T) {
	options := []selectOption{{label: "alpha"}, {label: "beta"}}
	if _, err := parseSelectAnswer("gamma", options, 0); err == nil {
		t.Fatal("expected an unrecognized-choice error")
	}
}

// TestPromptSelectPlainEnterThrough exercises promptSelect's non-interactive
// fallback (withPromptAnswers's scripted promptReader, uninstall_test.go)
// end-to-end: an empty line answers with the default, matching the
// "Enter-through-everything is a valid complete path" rule.
func TestPromptSelectPlainEnterThrough(t *testing.T) {
	withPromptAnswers(t, "")
	// withPromptAnswers forces isInteractive() true but leaves promptReader
	// as a scripted strings.Reader, never os.Stdin -- exactly the gate that
	// routes promptSelect to promptSelectPlain instead of the real-terminal
	// path (see promptSelect's own doc comment).
	options := []selectOption{{label: "on this machine"}, {label: "in this folder"}}

	index, _, err := promptSelect(context.Background(), "Where should this network live?", options, 0)
	if err != nil {
		t.Fatalf("promptSelect() error = %v", err)
	}
	if index != 0 {
		t.Fatalf("promptSelect() = %d, want the default 0", index)
	}
}

func TestPromptSelectPlainTypedChoice(t *testing.T) {
	withPromptAnswers(t, "2")
	options := []selectOption{{label: "on this machine"}, {label: "in this folder"}}

	index, _, err := promptSelect(context.Background(), "Where should this network live?", options, 0)
	if err != nil {
		t.Fatalf("promptSelect() error = %v", err)
	}
	if index != 1 {
		t.Fatalf("promptSelect() = %d, want 1", index)
	}
}

func TestPromptSelectPlainInvalidChoiceErrors(t *testing.T) {
	withPromptAnswers(t, "nonsense")
	options := []selectOption{{label: "alpha"}, {label: "beta"}}

	if _, _, err := promptSelect(context.Background(), "pick one", options, 0); err == nil {
		t.Fatal("expected an error for an unrecognized choice")
	}
}

// TestPromptSelectPlainReportsRowsUnmeasured pins promptSelect's contract
// for its non-interactive fallback path (P1-1): paintedRows must be
// selectRowsUnmeasured (-1), never a guessed row count, since
// promptSelectPlain's own top-to-bottom prints already flow through Write's
// ordinary accounting with nothing to reconcile.
func TestPromptSelectPlainReportsRowsUnmeasured(t *testing.T) {
	withPromptAnswers(t, "")
	options := []selectOption{{label: "alpha"}, {label: "beta"}}

	_, rows, err := promptSelect(context.Background(), "pick one", options, 0)
	if err != nil {
		t.Fatalf("promptSelect() error = %v", err)
	}
	if rows != selectRowsUnmeasured {
		t.Fatalf("promptSelect() paintedRows = %d, want selectRowsUnmeasured (%d)", rows, selectRowsUnmeasured)
	}
}

// TestSelectBlockRowsCountsWrappedOptionLines is P1-1's own test: this is
// the wizard's actual Q1 option set (setup_questions.go's askSetupScope)
// reproduced literally here (not imported — this file must stay correct
// even if that question's wording changes later), at the reviewer's own
// real-pty width (50 columns) where the first option's label+hint visibly
// wraps onto a second terminal row. The "1 + numOptions" formula P1-1
// replaces would report 1 (question) + 2 (options) = 3 rows regardless;
// the true, wrap-aware figure must be strictly greater.
func TestSelectBlockRowsCountsWrappedOptionLines(t *testing.T) {
	options := []selectOption{
		{label: "on this machine", hint: "always available, any agent that can reach it"},
		{label: "in this folder", hint: "lives with this project"},
	}
	const width = 50
	const oldFormula = 1 + 2 // question + one row per option, the pre-fix assumption.

	got := selectBlockRows(width, "Where should this network live?", options, 0)
	if got <= oldFormula {
		t.Fatalf("selectBlockRows(%d, ...) = %d, want > %d (the old one-row-per-option formula) -- the first option's hint should wrap at %d columns", width, got, oldFormula, width)
	}

	// Cross-check against advanceRows directly, so this isn't just
	// selectBlockRows asserting its own formula: feed the exact same
	// selectOptionText-built lines through it independently. Every line
	// here ends in '\n', so col is always back to 0 by the next line.
	var want int
	_, r, _ := advanceRows(0, width, false, "  Where should this network live?\n")
	want += r
	for i, opt := range options {
		_, r, _ := advanceRows(0, width, false, "  "+selectOptionText(opt, i == 0)+"\n")
		want += r
	}
	if got != want {
		t.Fatalf("selectBlockRows(%d, ...) = %d, want %d (advanceRows over the same lines directly)", width, got, want)
	}
}

// TestSelectBlockRowsUnknownWidthDegradesToNewlineCounting mirrors
// TestAdvanceRowsUnknownWidthDisablesWrapping (setup_header_test.go) at
// selectBlockRows' own level: width<=0 must fall back to one row per
// printed line, matching advanceRows' own degrade-to-newline-only
// contract, rather than fabricating a wrap point that may not exist.
func TestSelectBlockRowsUnknownWidthDegradesToNewlineCounting(t *testing.T) {
	options := []selectOption{{label: "alpha"}, {label: "beta"}}
	got := selectBlockRows(0, "pick one", options, 0)
	want := 1 + len(options) // one '\n'-terminated line each, no wrapping possible.
	if got != want {
		t.Fatalf("selectBlockRows(0, ...) = %d, want %d", got, want)
	}
}

// TestSelectOptionsRowsOption1BoundaryAt44DoesNotDoubleCount is the direct,
// exact-value pin for the width the immediate-wrap model got wrong in the
// opposite direction from 67: at width 44, option 1's own line (exactly 44
// visible columns) must contribute exactly one row, not two, even though
// option 0's longer (67-column) line genuinely does wrap at this width. The
// coarse wrapping/notWrapping split above only checks "some wrap happened
// somewhere in the block"; this pins the total to the one true value so a
// regression that overcounts just option 1's line (e.g. reverting to
// col==width meaning "wrapped already") cannot hide behind option 0's own,
// separate wrap.
func TestSelectOptionsRowsOption1BoundaryAt44DoesNotDoubleCount(t *testing.T) {
	options := []selectOption{
		{label: "on this machine", hint: "always available, any agent that can reach it"},
		{label: "in this folder", hint: "lives with this project"},
	}
	const width = 44

	// Ground truth, independent of selectOptionsRows: option 0's 67-column
	// line occupies ceil(67/44) = 2 rows; option 1's exactly-44-column line
	// occupies ceil(44/44) = 1 row. Total 3, never 4.
	const want = 3
	if got := selectOptionsRows(width, options, 0); got != want {
		t.Fatalf("selectOptionsRows(%d, ...) = %d, want %d (option 0 wraps to 2 rows, option 1's exact-width line stays 1 -- not 2)", width, got, want)
	}
}

// trueLineRows is deferred-autowrap ground truth for one '\n'-terminated
// line of visibleLen visible columns at a width-column-wide terminal,
// computed independently of advanceRows/selectOptionsRows: an empty line is
// always exactly one row (just the newline), and any non-empty line occupies
// ceil(visibleLen/width) rows -- reaching the last column parks the cursor
// there rather than spilling onto an extra row, so a line landing on an
// exact multiple of width is not a special case at all under this formula,
// it just falls out of the ceiling division.
func trueLineRows(visibleLen, width int) int {
	if visibleLen == 0 {
		return 1
	}
	return 1 + (visibleLen-1)/width
}

// TestSelectOptionsRowsSweep41To90MatchesTrueRowCount is the impasse
// report's own required verification: a full sweep of every width from 41
// to 90 (the range the reviewer's real-pty sweep covered, finding exactly
// two failures at 44 and 67) asserting selectOptionsRows -- the number
// renderSelectOptions' redraw uses verbatim as its cursor-up count -- equals
// the true painted row count at every single width, not just the two
// boundary widths the earlier tests pin individually. option 0's line is 67
// visible columns, option 1's is 44 (both reproduced from
// setup_questions.go's askSetupScope, same as every other test in this
// file); trueLineRows is the independent ground truth these are checked
// against.
func TestSelectOptionsRowsSweep41To90MatchesTrueRowCount(t *testing.T) {
	options := []selectOption{
		{label: "on this machine", hint: "always available, any agent that can reach it"},
		{label: "in this folder", hint: "lives with this project"},
	}
	// Derived from selectOptionText itself (ANSI stripped) -- the same
	// formatting authority both the paint path and selectOptionsRows share
	// (see selectOptionText's own doc comment) -- rather than hardcoded, so
	// editing Q1's hint copy can't leave this sweep passing against stale
	// ground truth.
	option0Width := utf8.RuneCountInString(stripANSI("  " + selectOptionText(options[0], true)))
	option1Width := utf8.RuneCountInString(stripANSI("  " + selectOptionText(options[1], false)))

	for width := 41; width <= 90; width++ {
		want := trueLineRows(option0Width, width) + trueLineRows(option1Width, width)
		if got := selectOptionsRows(width, options, 0); got != want {
			t.Errorf("selectOptionsRows(%d, ...) = %d, want %d (true painted rows: option 0 = %d, option 1 = %d)",
				width, got, want, trueLineRows(option0Width, width), trueLineRows(option1Width, width))
		}
	}
}

// TestSelectBlockRowsMatchesSplitOptionsRows pins selectBlockRows' round-3
// refactor (question rows + selectOptionsRows) against an independent,
// single-builder measurement of the exact same bytes: splitting the
// question line's own advanceRows call from the option block's must not
// change the total, since the question line always ends in '\n' and resets
// col to 0 before the option block begins either way.
func TestSelectBlockRowsMatchesSplitOptionsRows(t *testing.T) {
	options := []selectOption{
		{label: "on this machine", hint: "always available, any agent that can reach it"},
		{label: "in this folder", hint: "lives with this project"},
	}
	const width = 50
	const question = "Where should this network live?"

	got := selectBlockRows(width, question, options, 0)

	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n", question)
	for i, opt := range options {
		fmt.Fprintf(&b, "  %s\n", selectOptionText(opt, i == 0))
	}
	_, want, _ := advanceRows(0, width, false, b.String())

	if got != want {
		t.Fatalf("selectBlockRows(%d, ...) = %d, want %d (single-builder advanceRows over the identical bytes)", width, got, want)
	}
}
