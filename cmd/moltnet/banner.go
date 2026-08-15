package main

import (
	_ "embed"
	"fmt"
)

// bannerText is the small ASCII wordmark printed above `moltnet init` and
// bare `moltnet` output on a terminal. It is opaque content: printBanner
// makes no per-line assumptions about it, so swapping assets/banner.txt for
// a generated glyphcss-rendered banner later is a content-only change, no
// code here needs to move.
//
//go:embed assets/banner.txt
var bannerText string

// printBanner writes the moltnet wordmark banner, dimmed, followed by a
// blank line, but only when stdout is a terminal (isOutputTerminal) — never
// when piped, and never in the non-TTY golden tests. Styling (dim) still
// goes through the usual colorEnabled gate, so NO_COLOR prints the same
// banner text with no escape codes.
func printBanner() {
	if !isOutputTerminal() {
		return
	}
	fmt.Fprint(stdout, dim(bannerText))
	fmt.Fprintln(stdout)
}
