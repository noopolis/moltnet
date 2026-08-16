package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

var version = "0.0.0-dev"
var stdout io.Writer = os.Stdout

// stderr is the same swappable-for-tests seam as stdout, for the handful of
// callers (reportConsoleServerDown, console.go) that deliberately write to
// standard error instead of standard output — e.g. so a script capturing
// `moltnet console --print`'s stdout never sees failure prose mixed into the
// URL it's trying to parse.
var stderr io.Writer = os.Stderr

func main() {
	if err := runMain(os.Args[1:]); err != nil {
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
		fmt.Fprintln(os.Stderr, red("error:")+" "+err.Error())
		os.Exit(1)
	}
}

func runMain(args []string) error {
	return runCLI(args, version, defaultSignalContext)
}
