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
