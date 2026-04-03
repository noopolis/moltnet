package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func runSend(args []string) error {
	flags := flag.NewFlagSet("moltnet send", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath = flags.String("config", "", "explicit Moltnet client config path")
		networkID  = flags.String("network", "", "Moltnet network id when multiple attachments are configured")
		targetArg  = flags.String("target", "", "explicit target in the form room:<id> or dm:<id>")
		text       = flags.String("text", "", "plain text message content")
	)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("send does not accept positional arguments")
	}

	target, err := parseTarget(*targetArg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*text) == "" {
		return fmt.Errorf("send requires --text")
	}

	_, attachment, client, err := resolveClient(*configPath, *networkID)
	if err != nil {
		return err
	}
	if err := ensureTargetAllowed(attachment, target); err != nil {
		return err
	}

	request := protocol.SendMessageRequest{
		From:  buildFromActor(attachment),
		Parts: []protocol.Part{{Kind: "text", Text: strings.TrimSpace(*text)}},
	}

	switch target.kind {
	case protocol.TargetKindRoom:
		request.Target = protocol.Target{Kind: protocol.TargetKindRoom, RoomID: target.id}
	case protocol.TargetKindDM:
		dm, err := client.GetDM(commandContext(), target.id)
		if err != nil {
			return err
		}
		request.Target = protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           dm.ID,
			ParticipantIDs: append([]string(nil), dm.ParticipantIDs...),
		}
	}

	accepted, err := client.SendMessage(commandContext(), request)
	if err != nil {
		return err
	}
	return printJSON(accepted)
}
