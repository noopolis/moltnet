package main

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
)

// ANSI escape sequences for the small, fixed set of styles the CLI applies
// to its own output: bold for copy-paste commands, dim for right-column
// descriptions and path annotations, and three status colors (green for
// success, yellow for warning/note prefixes, red for the top-level error
// path in main.go). Stdlib-only, no color library dependency.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
)

// isOutputTerminal reports whether stdout is attached to a terminal. It
// mirrors isInteractive's go-isatty pattern (pair.go), checking the output
// side rather than stdin. It is a var, not a plain func, so tests can force
// the TTY-styled path without a real terminal attached to stdout — see
// cmd/moltnet/style_test.go, following the same seam pattern
// cmd/moltnet/uninstall_test.go uses for isInteractive.
var isOutputTerminal = func() bool {
	f, ok := stdout.(*os.File)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// isErrorTerminal is isOutputTerminal's counterpart for os.Stderr, the
// writer main.go's top-level error path uses. main() is never exercised
// under the captured-stdout test harness (it calls os.Exit), so this checks
// the real file descriptor directly; it is still a var for consistency and
// in case a future test wants to override it.
var isErrorTerminal = func() bool {
	fd := os.Stderr.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// noColorSet reports whether the NO_COLOR env var is set to any non-empty
// value, per https://no-color.org: "command-line software ... should check
// ... and if present and not an empty string ... should not add ANSI color".
func noColorSet() bool {
	return os.Getenv("NO_COLOR") != ""
}

// dumbTerminal reports whether TERM is set to "dumb", the conventional
// signal (used by readline, less, git, and others) that the terminal
// understands no ANSI escape sequences at all — distinct from NO_COLOR,
// which is an explicit user opt-out rather than a capability signal.
func dumbTerminal() bool {
	return os.Getenv("TERM") == "dumb"
}

// colorEnabled reports whether ANSI styling should be applied to stdout
// output: stdout must be a terminal, NO_COLOR must be unset, and TERM must
// not be "dumb" (P3-6). Checked fresh on every call rather than cached,
// since tests swap both stdout and isOutputTerminal between cases.
func colorEnabled() bool {
	return isOutputTerminal() && !noColorSet() && !dumbTerminal()
}

// errorColorEnabled is colorEnabled's counterpart for the stderr error
// path.
func errorColorEnabled() bool {
	return isErrorTerminal() && !noColorSet() && !dumbTerminal()
}

// styled wraps s in code (and a trailing reset) only when enabled; it is
// the shared implementation behind green/yellow/red/bold/dim below, and is
// what keeps plain (non-TTY, or NO_COLOR) output byte-identical to today's.
func styled(enabled bool, code, s string) string {
	if !enabled {
		return s
	}
	return code + s + ansiReset
}

// green, yellow, bold, and dim gate on colorEnabled(), i.e. on whether
// STDOUT is a styled terminal — they must only ever wrap text written to
// stdout. A write to stderr (os.Stderr, not the stdout var) styled with any
// of these four would use the wrong TTY/NO_COLOR/TERM check and could color
// (or fail to color) against the wrong stream's terminal state. red is the
// one exception: it gates on errorColorEnabled() (stderr's own TTY check)
// and is the only one of the five safe to use on a stderr write — see
// main.go's top-level error path for the intended usage.
func green(s string) string  { return styled(colorEnabled(), ansiGreen, s) }
func yellow(s string) string { return styled(colorEnabled(), ansiYellow, s) }
func bold(s string) string   { return styled(colorEnabled(), ansiBold, s) }
func dim(s string) string    { return styled(colorEnabled(), ansiDim, s) }
func red(s string) string    { return styled(errorColorEnabled(), ansiRed, s) }

// sectionPrinter prints a blank line to stdout before every call after its
// first, so a sequence of start() calls across a command's conditional
// output phases (a token prompt, a subdomain claim, a results block, ...)
// produces exactly one blank line between the phases that actually printed
// something, and none before the first phase or when a phase prints nothing
// at all — the house block style `moltnet relay deploy` shares with
// `init`/`uninstall` (see printInitSummary, printUninstallPlan). The zero
// value is ready to use; callers create one fresh per command invocation
// (see runRelayDeploy) so state never leaks between unrelated commands or
// test calls.
//
// Each phase-printing function calls start() itself, immediately before its
// own first write, rather than the orchestrator guessing in advance whether
// a given phase will print anything — a phase that turns out to print
// nothing (e.g. maybeSaveCloudflareToken's silent non-interactive no-op)
// then correctly costs no blank line at all.
type sectionPrinter struct{ started bool }

func (s *sectionPrinter) start() {
	if s.started {
		fmt.Fprintln(stdout)
	}
	s.started = true
}
