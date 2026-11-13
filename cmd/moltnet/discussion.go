package main

import (
	"flag"
	"fmt"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func runDiscussion(args []string) error {
	flags := flag.NewFlagSet("moltnet discussion", flag.ContinueOnError)
	flags.SetOutput(stdout)
	config := flags.String("config", "", "client config path")
	network := flags.String("network", "", "network id")
	member := flags.String("member", "", "member id")
	targetArg := flags.String("target", "", "room:<id>")
	override := bindOperatorOverrideFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("discussion does not accept positional arguments")
	}
	target, err := parseTarget(*targetArg)
	if err != nil || target.kind != protocol.TargetKindRoom {
		return fmt.Errorf("discussion requires --target room:<id>")
	}
	attachment, client, fallback, err := resolveClientOrOperator(*config, *network, *member, *override, authn.ScopeObserve)
	if err != nil {
		return err
	}
	if !fallback {
		if err := ensureTargetAllowed(attachment, target); err != nil {
			return err
		}
	}
	discussion, err := client.GetDiscussion(commandContext(), target.id)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"discussion": discussion})
}
