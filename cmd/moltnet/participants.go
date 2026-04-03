package main

import (
	"flag"
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func runParticipants(args []string) error {
	flags := flag.NewFlagSet("moltnet participants", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath = flags.String("config", "", "explicit Moltnet client config path")
		networkID  = flags.String("network", "", "Moltnet network id when multiple attachments are configured")
		targetArg  = flags.String("target", "", "explicit target in the form room:<id> or dm:<id>")
	)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("participants does not accept positional arguments")
	}

	target, err := parseTarget(*targetArg)
	if err != nil {
		return err
	}

	_, attachment, client, err := resolveClient(*configPath, *networkID)
	if err != nil {
		return err
	}
	if err := ensureTargetAllowed(attachment, target); err != nil {
		return err
	}

	switch target.kind {
	case protocol.TargetKindRoom:
		room, err := client.GetRoom(commandContext(), target.id)
		if err != nil {
			return err
		}
		return printJSON(room)
	case protocol.TargetKindDM:
		dm, err := client.GetDM(commandContext(), target.id)
		if err != nil {
			return err
		}
		return printJSON(dm)
	default:
		return fmt.Errorf("unsupported target kind %q", target.kind)
	}
}
