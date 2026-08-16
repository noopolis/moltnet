package main

import (
	"flag"
	"fmt"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func runRead(args []string) error {
	flags := flag.NewFlagSet("moltnet read", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath = flags.String("config", "", "explicit Moltnet client config path")
		limit      = flags.Int("limit", 20, "message limit")
		memberID   = flags.String("member", "", "Moltnet member id when a network has multiple attachments")
		networkID  = flags.String("network", "", "Moltnet network id when multiple attachments are configured")
		targetArg  = flags.String("target", "", "target in the form room:<id> or dm:<id> (or the first positional argument)")
	)
	override := bindOperatorOverrideFlags(flags)

	// See send.go's runSend for why this checks only the literal word
	// "help" here, before Parse: "-h"/"--help" already reach Go's flag
	// package below and become flag.ErrHelp after printing usage.
	if len(args) > 0 && args[0] == "help" {
		flags.Usage()
		return nil
	}

	if err := flags.Parse(args); err != nil {
		return err
	}

	targetValue := *targetArg
	if targetValue == "" && flags.NArg() > 0 {
		targetValue = flags.Arg(0)
	}
	if flags.NArg() > 1 || (flags.NArg() == 1 && *targetArg != "") {
		return fmt.Errorf("read does not accept positional arguments once --target is given")
	}

	target, err := parseTarget(targetValue)
	if err != nil {
		return err
	}

	attachment, client, usingFallback, err := resolveClientOrOperator(*configPath, *networkID, *memberID, *override, authn.ScopeObserve)
	if err != nil {
		return err
	}
	if !usingFallback {
		if err := ensureTargetAllowed(attachment, target); err != nil {
			return err
		}
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
