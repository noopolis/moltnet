package main

import "context"

// setup_header_prompts.go holds the two header-aware prompt wrappers every
// setup question calls through (setupPromptSelect, setupPromptLineCtx) --
// split out of setup_header_render.go purely to keep that file under this
// repo's 400-line limit.

// setupPromptSelect is promptSelect's (setup_prompt_select.go) header-aware
// wrapper: every select question in the wizard calls this instead, so the
// ticker is guaranteed suspended for the entire read — including the raw-
// mode arrow-key loop's own in-place redraws — and resumed the instant the
// answer is in hand, win or lose. A no-op wrapper around promptSelect itself
// whenever activeSetupHeader is nil.
//
// P1-1: the select widget's redraw (renderSelectOptions) reprints its whole
// block in place on every arrow press — cursor up len(options), reprint,
// net zero terminal rows — but every one of those bytes still flows through
// Write, whose forward-only row-accounting model has no way to know the
// block wasn't growing the transcript at all: each redraw added
// len(options) to bodyLines that were never actually new rows. Rather than
// teach Write to recognize this one region's own cursor motion, this
// wrapper owns reconciling it directly: snapshot bodyLines before the read,
// let Write count (and miscount) freely while promptSelect runs, then
// overwrite bodyLines with the region's true final size — paintedRows,
// promptSelectInteractive's own live-geometry measurement of the question
// line plus the option block in its final settled state (selectBlockRows,
// wrap included — not a "1 + numOptions" formula that assumes one terminal
// row per option) — once the read returns. paintedRows already folds in the
// P1-2 trailing-blank-line row on the confirm/EOF paths (promptSelectInteractive
// adds it itself, since only it knows which return path fired); the
// selectRowsUnmeasured (-1) sentinel means promptSelectPlain ran instead,
// whose pure top-to-bottom prints Write already counted correctly, so no
// reconciliation happens at all in that case — overwriting a correct count
// with a foreign value would be strictly worse than leaving it alone.
func setupPromptSelect(ctx context.Context, question string, options []selectOption, defaultIndex int) (int, error) {
	h := activeSetupHeader.Load()
	h.Suspend()
	preSelectBodyLines := h.snapshotBodyLines()
	index, paintedRows, err := promptSelect(ctx, question, options, defaultIndex)
	if paintedRows != selectRowsUnmeasured {
		h.reconcileSelectBodyLines(preSelectBodyLines, paintedRows)
	}
	h.Resume()
	return index, err
}

// snapshotBodyLines reads bodyLines under mu; nil-safe (0 on a nil header,
// the same "no header" state every other method here tolerates).
func (h *setupHeader) snapshotBodyLines() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bodyLines
}

// reconcileSelectBodyLines is setupPromptSelect's P1-1/P1-2 fix: it
// overwrites bodyLines to preSelectBodyLines (everything printed before the
// question) plus blockRows — promptSelectInteractive's own live-geometry
// measurement of the question line and option block's true final row count,
// wrap included, with the confirm/EOF path's trailing blank line already
// folded in by the caller that measured it — discarding whatever Write
// accumulated while renderSelectOptions was redrawing the block in place.
// col and lastNL are reset to match: every line the select widget or its
// trailing blank prints ends in '\n', so the cursor is always at column 0
// once this returns. Nil-safe.
func (h *setupHeader) reconcileSelectBodyLines(preSelectBodyLines, blockRows int) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.bodyLines = preSelectBodyLines + blockRows
	h.col = 0
	h.lastNL = true
	h.mu.Unlock()
}

// setupPromptLineCtx is promptLineCtx's (setup_prompt_line_ctx.go)
// header-aware wrapper, used by every typed question in the wizard. Beyond
// suspend/resume, it also reports the one echoed row noteEchoedLine exists
// for — but only when the read actually completed (err == nil); a
// cancelled read (Ctrl-C mid-keystroke) never reaches Enter, so nothing was
// actually echoed to account for.
func setupPromptLineCtx(ctx context.Context, question string) (string, error) {
	h := activeSetupHeader.Load()
	h.Suspend()
	defer h.Resume()
	answer, err := promptLineCtx(ctx, question)
	if err == nil {
		h.noteEchoedLine(answer)
	}
	return answer, err
}
