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
		fmt.Fprintln(os.Stderr, red("error:")+" "+err.Error())
		os.Exit(1)
	}
}

func runMain(args []string) error {
	return runCLI(args, version, defaultSignalContext)
}
