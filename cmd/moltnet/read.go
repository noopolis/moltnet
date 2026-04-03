package main

import (
	"flag"
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func runRead(args []string) error {
	flags := flag.NewFlagSet("moltnet read", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath = flags.String("config", "", "explicit Moltnet client config path")
		limit      = flags.Int("limit", 20, "message limit")
		networkID  = flags.String("network", "", "Moltnet network id when multiple attachments are configured")
		targetArg  = flags.String("target", "", "explicit target in the form room:<id> or dm:<id>")
	)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("read does not accept positional arguments")
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

	pageRequest := protocol.PageRequest{Limit: *limit}

	switch target.kind {
	case protocol.TargetKindRoom:
		page, err := client.ListRoomMessages(commandContext(), target.id, pageRequest)
		if err != nil {
			return err
		}
		return printJSON(page)
	case protocol.TargetKindDM:
		page, err := client.ListDMMessages(commandContext(), target.id, pageRequest)
		if err != nil {
			return err
		}
		return printJSON(page)
	default:
		return fmt.Errorf("unsupported target kind %q", target.kind)
	}
}
