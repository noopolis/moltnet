package main

import "bufio"

// setup_select_key.go holds promptSelect's raw-key decoding, split out of
// setup_prompt_select.go purely to keep that file under this repo's
// 400-line limit.

// selectKey is one decoded keypress from readSelectKey's byte stream.
type selectKey int

const (
	selectKeyNone selectKey = iota
	selectKeyUp
	selectKeyDown
	selectKeyConfirm
	selectKeyCancel
)

// readSelectKey decodes exactly one key from r: '\r'/'\n' confirms the
// current selection, 0x03 (Ctrl-C, see errSetupCancelled's doc comment)
// cancels, an ANSI cursor-up/down sequence ("\x1b[A" / "\x1b[B" — the
// sequence every common terminal emulator sends for the up/down arrow keys)
// moves the selection, and anything else decodes as selectKeyNone (ignored
// by the caller's redraw loop, so an unrecognized escape sequence or stray
// byte never crashes or corrupts the selection state). It is a plain
// function over a *bufio.Reader with no terminal dependency of its own, so
// it is unit-tested directly against a scripted byte stream
// (setup_prompt_select_test.go) — the terminal-facing half of arrow-key
// selection (enableRawSelectMode) is tested separately, the same split this
// package already uses for promptHidden vs. disableTerminalEcho.
func readSelectKey(r *bufio.Reader) (selectKey, error) {
	b, err := r.ReadByte()
	if err != nil {
		return selectKeyNone, err
	}
	switch b {
	case '\r', '\n':
		return selectKeyConfirm, nil
	case 0x03:
		return selectKeyCancel, nil
	case 0x1b:
		b2, err := r.ReadByte()
		if err != nil {
			return selectKeyNone, err
		}
		if b2 != '[' {
			return selectKeyNone, nil
		}
		b3, err := r.ReadByte()
		if err != nil {
			return selectKeyNone, err
		}
		switch b3 {
		case 'A':
			return selectKeyUp, nil
		case 'B':
			return selectKeyDown, nil
		default:
			return selectKeyNone, nil
		}
	default:
		return selectKeyNone, nil
	}
}
