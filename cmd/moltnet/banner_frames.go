package main

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// bannerFramesText is the embedded settle animation: rotY -25deg to 0deg
// over up to 16 source frames (frame 15, the last, is the frontal pose
// banner.txt is authored at), each rendered on a fixed 40x8 canvas, frames
// separated by a line containing only "%%". See assets/AGENTS.md for full
// render provenance. parseBannerFrames crops that shared canvas margin away
// and drops consecutive duplicate frames before playBanner ever sees them
// — see bannerFrames/bannerFramesErr below. assets/banner-frames-loop.txt
// (a 7-frame idle pendulum quarter-period — 0deg to +18deg; a player
// ping-pongs it for the full swing, see assets/AGENTS.md) ships alongside
// it for a later "waiting" animation but is not embedded or wired up here.
//
//go:embed assets/banner-frames.txt
var bannerFramesText string

// bannerFrameSeparator is the line that splits bannerFramesText into
// frames.
const bannerFrameSeparator = "%%"

// bannerFrames and bannerFramesErr are parsed once at package init from
// bannerFramesText: split into frames, dimension-validated, cropped to
// their shared content window, and de-duplicated (parseBannerFrames). A
// non-nil bannerFramesErr means the embedded asset failed its own
// dimension validation: playBanner treats that as "cannot animate" and
// falls back to the static banner rather than panicking —
// TestParseBannerFramesUniformDimensions is what actually catches a
// malformed or drifted assets/banner-frames.txt, at test time, before it
// ever ships.
var bannerFrames, bannerFramesErr = parseBannerFrames(bannerFramesText)

// bannerFrameContentWidth is the widest row (rune count) across every
// parsed, cropped, trailing-space-trimmed frame in bannerFrames. playBanner
// requires the terminal be at least bannerFrameContentWidth+1 columns wide
// before it will animate — see playBanner's doc comment for why the "+1"
// matters (deferred autowrap).
var bannerFrameContentWidth = maxFrameRowWidth(bannerFrames)

// bannerFrame is one frame of the settle animation: its rows, in source
// order, already cropped to the animation's shared content window and
// right-trimmed of trailing spaces by parseBannerFrames. Rows carry no
// trailing newline; block() adds exactly one.
type bannerFrame struct {
	rows []string
}

// block renders the frame for a single write: its rows joined by "\n",
// with exactly one trailing "\n" after the last row — never zero, never
// two — so every frame, first or Nth, advances the cursor by exactly
// len(rows) lines. playBanner's per-step "\x1b[<rows>A" cursor-up depends
// on that being true of every frame, not just the first.
func (f bannerFrame) block() string {
	return strings.Join(f.rows, "\n") + "\n"
}

// parseBannerFrames splits raw into frames, validates their dimensions,
// crops away the shared canvas margin, and drops consecutive duplicate
// frames. It returns an error rather than panicking on a dimension
// mismatch — see this file's package doc comment on
// bannerFrames/bannerFramesErr for why that matters.
func parseBannerFrames(raw string) ([]bannerFrame, error) {
	frames, err := splitAndValidateBannerFrames(raw)
	if err != nil {
		return nil, err
	}

	frames = cropBannerFramesToSharedContent(frames)
	frames = dedupeConsecutiveBannerFrames(frames)

	if len(frames) < 2 {
		return nil, fmt.Errorf("banner frames: %d frame(s) remain after removing consecutive duplicates, want at least 2 for an animation", len(frames))
	}
	return frames, nil
}

// splitAndValidateBannerFrames splits raw wherever a line is exactly
// bannerFrameSeparator, and validates that every frame has the same row
// count and every row across every frame has the same column count (rune
// count, since the wordmark is drawn in multi-byte Unicode quadrant-block
// characters where each rune is exactly one terminal cell). It returns an
// error rather than panicking on mismatch.
func splitAndValidateBannerFrames(raw string) ([]bannerFrame, error) {
	lines := strings.Split(raw, "\n")
	// A well-formed frames file ends with a single trailing newline after
	// its last row; strings.Split turns that into one extra empty trailing
	// element that is not itself a blank row of the last frame.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var frames []bannerFrame
	var current []string
	for _, line := range lines {
		if line == bannerFrameSeparator {
			frames = append(frames, bannerFrame{rows: current})
			current = nil
			continue
		}
		current = append(current, line)
	}
	frames = append(frames, bannerFrame{rows: current})

	if len(frames) < 2 {
		return nil, fmt.Errorf("banner frames: parsed %d frame(s), want at least 2 for an animation", len(frames))
	}

	wantRows := len(frames[0].rows)
	wantCols := -1
	for i, f := range frames {
		if len(f.rows) != wantRows {
			return nil, fmt.Errorf("banner frames: frame %d has %d row(s), want %d (frame 0's row count)", i, len(f.rows), wantRows)
		}
		for j, row := range f.rows {
			cols := utf8.RuneCountInString(row)
			if wantCols == -1 {
				wantCols = cols
				continue
			}
			if cols != wantCols {
				return nil, fmt.Errorf("banner frames: frame %d row %d has %d column(s), want %d", i, j, cols, wantCols)
			}
		}
	}

	return frames, nil
}

// cropBannerFramesToSharedContent strips assets/banner-frames.txt's shared
// fixed-canvas margin (see assets/AGENTS.md) using ONE content window
// computed as the union across every frame — never a per-frame bounding
// box — so every frame keeps exactly the same row/column offset as every
// other. A per-frame crop would let each frame's own tightest content box
// shift independently (foreshortening narrows a turntable-rotated frame
// from a different side than the frontal one), which is exactly what put
// the animated banner +2 cols/+1 row off the static banner's position
// (P2): playBanner used to write the frame's full, uncropped canvas.
// Rows span every frame identically (rotX stays pinned throughout the
// sweep, so row placement never moves); columns take the union of every
// frame's own left/right content edge, so the widest frame is never
// clipped.
//
// Every row is then right-trimmed of trailing spaces — invisible cells,
// so this never changes what's on screen — which both narrows playBanner's
// per-write cost (P1 narrow-terminal fix) and, combined with the shared
// crop window, reproduces banner.txt's own ragged-row shape exactly
// (glyphcss's cropGlyphInner trims trailing spaces independently per row):
// TestFinalBannerFrameMatchesStaticBanner checks the animation's rendered
// output is byte-identical to printBanner's static output, no test-only
// normalization required.
func cropBannerFramesToSharedContent(frames []bannerFrame) []bannerFrame {
	if len(frames) == 0 {
		return frames
	}

	rowFirst, rowLast := -1, -1
	colFirst, colLast := -1, -1
	for _, f := range frames {
		for i, row := range f.rows {
			runes := []rune(row)
			left, right := -1, -1
			for j, r := range runes {
				if r == ' ' {
					continue
				}
				if left == -1 {
					left = j
				}
				right = j
			}
			if left == -1 {
				continue // blank row: contributes nothing to the window
			}
			if rowFirst == -1 || i < rowFirst {
				rowFirst = i
			}
			if i > rowLast {
				rowLast = i
			}
			if colFirst == -1 || left < colFirst {
				colFirst = left
			}
			if right > colLast {
				colLast = right
			}
		}
	}
	if rowFirst == -1 {
		// Every frame is entirely blank; nothing to crop.
		return frames
	}

	cropped := make([]bannerFrame, len(frames))
	for fi, f := range frames {
		rows := make([]string, 0, rowLast-rowFirst+1)
		for i := rowFirst; i <= rowLast; i++ {
			runes := []rune(f.rows[i])
			end := colLast + 1
			if end > len(runes) {
				end = len(runes)
			}
			start := colFirst
			if start > end {
				start = end
			}
			rows = append(rows, strings.TrimRight(string(runes[start:end]), " "))
		}
		cropped[fi] = bannerFrame{rows: rows}
	}
	return cropped
}

// dedupeConsecutiveBannerFrames drops any frame whose rows are identical to
// the frame immediately before it. assets/banner-frames.txt currently has
// two such pairs, both artifacts of the easing curve's discrete steps
// landing on the same glyph selection twice in a row rather than any real
// motion: each duplicate is a transition that draws, sleeps
// bannerFrameStepInterval, and redraws pixel-for-pixel the same content — a
// dead frame from the viewer's perspective. Dropping them shortens the
// frame count (and thus, via bannerFrameStepInterval, tightens the
// per-transition spacing of the frames that remain) without touching the
// embedded asset itself.
func dedupeConsecutiveBannerFrames(frames []bannerFrame) []bannerFrame {
	if len(frames) == 0 {
		return frames
	}
	deduped := frames[:1:1]
	for _, f := range frames[1:] {
		prev := deduped[len(deduped)-1]
		if slices.Equal(prev.rows, f.rows) {
			continue
		}
		deduped = append(deduped, f)
	}
	return deduped
}

// maxFrameRowWidth returns the widest row (rune count) across every frame,
// or 0 for an empty/nil slice (the bannerFramesErr != nil case, where
// bannerFrames is nil and this value is never actually consulted by
// playBanner).
func maxFrameRowWidth(frames []bannerFrame) int {
	width := 0
	for _, f := range frames {
		for _, row := range f.rows {
			if w := utf8.RuneCountInString(row); w > width {
				width = w
			}
		}
	}
	return width
}
