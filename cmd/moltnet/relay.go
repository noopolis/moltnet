package main

import (
	"errors"
	"fmt"
)

// runRelayCommand implements `moltnet relay ...`, currently just `deploy`.
func runRelayCommand(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, buildRelayUsage())
		return errors.New("relay requires a subcommand (deploy)")
	}
	switch args[0] {
	case "help", "--help", "-h":
		fmt.Fprint(stdout, buildRelayUsage())
		return nil
	case "deploy":
		return runRelayDeploy(args[1:])
	default:
		fmt.Fprint(stdout, buildRelayUsage())
		return fmt.Errorf("unknown relay subcommand %q", args[0])
	}
}
