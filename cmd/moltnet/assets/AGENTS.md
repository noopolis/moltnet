# Assets Guide

This folder holds static, non-Go assets embedded into the `moltnet` binary.

## banner.txt

`banner.txt` is the "moltnet" wordmark printed above `moltnet init` and bare
`moltnet` output on a terminal (see `cmd/moltnet/banner.go`).

It was rendered headlessly with
[glyphcss](https://github.com/apresmoi/glyphcss): 3D extruded text using
`@glyphcss/fonts` textPolygons (Roboto-Bold), an orthographic camera with
`rotX: 90`, `mode: "ink"`, `autoFit` at 58 columns, and `useColors: false`.

`banner.go` treats this file as opaque text — no per-line assumptions, just
an embedded string that gets dimmed and printed. Edit the art by
re-rendering it with glyphcss and overwriting the file, not by hand-editing
the characters in place.
