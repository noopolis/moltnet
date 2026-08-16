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
// banner text with no escape codes. This is the plain, never-animated
// entry point — see printBannerAnimated for `init`'s counterpart.
func printBanner() {
	if !isOutputTerminal() {
		return
	}
	fmt.Fprint(stdout, dim(bannerText))
	fmt.Fprintln(stdout)
}

// printBannerAnimated is printBanner's counterpart for `moltnet init`
// only: same gate, same trailing blank line, but the banner content itself
// goes through playBanner (banner_player.go), which plays the settle
// animation when stdout is a genuine, ANSI-capable terminal and falls back
// byte-for-byte to printBanner's own static dim(bannerText) otherwise.
//
// Bare `moltnet` (cli.go's no-command path) deliberately keeps calling
// plain printBanner() instead of this — that path immediately starts the
// long-running server, and animating there would add up to ~1s to every
// server launch for a purely cosmetic flourish. `init` is already an
// interactive, one-shot command that narrates several steps in sequence,
// so the same ~1s reads as pacing rather than a delay.
func printBannerAnimated() {
	if !isOutputTerminal() {
		return
	}
	playBanner()
	fmt.Fprintln(stdout)
}
