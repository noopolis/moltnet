package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

var version = "0.0.0-dev"

// sourceCheckout is the source checkout path this binary was built from,
// stamped by the Makefile's `build` target via ldflags exactly like
// version is (`-X main.sourceCheckout=$(CURDIR)`). It is empty for any
// binary not built that way — release-assets/-docker builds, `go build`
// run by hand, or an older Makefile — and `moltnet update` (update.go)
// degrades to its plain source-install refusal whenever it is empty.
var sourceCheckout = ""

var stdout io.Writer = os.Stdout

// stderr is the same swappable-for-tests seam as stdout, for the handful of
// callers (reportConsoleServerDown, console.go) that deliberately write to
// standard error instead of standard output — e.g. so a script capturing
// `moltnet console --print`'s stdout never sees failure prose mixed into the
// URL it's trying to parse.
var stderr io.Writer = os.Stderr

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// Every flag.FlagSet in this CLI is built with
			// flags.SetOutput(stdout), so Go's flag package has already
			// printed usage to stdout by the time Parse returns ErrHelp
			// (its parseOne special-cases "-h"/"-help"/"--help": it calls
			// f.usage() itself before returning the sentinel). `--help` is
			// a request, never a failure, so this is the one place that
			// turns it into a clean exit 0 with no "error: ..." line on
			// top of the usage text that already printed — the same
			// sentinel-matching pattern as context.Canceled below.
			os.Exit(0)
		}
		if errors.Is(err, context.Canceled) {
			// A command aborted on ctx.Err() after SIGINT/SIGTERM (runCLI's
			// signal.NotifyContext, cli.go) — e.g. runInit mid-animation or
			// mid-prompt. The signal itself already told the operator what
			// happened; printing "error: context canceled" on top of that
			// would just be noise. Exit 130 is the conventional 128+SIGINT
			// code shells expect from an interrupted command.
			os.Exit(130)
		}
		if errors.Is(err, errConsoleServerDown) {
			// runConsole already printed the down-server fact and the
			// exact next command to stdout (reportConsoleServerDown,
			// console.go); echoing the same fact again as a bare
			// "error: ..." line on stderr would just repeat it with no
			// new information.
			os.Exit(1)
		}
		if errors.Is(err, errConsoleRestartFailed) {
			// restartForConsoleToken/selfHealConsoleToken (console_selfheal.go)
			// already printed exactly what failed and the manual command to
			// fix it, to stdout, alongside the rest of self-heal's status
			// lines; a second, context-free "error: ..." line here would just
			// repeat the fact with no new information.
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, red("error:")+" "+err.Error())
		os.Exit(1)
	}
}

func runMain(args []string) error {
	return runCLI(args, version, defaultSignalContext)
}
