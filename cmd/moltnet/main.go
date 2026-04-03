package main

import (
	"io"
	"log"
	"os"
)

var version = "0.0.0-dev"
var stdout io.Writer = os.Stdout

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func runMain(args []string) error {
	return runCLI(args, version, defaultSignalContext)
}
