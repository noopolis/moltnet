# Assets Guide

This folder holds static, non-Go assets embedded into the `moltnet` binary.

## banner.txt

`banner.txt` is the "moltnet" wordmark printed above `moltnet init` and bare
`moltnet` output on a terminal (see `cmd/moltnet/banner.go`).

It was rendered headlessly with
[glyphcss](https://github.com/apresmoi/glyphcss): 3D extruded text using
`@glyphcss/fonts` textPolygons (Roboto-Bold), lowercase "moltnet", depth 2,
via `compileScene` directly (`compilePolygons` doesn't expose `charMode`) —
`mode: "solid"`, `glyphPalette: "solid"`, `charMode: "quadrant"`,
`cellAspect: 1.4` — with an orthographic camera at `rotX: 90, rotY: 0`
(frontal, directly down the extrusion axis), sized to 36×6 using a
probe-zoom-ladder workaround for the `rotX: 90` `autoFit` blank-probe bug
(the library's single fixed probe zoom renders fully blank at this angle for
short words, so the render walks a ladder of larger probe zooms and keeps
the largest one whose measured content still fits the probe grid).

`banner.go` treats this file as opaque text — no per-line assumptions, just
an embedded string that gets dimmed and printed. Edit the art by
re-rendering it with glyphcss and overwriting the file, not by hand-editing
the characters in place.

## banner-frames.txt / banner-frames-loop.txt

`banner-frames.txt` (16 frames) and `banner-frames-loop.txt` (7 frames,
kept in for a later "waiting" idle animation — not wired into any command
yet) are animation frame sets for `banner.txt`'s wordmark, embedded and
played by `cmd/moltnet/banner_player.go`. Same glyphcss pipeline as
`banner.txt` above (`compileScene` direct, Roboto-Bold, lowercase
"moltnet", depth 2, `mode: solid`/`glyphPalette: solid`/`charMode:
quadrant`/`cellAspect: 1.4`, orthographic, `rotX: 90`), with two differences
from the static banner's render:

- **zoom is fixed** at `3.76595` across every frame of both files (see
  "Matching banner.txt exactly" below for how this value and the render
  canvas were derived) rather than re-computed per frame, so the wordmark's
  size never jumps between steps.
- **only `rotY` sweeps**; `rotX` stays pinned at 90 throughout, so every
  frame has identical row placement/height and only the column width
  foreshortens — a horizontal "turntable" effect. `banner-frames.txt` eases
  `rotY` from -25° to 0° (fast start, decelerating into rest — frame 15,
  the last, is the frontal pose, `t=1`). `banner-frames-loop.txt` was
  originally rendered as a full-period pendulum, `rotY = 18·sin(2πi/24)` for
  `i = 0..23` — but a pendulum swinging left is indistinguishable, frame by
  frame, from the same pendulum swinging right (foreshortening only ever
  narrows the sign; the render carries no sign of its own), so the
  negative-rotY half of that render (`i = 13..23`) came out byte-identical
  to the positive half reversed (frame `i` == frame `24-i mod 24` for every
  `i`, only 7 distinct rows of glyphs among the 24 frames). The same
  symmetry recurs one level down: that non-redundant half-period, `i =
  0..12` (sweeping `0° → peak (+18° at i=6) → 0°`), is itself symmetric
  about its own peak — `sin(θ)` is symmetric about `θ=π/2`, so frame `i =
  6+k` came out byte-identical to frame `i = 6-k` for every `k = 1..6`
  (frame 7 == frame 5, frame 8 == frame 4, … frame 12 == frame 0). What
  ships here is just the non-redundant quarter: **7 frames,
  `rotY = 18·sin(2πi/24)` for `i = 0..6`** — a quarter-period sweeping `0° →
  peak (+18° at i=6)`, frame 0 the frontal pose. A player reconstructs the
  `0° → peak → 0°` half-swing this file used to ship outright by
  ping-ponging these 7 frames (forward `0..6` then backward `5..0`, repeat —
  the full cycle of one swing direction); the full back-and-forth pendulum
  needs the same trick one level up on top of that (play the reconstructed
  half-swing forward, then backward again) rather than looping either level
  forward-only, which would show only a quarter or a half of the swing
  before jumping back to frame 0.

Both files render every frame at a **fixed 40×8 canvas** (a tight common
content box across every frame of that file — frames 7-15 of
`banner-frames.txt` tie for widest, since foreshortening only ever narrows
the sign and several late steps round to the same pixel width — plus a
uniform 2-column / 1-row margin, so content never touches col 0, the last
col, row 0, or the last row of any frame), so every frame has identical
dimensions regardless of how narrow the sign gets — required for
`banner_player.go`'s cursor-up redraw, which needs a constant row count to
erase and repaint in place. Frames are separated by a line containing only
`%%`; every frame within a file has the same row/column count
(`cmd/moltnet/banner_player_test.go` pins this).

### Matching banner.txt exactly

Earlier renders of these two files used a *different* zoom/canvas than the
one that actually produced `banner.txt`, so the frontal frame (rotY=0) came
out at the right size and pose but with a **different quadrant sub-cell
phase** — same wordmark, systematically different choice of which
`▘▝▖▗▄▖▙▟▛▜█` glyph represents a given stroke edge. The cause: glyphcss's
orthographic camera centers the projected content at `(cols/2, rows/2)` in
fractional cell-space (`resolveProjectionMetrics` in
`packages/glyphcss/src/api/createGlyphCamera.ts`); cell pixel size itself
(`BASE_TILE`/`cellAspect`) does not depend on canvas size, but **canvas
parity does** — an even `cols` or `rows` puts that center exactly on a cell
boundary, an odd one puts it a half-cell off, and the quadrant charMode's
2×2 sub-cell sampling picks a different glyph depending on which side of
that boundary a stroke edge falls. `banner.txt`'s own render (36×6, cropped
from a larger canvas by `compilePolygons`'s probe-then-crop `autoFit`
algorithm) and the previous fixed-canvas animation render (40×7) landed on
different canvas parities, hence the mismatch — not a content, sizing, or
word bug.

The fix: reproduce `compilePolygons`'s exact `autoFit` algorithm
(`packages/compile/src/compileFile.ts`) on top of `compileScene` directly
(same gap as `banner.txt` above — `compilePolygons` doesn't expose
`charMode`), including the probe-zoom-ladder workaround for the `rotX: 90`
blank-probe bug, targeting `cols=36` by width — the same target `banner.txt`
used (candidate #26 in the untracked local rendering gallery this project
kept during banner selection, `BANNERS.md`; it never made it into the repo,
so it's not something a later reader can open) — then grid-searching the
ladder step for the exact `(zoom, cols, rows)` triple whose crop is
byte-identical to `banner.txt`. That search landed on **`cols=57, rows=15`
(both odd — the required parity) at `zoom` anywhere in a small plateau
centered on `≈3.76595`** (float rounding inside that ladder step doesn't
change the selected glyphs); `3.76595` is the value actually used, fixed
across every frame of both files exactly like the zoom before it. Rendering
the frontal pose (`rotY=0`) at that exact triple and cropping to content
reproduces `banner.txt` exactly, modulo one representation difference:
`banner.txt` (via glyphcss's own `cropGlyphInner`) trims *trailing* spaces
independently per row, so its rows are ragged (35, 36, 36, 35, 36, 36
columns).

An individual animation frame's own rows can't be independently ragged
that way — `parseBannerFrames` requires every row of every frame in a file
to share one column count, straight off the fixed 40×8 canvas render. That
uniform-canvas requirement is exactly why `cmd/moltnet/banner_player.go`
does its own cropping at parse time rather than shipping the raw canvas to
the terminal: it computes ONE content window as the union of every frame's
bounding box (not each frame's own tightest box — a per-frame crop would
let each frame's content shift to a different on-screen column as the
turntable foreshortens, which is what originally put the animated banner
off the static banner's position), crops every frame to that shared window,
and then right-trims trailing spaces per row — safe because they're
invisible cells, and it's what makes the cropped frames end up exactly as
ragged as `banner.txt` itself. `TestFinalBannerFrameMatchesStaticBanner`
(`banner_player_test.go`) checks the parsed settled last frame against
`banner.txt` with strict equality and no normalization on either side;
`TestPlayBannerAnimatesOverRealPTY` checks the same guarantee against what
actually gets written to a real terminal.

Same rule as `banner.txt`: edit either file by re-rendering with glyphcss
(same fixed zoom, same canvas parity, same rotX/rotY sweep), never by
hand-editing the characters in place.
