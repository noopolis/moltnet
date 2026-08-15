package main

import (
	"fmt"
	"io"
	"os"
)

var version = "0.0.0-dev"
var stdout io.Writer = os.Stdout

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, red("error:")+" "+err.Error())
		os.Exit(1)
	}
}

func runMain(args []string) error {
	return runCLI(args, version, defaultSignalContext)
}
