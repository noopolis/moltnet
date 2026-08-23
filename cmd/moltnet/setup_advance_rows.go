package main

import "unicode/utf8"

// setup_advance_rows.go holds advanceRows, split out of setup_header.go
// purely to keep that file under this repo's 400-line limit.

// advanceRows is setupHeader.Write's pure row-accounting core, split out so
// it is unit-tested directly against a known width and starting column —
// mirroring nextPingPongIndex's own "pure function, independent of any
// terminal state" split (setup_header_render.go) — rather than only
// indirectly through a real pty. It reports how many additional terminal
// rows s consumes when appended at column col on a width-column-wide
// terminal, the column the cursor ends up at afterward, and whether a wrap
// is left pending at the end of s (see pendingWrap below) — the same triple
// setupHeader.col/pendingWrap carry across separate Write calls.
//
// Two things advance the row count, matching how a real terminal actually
// tracks its own cursor: an explicit '\n' byte always advances one row and
// resets the column to 0, and every other visible character advances the
// column by one. ANSI SGR escape sequences ("\x1b[...m", the only kind
// style.go's dim/green/yellow/bold/red ever produce) are skipped entirely
// rather than counted as columns: they are zero-width on a real terminal, so
// counting their bytes would overstate how far a line actually reaches
// before it wraps.
//
// P2-3 (this fix): every mainstream terminal uses *deferred* (pending)
// autowrap, not immediate wrap — reaching the last column does not itself
// move the cursor to a new row. The cursor parks there, and the wrap only
// resolves once the *next visible character* actually arrives; a '\n' that
// arrives instead simply ends the line, consuming its own one row and
// nothing more, because a terminal never counts the deferred wrap as a
// second row when nothing after it turns out to occupy one. pendingWrap
// (the incoming and outgoing bool) is exactly this parked state, threaded
// through a single Write call's whole input and back out so it survives
// into the *next* Write call too — the same reason col itself is threaded
// rather than reset to 0 at the top of every call. col intentionally stays
// at width (not reset to 0) for as long as a wrap is pending: only the
// resolving character (or, for setupHeader.col's own purposes, the fact
// that a '\n' arrived instead) actually resets it.
//
// This replaces the immediate-wrap model P2-1 shipped (rows++ / col = 0 the
// instant col reached width): that model overcounted by exactly one row for
// any line landing on an exact multiple of width — not a rare case in
// practice, since the wizard's own Q1 option text (setup_questions.go's
// askSetupScope) renders at exactly 67 and 44 visible columns for its two
// options, so terminals at those two widths hit the overcount on every
// single redraw, not just hypothetically.
//
// width <= 0 means the caller could not determine a live terminal width
// (terminalWidth's own "assume unusable" case): wrapping is disabled
// entirely rather than guessed at, degrading to exactly the old
// newline-counting-only model instead of fabricating a wrap point that may
// not exist. pendingWrap can never become true in this mode.
//
// Known, consolidated cell-width modelling gaps -- grouped here because all
// three share the same root cause (this function's simplified terminal
// model) and, as of this sweep, are all confirmed unreachable through any
// call site in this package:
//
//  1. No mid-CSI state across separate Write calls: col alone is not enough
//     to resume a CSI sequence a previous Write call left mid-scan.
//     Verified directly: "\x1b[32m" + 19 chars + "\x1b[0m" fed in one call
//     reports (col 19, rows 0), but the identical bytes split into two
//     calls right after "\x1b[" report (col 2, rows 1) instead -- the
//     second call has no way to know it started inside an unterminated
//     escape and counts the escape's own remaining bytes as visible
//     columns. Unreachable: every writer in this package (style.go's
//     dim/green/etc, every Fprintf/Fprintln call site) emits a complete
//     escape sequence within a single Write call, and child process
//     output is buffered before being written rather than streamed
//     byte-by-byte into Write. Fix shape: carry an `inCSI bool` alongside
//     col.
//  2. '\r' and '\t' are each counted as one visible column, like any other
//     default-case byte, rather than '\r' resetting col to 0 or '\t'
//     advancing to the next tab stop -- worse, arriving right after a
//     pending wrap resolves it as if it were the next printed glyph,
//     charging a row a real terminal never charges for a non-printing
//     byte. Measured: advanceRows(0, 10, false, "0123456789\r") returns
//     (col 1, rows 1, false); a real terminal instead returns to column 0,
//     clears the pending wrap, and charges no row at all. Unreachable:
//     spinner.go is the only '\r' writer in this codebase, and only
//     `relay deploy` ever exercises it, never a header-active path.
//  3. A wide CJK/emoji rune still advances col by exactly one, undercounting
//     cells for anything that actually occupies two terminal columns (rune
//     counting itself is correct; terminal cell width per rune is not
//     modelled). Unreachable: every id this wizard mints or accepts
//     (network id, room id) is validated against an ASCII-only grammar
//     (protocol.ValidateMessageID, validateCanonicalRoomID) before it can
//     ever be echoed back while a header is live.
//
// A single, bounded fix could close all three (a small state machine
// tracking inCSI/tab-stops/rune width instead of the current byte-at-a-time
// scan), but every path that would exercise any of them is closed off by an
// upstream invariant elsewhere in this package -- kept as one consolidated
// note rather than three separately-tracked ones, so a change to any of
// those upstream invariants (a new raw-byte writer, a new free-text prompt
// echoed under a live header, non-ASCII ids ever being allowed) is the
// trigger to revisit this function, not a reason to believe it is broken
// today.
func advanceRows(col, width int, pendingWrap bool, s string) (newCol, rows int, newPendingWrap bool) {
	wrap := width > 0
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\n':
			// A '\n' always ends the line and always advances exactly one
			// row, whether or not a wrap was pending: a real terminal never
			// also charges the deferred wrap its own row here — the pending
			// state simply lapses along with the rest of the line.
			pendingWrap = false
			rows++
			col = 0
			i++
		case s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[':
			// P2-1: scan for the CSI sequence's actual final byte (any byte
			// in 0x40-0x7E — ECMA-48's whole final-byte range), not just
			// 'm' (SGR only, "dim/green/yellow/bold/red"). style.go's own
			// escapes are all SGR, but the header's own cursor-motion
			// sequences ("\x1b[3A", "\x1b[2K", ...) end in 'A'/'K'/etc, and
			// scanning only for 'm' never found one, hit the "unterminated"
			// branch below, and discarded every byte after the escape —
			// including its newlines — from this Write call's row count.
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j >= len(s) {
				// Genuinely unterminated CSI at the tail of this write (no
				// final byte anywhere in the rest of s): stop advancing col
				// for these trailing bytes rather than guess, but keep
				// whatever rows/col were already correctly counted before
				// it — style.go always writes a complete "<code>...reset"
				// span in one call, so this should never actually trigger
				// in practice.
				return col, rows, pendingWrap
			}
			i = j + 1
		default:
			// Escape sequences aside, this is the only branch that reaches a
			// genuinely visible character — exactly the event a deferred
			// wrap is waiting for. Resolve it first, before this character
			// is placed: it lands on the new row, not the one that just
			// filled up.
			if pendingWrap {
				rows++
				col = 0
				pendingWrap = false
			}
			_, size := utf8.DecodeRuneInString(s[i:])
			col++
			if wrap && col >= width {
				// col has reached the last column. Do not charge the row
				// yet: park here (col stays at width, not reset to 0) and
				// defer it exactly like a real terminal does, until either
				// a following visible character resolves it above or a '\n'
				// lets it lapse for free.
				pendingWrap = true
			}
			i += size
		}
	}
	return col, rows, pendingWrap
}
